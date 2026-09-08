package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"tgguard/internal/domain"
	"tgguard/internal/rules"
	"tgguard/internal/store"
	"tgguard/internal/telegram"
)

func (s *Service) Moderate(ctx context.Context, update int64, m domain.Message) error {
	_, err := s.ModerateResult(ctx, update, m)
	return err
}
func (s *Service) ModerateResult(ctx context.Context, update int64, m domain.Message) (store.Log, error) {
	if m.From == nil || m.From.IsBot || m.SenderChat != nil || m.Chat.Type != "supergroup" {
		return store.Log{}, nil
	}
	if ok, e := s.Store.GroupAuthorized(ctx, m.Chat.ID); e != nil || !ok {
		return store.Log{}, e
	}
	key := eventKey(update, "auto")
	prior, e := s.Store.GetLog(ctx, key)
	if e == nil {
		return prior, s.applyModeration(ctx, m, prior)
	}
	if e != sql.ErrNoRows {
		return store.Log{}, e
	}
	if e = s.Group(ctx, m.Chat); e != nil {
		return store.Log{}, e
	}
	if e = s.Store.User(ctx, *m.From); e != nil {
		return store.Log{}, e
	}
	settings, e := s.Store.Settings(ctx, m.Chat.ID)
	if e != nil {
		return store.Log{}, e
	}
	n := rules.Normalize(m)
	n.IsNew, n.FirstMessage, e = s.Store.Context(ctx, m.Chat.ID, m.From.ID)
	if e != nil {
		return store.Log{}, e
	}
	kind, e := s.Store.ListStatus(ctx, m.Chat.ID, m.From.ID, m.From.Username)
	if e != nil {
		return store.Log{}, e
	}
	protected, e := s.moderationProtected(ctx, m.Chat.ID, *m.From, kind)
	if e != nil {
		return store.Log{}, e
	}
	l := store.Log{EventKey: key, ChatID: n.ChatID, UserID: n.UserID, MessageID: n.MessageID, Text: m.ModerationText(), Risk: domain.Risk{Matches: []domain.Match{}}, Decision: domain.Decision{Action: "allow", Reason: "moderation_disabled"}, Source: "automatic"}
	if settings.ModerationEnabled && !(protected && (settings.AdminBypass || kind == "white" || kind == "trusted")) {
		engine := moderationEngine{violations: s.Store, spam: s.State}
		if s.AI != nil {
			engine.review = func(ctx context.Context, n domain.Normalized, r *domain.Risk) *domain.AIResult {
				return s.reviewAI(ctx, n, r, "automatic", key)
			}
		}
		l.Risk, l.AI, l.Decision, e = engine.decide(ctx, n, settings, update, protected)
		if e != nil {
			return store.Log{}, e
		}
	}
	if kind == "black" && !protected {
		l.Decision = domain.Decision{Action: "ban", Delete: settings.AutoDelete, Reason: "blacklist"}
	}
	if settings.ModerationEnabled && !protected {
		l.Decision, e = s.repeatAdDecision(ctx, l, settings)
		if e != nil {
			return store.Log{}, e
		}
	}
	l, e = s.Store.SaveLog(ctx, l)
	if e != nil {
		return store.Log{}, e
	}
	return l, s.applyModeration(ctx, m, l)
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
	if ok, e := s.Store.GroupAuthorized(ctx, l.ChatID); e != nil || !ok {
		return e
	}
	unlock, e := s.State.Lock(ctx, verifyLock(l.ChatID, l.UserID), 60*time.Second)
	if e != nil {
		return e
	}
	defer unlock()
	loggedDecision := l.Decision
	if (l.Source == "automatic" || l.Source == "review") && l.Decision.Delete && l.Decision.Action != "ban" {
		already, err := s.Store.MessageAlreadyPunished(ctx, l)
		if err != nil || already {
			return err
		}
		settings, err := s.Store.Settings(ctx, l.ChatID)
		if err != nil {
			return err
		}
		l.Decision, err = s.repeatAdDecision(ctx, l, settings)
		if err != nil {
			return err
		}
	}
	p, e := s.Store.PreparePunishment(ctx, l, actor)
	if e != nil {
		return e
	}
	if p.Status == "done" || p.Status == "skipped" {
		return nil
	}
	if p.Status == "failed" {
		return fmt.Errorf("处罚已终止，请修复权限后重新发起操作")
	}
	l.Decision = p.Decision
	if l.Decision != loggedDecision {
		if e = s.Store.UpdateLogDecision(ctx, l); e != nil {
			return e
		}
	}
	mayHaveActed := p.Acted || p.Deleted || p.Started
	stage := "preflight"

	defer func() {
		if err != nil {
			auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			var te *telegram.APIError
			if stage == "action" && !mayHaveActed && errors.As(err, &te) && (te.Code == 400 || te.Code == 403) {
				if e := s.Store.RejectPunishment(auditCtx, l.EventKey); e != nil {
					slog.Error("punishment termination failed", "event_key", l.EventKey, "error", e)
				}
			}
			if e := s.Store.RetryPunishment(auditCtx, l.EventKey); e != nil {
				slog.Error("punishment retry scheduling failed", "event_key", l.EventKey, "error", e)
			}
			if e := s.Store.PunishmentError(auditCtx, l.EventKey, fmt.Errorf("%s: %w", stage, err)); e != nil {
				slog.Error("punishment error audit failed", "error", e)
			}
		}
	}()
	if l.UserID < 0 {
		return s.punishSenderChat(ctx, l, p)
	}
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
	if l.Source == "automatic" || l.Source == "manual" || l.Source == "review" {
		resolved, e := s.Store.NewerManualResolution(ctx, l.ChatID, l.UserID, p.CreatedAt)
		if e != nil {
			return e
		}
		if resolved {
			return s.Store.PunishmentStep(ctx, l.EventKey, "skipped")
		}
	}
	d := p.Decision
	stage = "action"
	if d.Delete && !p.Deleted && l.MessageID > 0 {
		if e = s.executor().Delete(ctx, l.ChatID, l.MessageID); e != nil {
			return e
		}
		if e = s.Store.PunishmentStep(ctx, l.EventKey, "deleted"); e != nil {
			return e
		}
		mayHaveActed = true
	}
	if !p.Acted {
		if d.Action == "mute" && d.Reason != "local_ad_review" && !p.Started && time.Until(p.CreatedAt.Add(time.Duration(d.Duration)*time.Second)) <= 0 {
			if e = s.Store.FailPunishment(ctx, l.EventKey); e != nil {
				return e
			}
			return fmt.Errorf("禁言计划已过期，请重新操作")
		}
		if d.Action == "mute" || d.Action == "ban" || d.Action == "kick" || d.Action == "unmute" || d.Action == "unban" {
			var until time.Time
			if d.Action == "mute" && d.Reason != "local_ad_review" {
				until = p.CreatedAt.Add(time.Duration(d.Duration) * time.Second)
			}
			if e = s.Store.StartPermission(ctx, l, until); e != nil {
				return e
			}
		}
		switch d.Action {
		case "delete":
			if !d.Delete && l.MessageID > 0 {
				e = s.executor().Delete(ctx, l.ChatID, l.MessageID)
			}
		case "warn":
			stage = "notification"
			settings, se := s.Store.Settings(ctx, l.ChatID)
			if se != nil {
				return se
			}
			count, ce := s.Store.ViolationCount(ctx, l.ChatID, l.UserID, l.EventKey)
			if ce != nil {
				return ce
			}
			l.Decision = d
			if l.Source == "review" && l.AI != nil {
				e = s.sendReviewNotice(ctx, l, u.User, settings, store.Punishment{Status: "done", Decision: d}, 0)
			} else {
				e = s.Bot.Call(ctx, "sendMessage", map[string]any{"chat_id": l.ChatID, "text": warningNotice(l, u.User, settings, count), "parse_mode": "HTML"}, nil)
			}
		case "mute":
			if d.Reason == "local_ad_review" {
				e = s.executor().Restrict(ctx, l.ChatID, l.UserID, 0)
				break
			}
			seconds := int(time.Until(p.CreatedAt.Add(time.Duration(d.Duration) * time.Second)).Seconds())
			if seconds > 0 {
				e = s.executor().Restrict(ctx, l.ChatID, l.UserID, seconds)
			} else {
				if e = s.Store.FailPunishment(ctx, l.EventKey); e != nil {
					return e
				}
				return fmt.Errorf("禁言计划已过期，请重新操作")
			}
		case "ban":
			e = s.executor().Ban(ctx, l.ChatID, l.UserID)
		case "kick":
			// A kick has two independently recoverable effects.
			var banned bool
			banned, e = s.Store.KickBanned(ctx, l.EventKey)
			if e == nil && !banned {
				e = s.executor().Ban(ctx, l.ChatID, l.UserID)
				if e == nil {
					mayHaveActed = true
					e = s.Store.MarkKickBanned(ctx, l.EventKey)
				}
			}
			if e == nil {
				mayHaveActed = true
				e = s.executor().Unban(ctx, l.ChatID, l.UserID)
			}
		case "unmute":
			e = s.executor().Restore(ctx, l.ChatID, l.UserID)
		case "unban":
			e = s.executor().Unban(ctx, l.ChatID, l.UserID)
		default:
			return fmt.Errorf("unsupported punishment %q", d.Action)
		}
		if e != nil {
			return e
		}
		mayHaveActed = true
		if d.Action == "mute" || d.Action == "ban" || d.Action == "kick" || d.Action == "unmute" || d.Action == "unban" {
			e = s.Store.CompleteTakeover(ctx, l)
		} else {
			e = s.Store.PunishmentStep(ctx, l.EventKey, "acted")
		}
		if e != nil {
			return e
		}
	}
	stage = "notification"
	if d.Action == "warn" && d.Delete && (l.Source == "automatic" || l.Source == "review") {
		if e = s.Store.RememberAdWarning(ctx, l, adFingerprint(l.Text)); e != nil {
			return e
		}
	}
	if d.Reason == "repeated_ad" {
		if l.Source == "review" && l.AI != nil {
			e = s.sendReviewNotice(ctx, l, u.User, domain.Settings{}, store.Punishment{Status: "done", Decision: d}, 0)
		} else {
			e = s.text(ctx, l.ChatID, fmt.Sprintf("用户 %d 再次发送已删除并警告的相同内容，原消息已删除，已禁言 %s。管理员可使用 /unmute %d 解除禁言。", l.UserID, warningDuration(d.Duration), l.UserID))
		}
		if e != nil {
			return e
		}
	}
	if d.Reason == "local_ad_review" {
		resolved, e := s.Store.NewerManualResolution(ctx, l.ChatID, l.UserID, p.CreatedAt)
		if e != nil {
			return e
		}
		if !resolved {
			e = s.Bot.Call(ctx, "sendMessage", map[string]any{"chat_id": l.ChatID, "parse_mode": "HTML", "text": fmt.Sprintf("<a href=\"tg://user?id=%d\">用户 %d</a> 累计违规超过 3 次，原消息已删除，已禁言并记录，等待管理员处理。\n管理员可使用 /ban %d 封禁，或 /unmute %d 解除禁言。", l.UserID, l.UserID, l.UserID, l.UserID)}, nil)

			if e != nil {
				return e
			}
		}
	}
	if e = s.Store.PunishmentStep(ctx, l.EventKey, "done"); e != nil {
		return e
	}
	s.LogChannel(ctx, l.ChatID, fmt.Sprintf("用户 %d，操作 %s，风险 %d，原因 %s", l.UserID, d.Action, l.Risk.Score, d.Reason))
	return nil
}

func (s *Service) Keywords(ctx context.Context, m domain.Message, event string) error {
	if ok, e := s.Store.GroupAuthorized(ctx, m.Chat.ID); e != nil || !ok {
		return e
	}
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
	if ok, e := s.Store.GroupAuthorized(ctx, m.Chat.ID); e != nil || !ok {
		return e
	}
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
	decision := domain.Decision{Action: "shadow_log", Reason: "manual_review"}
	if a.IsAd {
		decision = domain.Decision{Action: "warn", Delete: true, Reason: "manual_ai_ad"}
	}
	l, e := s.Store.SaveLog(ctx, store.Log{EventKey: fmt.Sprintf("review:%d:%d", m.Chat.ID, target.ID), ChatID: m.Chat.ID, UserID: n.UserID, MessageID: n.MessageID, Text: target.ModerationText(), Risk: r, AI: &a, Decision: decision, Source: "review"})
	if e != nil {
		return e
	}
	if e = s.Punish(ctx, l, m.From.ID); e != nil {
		return e
	}
	outcome, e := s.Store.ReviewOutcome(ctx, l)
	if e != nil {
		return e
	}
	// The successful warning/repeat-mute already delivered the complete review card.
	if outcome.EventKey == l.EventKey && outcome.Status == "done" && (outcome.Decision.Action == "warn" || outcome.Decision.Reason == "repeated_ad") {
		return nil
	}
	u := domain.User{}
	if target.From != nil {
		u = *target.From
	}
	return s.sendReviewNotice(ctx, l, u, settings, outcome, m.ID)
}
