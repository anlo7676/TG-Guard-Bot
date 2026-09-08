package service

import (
	"context"
	"fmt"
	"strings"
	"tgguard/internal/domain"
	"tgguard/internal/store"
)

func menuSectionLabel(section string) string {
	return map[string]string{"home": "群管理", "settings": "群设置", "rules": "审核规则", "stats": "群统计", "keywords": "关键词回复", "white": "白名单", "black": "黑名单", "trusted": "可信用户"}[section]
}
func validMenuSection(section string) bool { return menuSectionLabel(section) != "" }
func (s *Service) commandMenuLink(ctx context.Context, m domain.Message, section, text string) error {
	rows := [][]menuButton{{{"text": "管理" + menuSectionLabel(section), "url": fmt.Sprintf("https://t.me/%s?start=group_%d_%s", s.Bot.Username, m.Chat.ID, section)}}}
	return s.groupMenuSend(ctx, m.Chat.ID, text+"\n\n点击下方按钮，在私聊中查看和管理本群。", rows)
}
func rulesSummary(v domain.Settings) string {
	text := fmt.Sprintf("本群审核规则\n消息审核：%s\nAI 审核：%s（触发风险分 %d）\n直接处理风险分：%d\n", displayValue(v.ModerationEnabled), displayValue(v.AIEnabled), v.AIThreshold, v.DirectThreshold)
	for _, r := range menuRules {
		value := ruleValue(v, r.Key)
		text += fmt.Sprintf("\n• %s：%s · %d 分", r.Label, displayValue(value.Enabled), value.Score) + " · " + ruleActionLabel(value.Action)
	}
	return text + fmt.Sprintf("\n自定义广告规则：%d 条。点击按钮设置命中动作。", len(v.AdRules))
}
func (s *Service) statsSummary(ctx context.Context, chat int64) (string, error) {
	r, e := s.Store.GroupStatistics(ctx, chat)
	if e != nil {
		return "", e
	}
	return fmt.Sprintf("本群统计\n已记录在群成员：%d\n累计审核消息：%d\n已完成处罚：%d\n成员数仅统计机器人已记录的成员。", r.KnownMembers, r.ReviewedMessages, r.Punishments), nil
}
func commandExcerpt(value string, limit int) string {
	v := []rune(strings.Join(strings.Fields(value), " "))
	if len(v) > limit {
		return string(v[:limit]) + "…"
	}
	return string(v)
}
func (s *Service) keywordSummary(ctx context.Context, m domain.Message) error {
	ks, e := s.Store.Keywords(ctx, m.Chat.ID)
	if e != nil {
		return e
	}
	text := fmt.Sprintf("本群关键词回复 · %d 条", len(ks))
	if len(ks) == 0 {
		text += "\n暂无关键词，点击下方按钮新增。"
	}
	modes := map[string]string{"contains": "包含", "exact": "完全匹配", "regex": "正则匹配"}
	for i, k := range ks {
		if i >= 8 {
			text += "\n更多规则请点击下方按钮查看。"
			break
		}
		mode := modes[k.MatchType]
		if mode == "" {
			mode = "其他匹配"
		}
		text += fmt.Sprintf("\n• #%d %s · %s · %s", k.ID, commandExcerpt(k.Keyword, 60), mode, displayValue(k.Enabled))
	}
	return s.commandMenuLink(ctx, m, "keywords", text)
}
func (s *Service) listSummary(ctx context.Context, m domain.Message, kind string) error {
	rows, e := s.Store.View(ctx, store.ViewListSummary, m.Chat.ID, kind)
	if e != nil {
		return e
	}
	text := "本群" + menuSectionLabel(kind)
	if len(rows) == 0 {
		text += "\n暂无记录，点击下方按钮添加。"
	}
	for i, r := range rows {
		if i >= 20 {
			text += "\n更多记录请点击下方按钮查看。"
			break
		}
		name := fmt.Sprint(r["username"])
		if name != "" && name != "<nil>" {
			text += "\n• @" + commandExcerpt(name, 40)
		} else {
			text += fmt.Sprintf("\n• 用户 ID：%v", r["user_id"])
		}
	}
	return s.commandMenuLink(ctx, m, kind, text)
}
