package service

import (
	"fmt"
	"html"
	"strings"
	"tgguard/internal/domain"
	"tgguard/internal/store"
)

// warningNotice describes current policy; fixed local actions do not escalate by count.
func warningNotice(l store.Log, u domain.User, s domain.Settings, completed int) string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if u.Username != "" {
		name = "@" + u.Username
	}
	if name == "" {
		name = fmt.Sprintf("用户 %d", l.UserID)
	}
	mention := fmt.Sprintf("<a href=\"tg://user?id=%d\">%s</a>", l.UserID, html.EscapeString(name))
	source := "本地风险规则"
	if l.Risk.Spam {
		source = "本地刷屏／重复消息检测"
	} else if l.AI != nil && l.Risk.LocalAction == "" {
		source = fmt.Sprintf("AI 复核判定广告（置信度 %.0f%%）", l.AI.Confidence*100)
	}
	if l.Source != "automatic" {
		source = "管理员人工警告"
	}
	action := "本次处理：警告。"
	if l.Decision.Delete {
		action = "本次处理：原消息已删除，并发出警告。"
	}
	lines := []string{mention + "，请遵守群规则。", "判断来源：" + source + "。", action}
	if l.Source == "automatic" {
		lines = append(lines, fmt.Sprintf("本群累计自动处罚：第 %d 次（不按天清零）。", completed+1))
	}
	if l.Risk.LocalAction == "delete" {
		lines = append(lines, "命中本地广告规则；继续命中此规则仍会删除消息并警告，不会仅因本规则累计次数自动禁言或封禁。")
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "按当前群设置，再次达到累计策略的违规处罚条件时：")
	if s.AutoMute {
		lines = append(lines, fmt.Sprintf("• 累计第 %d 次及以后禁言 %s。", s.MuteAfter, warningDuration(s.MuteSeconds)))
	} else {
		lines = append(lines, "• 未开启累计自动禁言。")
	}
	if s.AutoBan {
		lines = append(lines, fmt.Sprintf("• 累计第 %d 次及以后封禁并清理发言（优先于禁言）。", s.BanAfter))
	} else {
		lines = append(lines, "• 未开启累计自动封禁。")
	}
	lines = append(lines, "单条广告规则指定的删除／禁言／封禁会直接执行；删除并警告规则不会因重复命中自动升级。低置信度警告不自动升级。")
	if s.AutoMute {
		lines = append(lines, "高置信度严重广告可能提前禁言。")
	}
	return strings.Join(lines, "\n")
}
func warningDuration(seconds int) string {
	if seconds%3600 == 0 {
		return fmt.Sprintf("%d 小时", seconds/3600)
	}
	if seconds%60 == 0 {
		return fmt.Sprintf("%d 分钟", seconds/60)
	}
	return fmt.Sprintf("%d 秒", seconds)
}
