package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"tgguard/internal/domain"
	"tgguard/internal/i18n"
	"tgguard/internal/rules"
	"tgguard/internal/store"
)

func (s *Service) Moderate(ctx context.Context, update int64, m domain.Message) error {
	if m.From == nil || m.From.IsBot || m.SenderChat != nil || m.Chat.Type != "supergroup" {
		return nil
	}
	key := eventKey(update, "auto")
	prior, e := s.Store.GetLog(ctx, key)
	if e == nil {
		return s.applyModeration(ctx, m, prior)
	}
	if e != sql.ErrNoRows {
		return e
	}
	if e = s.Group(ctx, m.Chat); e != nil {
		return e
	}
	if e = s.Store.User(ctx, *m.From); e != nil {
		return e
	}
	settings, e := s.Store.Settings(ctx, m.Chat.ID)
	if e != nil {
		return e
	}
	n := rules.Normalize(m)
	n.IsNew, n.FirstMessage, e = s.Store.Context(ctx, m.Chat.ID, m.From.ID)
	if e != nil {
		return e
	}
	protected, e := s.ModerationProtected(ctx, m.Chat.ID, *m.From)
	if e != nil {
		return e
	}
	kind, e := s.Store.ListStatus(ctx, m.Chat.ID, m.From.ID, m.From.Username)
	if e != nil {
		return e
	}
	l := store.Log{EventKey: key, ChatID: n.ChatID, UserID: n.UserID, MessageID: n.MessageID, Text: m.Text + m.Caption, Risk: domain.Risk{Matches: []domain.Match{}}, Decision: domain.Decision{Action: "allow", Reason: "moderation_disabled"}, Source: "automatic"}
	if settings.ModerationEnabled && !(protected && (settings.AdminBypass || kind == "white" || kind == "trusted")) {
		l.Risk = rules.Evaluate(n, settings)
		if settings.SpamEnabled {
			dupText := n.Text
			if dupText == "" {
				dupText = strconv.FormatInt(update, 10)
			}
			rate, dup, e := s.State.Spam(ctx, n.ChatID, n.UserID, update, dupText, settings.RateWindow)
			if e != nil {
				return e
			}
			if rate > settings.RateLimit || dup >= settings.DuplicateLimit {
				l.Risk.Spam = true
				l.Risk.Score = 100
				l.Risk.Matches = append(l.Risk.Matches, domain.Match{Rule: "spam", Score: 100, Reason: fmt.Sprintf("rate=%d duplicate=%d", rate, dup)})
			}
		}
		if settings.AIEnabled && s.AI != nil && !l.Risk.Spam && l.Risk.Score >= settings.AIThreshold && l.Risk.Score < settings.DirectThreshold {
			a, e := s.AI.Review(ctx, n, l.Risk)
			if e != nil {
				slog.Warn("AI review unavailable; local decision only", "chat_id", n.ChatID, "error", e)
			} else {
				l.AI = &a
			}
		}
		count, e := s.Store.ViolationCount(ctx, n.ChatID, n.UserID)
		if e != nil {
			return e
		}
		l.Decision = rules.Decide(l.Risk, l.AI, settings, count, protected)
	}
	if kind == "black" && !protected {
		l.Decision = domain.Decision{Action: "ban", Delete: settings.AutoDelete, Reason: "blacklist"}
	}
	l, e = s.Store.SaveLog(ctx, l)
	if e != nil {
		return e
	}
	return s.applyModeration(ctx, m, l)
}
func (s *Service) applyModeration(ctx context.Context, m domain.Message, l store.Log) error {
	if l.Decision.Action != "allow" && l.Decision.Action != "shadow_log" {
		return s.Punish(ctx, l, 0)
	}
	if l.Decision.Action == "allow" {
		return s.Keywords(ctx, m, l.EventKey)
	}
	return nil
}

func (s *Service) Punish(ctx context.Context, l store.Log, actor int64) (err error) {
	if l.Decision.Action == "allow" || l.Decision.Action == "shadow_log" {
		return nil
	}
	unlock, e := s.State.Lock(ctx, verifyLock(l.ChatID, l.UserID), 60*time.Second)
	if e != nil {
		return e
	}
	defer unlock()
	p, e := s.Store.PreparePunishment(ctx, l, actor)
	if e != nil {
		return e
	}
	if p.Status == "done" || p.Status == "skipped" {
		return nil
	}
	defer func() {
		if err != nil {
			if e := s.Store.PunishmentError(ctx, l.EventKey, err); e != nil {
				slog.Error("punishment error audit failed", "error", e)
			}
		}
	}()
	u, e := s.Bot.Member(ctx, l.ChatID, l.UserID)
	if e != nil {
		return e
	}
	protected, e := s.Protected(ctx, l.ChatID, u.User)
	if e != nil {
		return e
	}
	if protected && p.Decision.Action != "unmute" && p.Decision.Action != "unban" {
		return s.Store.PunishmentStep(ctx, l.EventKey, "skipped")
	}
	d := p.Decision
	if d.Delete && !p.Deleted && l.MessageID > 0 {
		if e = s.Bot.Delete(ctx, l.ChatID, l.MessageID); e != nil {
			return e
		}
		if e = s.Store.PunishmentStep(ctx, l.EventKey, "deleted"); e != nil {
			return e
		}
	}
	if !p.Acted {
		if d.Action == "mute" || d.Action == "ban" || d.Action == "kick" || d.Action == "unmute" {
			if e = s.Store.CancelVerification(ctx, l.ChatID, l.UserID); e != nil {
				return e
			}
		}
		switch d.Action {
		case "delete":
			if !d.Delete && l.MessageID > 0 {
				e = s.Bot.Delete(ctx, l.ChatID, l.MessageID)
			}
		case "warn":
			settings, se := s.Store.Settings(ctx, l.ChatID)
			if se != nil {
				return se
			}
			e = s.Say(ctx, l.ChatID, settings.Language, "warning", l.UserID)
		case "mute":
			seconds := int(time.Until(p.CreatedAt.Add(time.Duration(d.Duration) * time.Second)).Seconds())
			if seconds > 0 {
				e = s.Bot.Restrict(ctx, l.ChatID, l.UserID, max(30, seconds))
			}
		case "ban":
			e = s.Bot.Ban(ctx, l.ChatID, l.UserID)
		case "kick":
			e = s.Bot.Kick(ctx, l.ChatID, l.UserID)
		case "unmute":
			e = s.Bot.Restore(ctx, l.ChatID, l.UserID)
		case "unban":
			e = s.Bot.Unban(ctx, l.ChatID, l.UserID)
		default:
			return fmt.Errorf("unsupported punishment %q", d.Action)
		}
		if e != nil {
			return e
		}
		if e = s.Store.PunishmentStep(ctx, l.EventKey, "acted"); e != nil {
			return e
		}
	}
	if e = s.Store.PunishmentStep(ctx, l.EventKey, "done"); e != nil {
		return e
	}
	s.LogChannel(ctx, l.ChatID, fmt.Sprintf("用户 %d，操作 %s，风险 %d，原因 %s", l.UserID, d.Action, l.Risk.Score, d.Reason))
	return nil
}

func (s *Service) Keywords(ctx context.Context, m domain.Message, event string) error {
	settings, e := s.Store.Settings(ctx, m.Chat.ID)
	if e != nil || !settings.KeywordEnabled {
		return e
	}
	ks, e := s.Store.Keywords(ctx, m.Chat.ID)
	if e != nil {
		return e
	}
	count := 0
	for _, k := range ks {
		if !rules.KeywordMatch(k, m.Body()) {
			continue
		}
		key := fmt.Sprintf("keyword:sent:%s:%d", event, k.ID)
		done, e := s.State.R.Exists(ctx, key).Result()
		if e != nil {
			return e
		}
		if done > 0 {
			if !settings.KeywordAll {
				return nil
			}
			continue
		}
		in := map[string]any{"chat_id": m.Chat.ID}
		if len(k.Buttons) > 0 {
			in["reply_markup"] = map[string]any{"inline_keyboard": k.Buttons}
		}
		if k.Reply {
			in["reply_parameters"] = map[string]any{"message_id": m.ID, "allow_sending_without_reply": true}
		}
		method := "sendMessage"
		content := k.Content
		if k.ReplyType == "random" {
			lines := strings.Split(content, "\n")
			content = lines[int(m.ID%int64(len(lines)))]
		}
		switch k.ReplyType {
		case "photo", "video", "document":
			method = "send" + strings.ToUpper(k.ReplyType[:1]) + k.ReplyType[1:]
			in[k.ReplyType] = content
		default:
			in["text"] = content
			if k.ReplyType == "HTML" || k.ReplyType == "MarkdownV2" {
				in["parse_mode"] = k.ReplyType
			}
		}
		if e = s.Bot.Call(ctx, method, in, nil); e != nil {
			return e
		}
		if e = s.State.R.Set(ctx, key, "1", 24*time.Hour).Err(); e != nil {
			return e
		}
		count++
		if !settings.KeywordAll || count >= 5 {
			break
		}
	}
	return nil
}

func (s *Service) Review(ctx context.Context, update int64, m domain.Message) error {
	if m.From == nil || m.Chat.Type != "supergroup" {
		return nil
	}
	settings, e := s.Store.Settings(ctx, m.Chat.ID)
	if e != nil {
		return e
	}
	member, e := s.Bot.Member(ctx, m.Chat.ID, m.From.ID)
	if e != nil {
		return e
	}
	if !member.Present() {
		return s.Say(ctx, m.Chat.ID, settings.Language, "denied")
	}
	admin, e := s.Admin(ctx, m.Chat.ID, m.From.ID)
	if e != nil {
		return e
	}
	kind, e := s.Store.ListStatus(ctx, m.Chat.ID, m.From.ID, m.From.Username)
	if e != nil {
		return e
	}
	if !admin && (settings.ReviewAccess == "admin" || settings.ReviewAccess == "trusted" && kind != "trusted" && kind != "white") {
		return s.Say(ctx, m.Chat.ID, settings.Language, "denied")
	}
	if m.Reply == nil || m.Reply.From == nil || m.Reply.SenderChat != nil {
		return s.Say(ctx, m.Chat.ID, settings.Language, "reply_required")
	}
	ok, e := s.State.Limit(ctx, fmt.Sprintf("review:rate:%d:%d", m.Chat.ID, m.From.ID), 3, time.Minute)
	if e != nil {
		return e
	}
	if !ok {
		return s.Say(ctx, m.Chat.ID, settings.Language, "review_limited")
	}
	if s.AI == nil || !settings.AIEnabled {
		return s.Say(ctx, m.Chat.ID, settings.Language, "ai_unavailable")
	}
	target := *m.Reply
	target.Chat = m.Chat
	n := rules.Normalize(target)
	r := rules.Evaluate(n, settings)
	a, e := s.AI.Review(ctx, n, r)
	if e != nil {
		slog.Warn("manual AI unavailable", "error", e)
		return s.Say(ctx, m.Chat.ID, settings.Language, "ai_unavailable")
	}
	l, e := s.Store.SaveLog(ctx, store.Log{EventKey: eventKey(update, "review"), ChatID: m.Chat.ID, UserID: n.UserID, MessageID: n.MessageID, Text: target.Text + target.Caption, Risk: r, AI: &a, Decision: domain.Decision{Action: "shadow_log", Reason: "manual_review"}, Source: "review"})
	if e != nil {
		return e
	}
	markup, e := s.ReviewButtons(ctx, l)
	if e != nil {
		return e
	}
	_, e = s.Bot.Send(ctx, m.Chat.ID, i18n.Text(settings.Language, "ai_result", a.IsAd, a.Confidence*100, a.Category, a.Reason, a.RecommendedAction), markup, m.ID)
	return e
}
