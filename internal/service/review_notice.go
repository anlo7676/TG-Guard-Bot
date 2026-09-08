package service

import (
	"context"
	"fmt"
	"html"
	"strings"

	"tgguard/internal/domain"
	"tgguard/internal/store"
)

func reviewNotice(l store.Log, u domain.User, settings domain.Settings, outcome store.Punishment) string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if u.Username != "" {
		name = "@" + u.Username
	}
	if name == "" {
		name = fmt.Sprintf("用户 %d", l.UserID)
	}
	mention := fmt.Sprintf("<a href=\"tg://user?id=%d\">%s</a>", l.UserID, html.EscapeString(name))
	verdict := "未发现广告"
	if l.AI != nil && l.AI.IsAd {
		verdict = "判定为广告"
	}
	lines := []string{"<b>AI 审核 · " + verdict + "</b>", mention}
	if a := l.AI; a != nil {
		category := map[string]string{"normal": "正常内容", "promotion": "广告推广", "crypto": "虚拟币推广", "gambling": "博彩推广", "porn": "色情推广", "recruitment": "招聘招揽", "scam": "诈骗引流", "traffic_diversion": "引流推广", "external_group": "外部群组推广", "financial": "金融推广", "unknown": "其他类型"}[a.Category]
		if category == "" {
			category = "其他类型"
		}
		lines = append(lines, fmt.Sprintf("类型：%s · 置信度 %.0f%%", category, a.Confidence*100))
		reason := []rune(strings.TrimSpace(a.Reason))
		if len(reason) > 500 {
			reason = append(reason[:500], '…')
		}
		lines = append(lines, "依据："+html.EscapeString(string(reason)))
	}
	status := reviewOutcomeText(outcome)
	if outcome.Status == "done" && outcome.Decision.Action == "warn" && outcome.Decision.Delete {
		status = "原消息已删除，并发出警告。"
		lines = append(lines, "", "处理："+status, "请遵守群规则；再次发送相同内容将删除并禁言 "+warningDuration(settings.MuteSeconds)+"。")
	} else {
		if outcome.Status == "done" && outcome.Decision.Action == "mute" && outcome.Decision.Delete {
			status = "原消息已删除，已禁言 " + warningDuration(outcome.Decision.Duration) + "。"
		}
		lines = append(lines, "", "处理："+html.EscapeString(status))
	}
	return strings.Join(append(lines, "", "下方操作仅限管理员；标记误判不会恢复已删除的消息或自动解禁。"), "\n")
}

func (s *Service) sendReviewNotice(ctx context.Context, l store.Log, u domain.User, settings domain.Settings, outcome store.Punishment, reply int64) error {
	markup, err := s.ReviewButtons(ctx, l, outcome)
	if err != nil {
		return err
	}
	in := map[string]any{"chat_id": l.ChatID, "text": reviewNotice(l, u, settings, outcome), "parse_mode": "HTML", "reply_markup": markup, "link_preview_options": map[string]bool{"is_disabled": true}}
	if reply > 0 {
		in["reply_parameters"] = map[string]any{"message_id": reply, "allow_sending_without_reply": true}
	}
	return s.Bot.Call(ctx, "sendMessage", in, nil)
}
