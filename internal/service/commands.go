package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"tgguard/internal/domain"
	"tgguard/internal/store"
)

func ParseCommand(text, username string) (command, arg string, ok bool) {
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	head, tail, _ := strings.Cut(text, " ")
	name, target, has := strings.Cut(strings.TrimPrefix(head, "/"), "@")
	if has && !strings.EqualFold(target, username) {
		return "", "", false
	}
	return strings.ToLower(name), strings.TrimSpace(tail), true
}
func (s *Service) Command(ctx context.Context, update int64, m domain.Message, command, arg string) error {
	if m.From == nil {
		return nil
	}
	if command == "start" && strings.HasPrefix(arg, "verify_") {
		return s.StartVerification(ctx, m, strings.TrimPrefix(arg, "verify_"))
	}
	lang := "zh_CN"
	if m.Chat.Type == "private" {
		switch command {
		case "start", "menu":
			return s.Home(ctx, m)
		case "help":
			return s.PrivateSection(ctx, m, "help")
		case "panel", "settings":
			return s.PrivateSection(ctx, m, "panel")
		case "admins":
			return s.PrivateSection(ctx, m, "admins")
		case "ai":
			return s.PrivateSection(ctx, m, "ai")
		}
	}
	switch command {
	case "help", "start":
		return s.Say(ctx, m.Chat.ID, lang, "help")
	case "id":
		text := fmt.Sprintf("chat_id: %d\nuser_id: %d\nmessage_id: %d", m.Chat.ID, m.From.ID, m.ID)
		if m.Reply != nil && m.Reply.From != nil {
			text += fmt.Sprintf("\nreply_user_id: %d\nreply_message_id: %d", m.Reply.From.ID, m.Reply.ID)
		}
		_, e := s.Bot.Send(ctx, m.Chat.ID, text, nil, 0)
		return e
	}
	if m.Chat.Type != "supergroup" {
		return s.Say(ctx, m.Chat.ID, lang, "help")
	}
	if e := s.Group(ctx, m.Chat); e != nil {
		return e
	}
	settings, e := s.Store.Settings(ctx, m.Chat.ID)
	if e != nil {
		return e
	}
	lang = settings.Language
	if command == "check" || command == "ai" {
		return s.Review(ctx, update, m)
	}
	if command == "verify" {
		v, e := s.Store.ActiveVerification(ctx, m.Chat.ID, m.From.ID)
		if e == sql.ErrNoRows {
			_, e = s.Bot.Send(ctx, m.Chat.ID, "没有进行中的验证。", nil, m.ID)
			return e
		}
		if e != nil {
			return e
		}
		_, e = s.Bot.Send(ctx, m.Chat.ID, fmt.Sprintf("验证状态：%s，截止时间：%s", v.Status, v.ExpiresAt.Format(time.RFC3339)), nil, m.ID)
		return e
	}
	admin, e := s.Admin(ctx, m.Chat.ID, m.From.ID)
	if e != nil {
		return e
	}
	if !admin {
		return s.Say(ctx, m.Chat.ID, lang, "denied")
	}
	switch command {
	case "settings":
		if arg == "" {
			return s.sendJSON(ctx, m.Chat.ID, settings)
		}
		d := json.NewDecoder(strings.NewReader(arg))
		d.DisallowUnknownFields()
		if e = d.Decode(&settings); e != nil {
			return s.text(ctx, m.Chat.ID, "用法：/settings {\"ai_enabled\":true}，仅覆盖提供的字段。")
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			return s.text(ctx, m.Chat.ID, "只允许提供一个 JSON 对象。")
		}
		if e = settings.Validate(); e != nil {
			return s.text(ctx, m.Chat.ID, e.Error())
		}
		if e = s.Store.SaveSettings(ctx, m.Chat.ID, m.From.ID, settings); e != nil {
			return e
		}
	case "rules":
		return s.sendJSON(ctx, m.Chat.ID, map[string]any{"defaults": map[string]int{"url": 20, "telegram_link": 40, "contact": 25, "mention": 20, "advertising": 25, "gambling": 35, "porn": 35, "crypto": 20, "many_links": 25, "emoji": 15}, "overrides": settings.Rules, "usage": "/settings {\"rules\":{\"url\":{\"enabled\":false,\"score\":20}}}"})
	case "stats":
		rows, e := s.Store.Rows(ctx, "SELECT (SELECT COUNT(*) FROM group_members WHERE chat_id=? AND left_at IS NULL) AS known_members,(SELECT COUNT(*) FROM moderation_logs WHERE chat_id=?) AS reviewed_messages,(SELECT COUNT(*) FROM punishments WHERE chat_id=? AND status='done') AS punishments", m.Chat.ID, m.Chat.ID, m.Chat.ID)
		if e != nil {
			return e
		}
		return s.sendJSON(ctx, m.Chat.ID, rows)
	case "whitelist", "blacklist":
		kind := "white"
		if command == "blacklist" {
			kind = "black"
		}
		args := strings.Fields(arg)
		if len(args) == 0 {
			rows, e := s.Store.Rows(ctx, "SELECT user_id,username,kind,reason,expires_at FROM list_entries WHERE chat_id=? AND kind=? ORDER BY id DESC LIMIT 20", m.Chat.ID, kind)
			if e != nil {
				return e
			}
			return s.sendJSON(ctx, m.Chat.ID, rows)
		}
		if args[0] != "add" && args[0] != "remove" {
			return s.text(ctx, m.Chat.ID, "用法：/"+command+" add|remove 用户ID或@username，也可回复用户消息。")
		}
		l := domain.ListEntry{ChatID: m.Chat.ID, Kind: kind, Reason: "Telegram 管理员命令"}
		if len(args) > 1 {
			if strings.HasPrefix(args[1], "@") {
				l.Username = strings.TrimPrefix(args[1], "@")
			} else {
				l.UserID, _ = strconv.ParseInt(args[1], 10, 64)
			}
		} else if m.Reply != nil && m.Reply.From != nil {
			l.UserID = m.Reply.From.ID
		}
		if e = l.Validate(); e != nil {
			return s.text(ctx, m.Chat.ID, e.Error())
		}
		if e = s.Store.SaveList(ctx, l, m.From.ID, args[0] == "remove"); e != nil {
			return e
		}
	case "keywords":
		return s.keywordCommand(ctx, m, arg)
	case "warn", "mute", "ban", "unmute", "unban", "kick":
		var target int64
		var message int64
		if m.Reply != nil && m.Reply.From != nil && m.Reply.SenderChat == nil {
			target = m.Reply.From.ID
			message = m.Reply.ID
		}
		duration := settings.MuteSeconds
		if command == "unban" && target == 0 {
			target, _ = strconv.ParseInt(arg, 10, 64)
		}
		if target <= 0 {
			return s.Say(ctx, m.Chat.ID, lang, "reply_required")
		}
		if command == "mute" && arg != "" {
			d, e := time.ParseDuration(arg)
			if e != nil || d < 30*time.Second || d > 366*24*time.Hour {
				return s.text(ctx, m.Chat.ID, "禁言时长需为 30s 至 366 天，例如 /mute 1h")
			}
			duration = int(d.Seconds())
		}
		tm, e := s.Bot.Member(ctx, m.Chat.ID, target)
		if e != nil {
			return e
		}
		protected, e := s.Protected(ctx, m.Chat.ID, tm.User)
		if e != nil {
			return e
		}
		if protected {
			return s.Say(ctx, m.Chat.ID, lang, "denied")
		}
		if e = s.Punish(ctx, store.Log{EventKey: eventKey(update, "command"), ChatID: m.Chat.ID, UserID: target, MessageID: message, Decision: domain.Decision{Action: command, Duration: duration, Reason: "administrator_command"}, Source: "manual"}, m.From.ID); e != nil {
			return e
		}
	default:
		return s.Say(ctx, m.Chat.ID, lang, "unknown_command")
	}
	return s.Say(ctx, m.Chat.ID, lang, "done")
}
func (s *Service) sendJSON(ctx context.Context, chat int64, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	r := []rune(string(b))
	if len(r) > 3900 {
		r = append(r[:3800], []rune("\n…结果较多，请通过管理 API 查看。")...)
	}
	return s.text(ctx, chat, string(r))
}
func (s *Service) text(ctx context.Context, chat int64, text string) error {
	_, e := s.Bot.Send(ctx, chat, text, nil, 0)
	return e
}
func (s *Service) keywordCommand(ctx context.Context, m domain.Message, arg string) error {
	if arg == "" {
		ks, e := s.Store.Keywords(ctx, m.Chat.ID)
		if e != nil {
			return e
		}
		return s.sendJSON(ctx, m.Chat.ID, ks)
	}
	if strings.HasPrefix(arg, "del ") {
		id, e := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(arg, "del ")), 10, 64)
		if e != nil {
			return s.Say(ctx, m.Chat.ID, "zh_CN", "unknown_command")
		}
		if e = s.Store.DeleteKeyword(ctx, m.Chat.ID, id, m.From.ID); e != nil {
			return e
		}
		return s.Say(ctx, m.Chat.ID, "zh_CN", "done")
	}
	if strings.HasPrefix(arg, "add ") {
		left, content, ok := strings.Cut(strings.TrimPrefix(arg, "add "), "|")
		mode, word, _ := strings.Cut(strings.TrimSpace(left), " ")
		if ok {
			k := domain.Keyword{ChatID: m.Chat.ID, Keyword: strings.TrimSpace(word), MatchType: mode, ReplyType: "text", Content: strings.TrimSpace(content), Enabled: true, Reply: true}
			if e := k.Validate(); e != nil {
				return s.text(ctx, m.Chat.ID, e.Error())
			}
			id, e := s.Store.SaveKeyword(ctx, k, m.From.ID)
			if e != nil {
				return e
			}
			return s.text(ctx, m.Chat.ID, fmt.Sprintf("关键词已创建：%d", id))
		}
	}
	return s.text(ctx, m.Chat.ID, "用法：/keywords add contains 官网 | 我们的官网是 https://example.com\n删除：/keywords del 规则ID\n高级回复类型、优先级和启停请使用管理 API。")
}
