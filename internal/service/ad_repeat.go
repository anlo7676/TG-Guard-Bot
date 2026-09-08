package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"tgguard/internal/domain"
	"tgguard/internal/rules"
	"tgguard/internal/store"
)

func adFingerprint(text string) string {
	text = rules.Clean(text)
	if text == "" {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
}

func (s *Service) repeatAdDecision(ctx context.Context, l store.Log, settings domain.Settings) (domain.Decision, error) {
	repeat, err := s.Store.RepeatedAd(ctx, l.ChatID, l.UserID, l.MessageID, adFingerprint(l.Text))
	if err != nil || !repeat || l.Decision.Action == "ban" || l.Decision.Reason == "local_ad_review" {
		return l.Decision, err
	}
	return domain.Decision{Action: "mute", Delete: true, Duration: settings.MuteSeconds, Reason: "repeated_ad"}, nil
}

func reviewOutcomeText(p store.Punishment) string {
	switch p.Status {
	case "":
		return "未执行处罚"
	case "skipped":
		return "目标受保护或操作已被后续管理操作替代，未执行本次处罚"
	case "done":
		switch p.Decision.Action {
		case "warn":
			if p.Decision.Delete {
				return "原消息已删除并警告；再次发送相同内容将禁言"
			}
			return "已警告"
		case "mute":
			return "原消息已删除并禁言"
		case "ban":
			return "用户已封禁"
		default:
			return "原消息已处理，请查看处罚记录"
		}
	default:
		return "处罚未完成，请查看处罚记录"
	}
}
