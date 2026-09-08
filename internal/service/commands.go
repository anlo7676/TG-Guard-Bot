package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"tgguard/internal/buildinfo"
	"time"
	"unicode"

	"tgguard/internal/domain"
	"tgguard/internal/store"
)

func ParseCommand(text, username string) (command, arg string, ok bool) {
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	head, tail := text, ""
	if i := strings.IndexFunc(text, unicode.IsSpace); i >= 0 {
		head, tail = text[:i], text[i:]
	}
	name, target, has := strings.Cut(strings.TrimPrefix(head, "/"), "@")
	if has && !strings.EqualFold(target, username) {
		return "", "", false
	}
	return strings.ToLower(name), strings.TrimSpace(tail), true
}
func (s *Service) Command(ctx context.Context, update int64, m domain.Message, command, arg string) error {
	if m.From == nil || command == "help" {
		return nil
	}
	if m.Chat.Type == "private" && (command == "approve" || command == "reject" || command == "revoke") {
		return s.authorizationCommand(ctx, m, command, arg)
	}
	if m.Chat.Type == "supergroup" {
		if ok, e := s.Store.GroupAuthorized(ctx, m.Chat.ID); e != nil {
			return e
		} else if !ok {
			return s.AuthorizationNotice(ctx, m.Chat.ID)
		}
	}
	if command == "start" && strings.HasPrefix(arg, "verify_") {
		return s.StartVerification(ctx, m, strings.TrimPrefix(arg, "verify_"))
	}
	lang := "zh_CN"
	if m.Chat.Type == "private" {
		if command == "start" && arg == "help" {
			return s.PrivateSection(ctx, m, "help")
		}
		if command == "start" && strings.HasPrefix(arg, "group_") {
			target := strings.SplitN(strings.TrimPrefix(arg, "group_"), "_", 2)
			section := "home"
			if len(target) == 2 {
				section = target[1]
			}
			if !validMenuSection(section) {
				return s.text(ctx, m.Chat.ID, "该功能链接无效，请发送 /menu 重新打开。")
			}
			return s.GroupMenu(ctx, m, "gm:"+target[0]+":"+section)
		}
		switch command {
		case "start", "menu":
			return s.Home(ctx, m)
		case "verify":
			return s.text(ctx, m.Chat.ID, "新人验证无需发送命令。请回到群内，点击入群提示中的验证按钮，再按私聊题目提示完成验证。按钮失效或找不到提示时，请联系群管理员。")
		case "cancel":
			return s.CancelGroupInput(ctx, m)
		case "groups":
			return s.MyGroups(ctx, m, 0)
		case "settings", "rules", "stats", "keywords", "whitelist", "blacklist":
			section := command
			if command == "whitelist" {
				section = "white"
			}
			if command == "blacklist" {
				section = "black"
			}
			return s.MyGroups(ctx, m, 0, section)
		case "panel":
			return s.PrivateSection(ctx, m, "panel")
		case "admins":
			return s.PrivateSection(ctx, m, "admins")
		case "ai":
			return s.PrivateSection(ctx, m, "ai")
		}
	}
	switch command {
	case "dc":
		return s.dataCenter(ctx, m, arg)
	case "version":
		return s.text(ctx, m.Chat.ID, buildinfo.Label())
	case "start":
		if m.Chat.Type == "supergroup" {
			return s.GroupHelp(ctx, m)
		}
		return s.Say(ctx, m.Chat.ID, lang, "help")
	case "id":
		if m.Chat.Type == "supergroup" {
			allowed, e := s.Admin(ctx, m.Chat.ID, m.From.ID)
			if e != nil {
				return e
			}
			if !allowed {
				return s.text(ctx, m.Chat.ID, "群内 ID 查询供管理员使用。如需查看自己的 ID，请私聊机器人发送 /id。")
			}
		}
		text := fmt.Sprintf("群／会话 ID：%d\n你的用户 ID：%d\n消息 ID：%d", m.Chat.ID, m.From.ID, m.ID)
		if m.Reply != nil && m.Reply.From != nil {
			text += fmt.Sprintf("\n目标用户 ID：%d\n目标消息 ID：%d", m.Reply.From.ID, m.Reply.ID)
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
		return s.text(ctx, m.Chat.ID, "新人验证无需在群里发送命令。请点击入群提示中的验证按钮，在私聊中完成验证；验证链接失效时联系群管理员。")
	}
	admin, e := s.Admin(ctx, m.Chat.ID, m.From.ID)
	if e != nil {
		return e
	}
	if !admin {
		return s.Say(ctx, m.Chat.ID, lang, "denied")
	}
	switch command {
	case "menu":
		return s.GroupMenuLink(ctx, m)
	case "settings":
		if arg == "" {
			return s.GroupMenuLink(ctx, m)
		}
		if e = domain.ApplySettingsPatch(&settings, []byte(arg)); e != nil {
			return s.text(ctx, m.Chat.ID, "设置未保存："+e.Error())
		}
		if e = s.Store.ChangeSettings(ctx, m.Chat.ID, m.From.ID, func(v *domain.Settings) error { return domain.ApplySettingsPatch(v, []byte(arg)) }); e != nil {
			return e
		}

	case "rules":
		return s.commandMenuLink(ctx, m, "rules", rulesSummary(settings))
	case "stats":
		text, e := s.statsSummary(ctx, m.Chat.ID)
		if e != nil {
			return e
		}
		return s.commandMenuLink(ctx, m, "stats", text)
	case "whitelist", "blacklist":
		kind := "white"
		if command == "blacklist" {
			kind = "black"
		}
		args := strings.Fields(arg)
		if len(args) == 0 {
			return s.listSummary(ctx, m, kind)
		}
		if args[0] != "add" && args[0] != "remove" {
			return s.text(ctx, m.Chat.ID, "用法：/"+command+" add|remove 数字用户 ID，也可回复用户消息；旧用户名条目仅可删除。")
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
		if target == 0 {
			fields := strings.Fields(arg)
			if len(fields) > 0 {
				target, _ = strconv.ParseInt(fields[0], 10, 64)
				arg = strings.Join(fields[1:], " ")
			}
			if command != "mute" && arg != "" {
				return s.text(ctx, m.Chat.ID, "格式：/"+command+" 用户ID；或回复目标用户的消息。")
			}
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
		if protected && command != "unmute" && command != "unban" {
			return s.Say(ctx, m.Chat.ID, lang, "denied")
		}
		if e = s.Punish(ctx, store.Log{EventKey: eventKey(update, "command"), ChatID: m.Chat.ID, UserID: target, MessageID: message, Decision: domain.Decision{Action: command, Duration: duration, Reason: "administrator_command"}, Source: "manual"}, m.From.ID); e != nil {
			return e
		}
	default:
		return s.Say(ctx, m.Chat.ID, lang, "unknown_command")
	}
	if command == "ban" {
		return s.text(ctx, m.Chat.ID, "封禁操作已完成，已请求 Telegram 同时清理该用户在本群的全部发言。")
	}
	return s.Say(ctx, m.Chat.ID, lang, "done")
}
func (s *Service) text(ctx context.Context, chat int64, text string) error {
	_, e := s.Bot.Send(ctx, chat, text, nil, 0)
	return e
}
func (s *Service) keywordCommand(ctx context.Context, m domain.Message, arg string) error {
	if arg == "" {
		return s.keywordSummary(ctx, m)
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
	return s.text(ctx, m.Chat.ID, "用法：/keywords add contains 官网 | 我们的官网是 https://example.com\n删除：/keywords del 规则ID\n发送 /keywords 点击「管理关键词回复」，可在私聊中新增、编辑、启停和删除。")
}
