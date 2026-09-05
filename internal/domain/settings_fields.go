package domain

// SettingFields describes editable group settings. Validation remains authoritative in Settings.Validate.
type SettingField struct {
	Key, Label, Section, Kind string
	Choices                   []string
}

var SettingFields = []SettingField{
	{"verification_enabled", "开启新人验证", "verify", "bool", nil}, {"verification_type", "验证方式", "verify", "enum", []string{"math", "button"}}, {"verification_timeout", "验证时限（30–3600 秒）", "verify", "number", nil}, {"verification_fail_action", "验证失败操作", "verify", "enum", []string{"kick", "ban", "mute"}},
	{"moderation_enabled", "内容审核", "review", "bool", nil}, {"ai_enabled", "AI 辅助审核", "review", "bool", nil}, {"ai_threshold", "AI 触发风险分（0–100）", "review", "number", nil}, {"direct_threshold", "直接处理风险分（0–100）", "review", "number", nil}, {"ai_warn_confidence", "AI 警告置信度（0–1）", "review", "number", nil}, {"ai_delete_confidence", "AI 删除置信度（0–1）", "review", "number", nil}, {"ai_mute_confidence", "AI 禁言置信度（0–1）", "review", "number", nil}, {"review_access", "人工 AI 查询权限", "review", "enum", []string{"all", "admin", "trusted"}}, {"admin_bypass", "管理员免分析", "review", "bool", nil}, {"new_member_protection", "新成员风险加权", "review", "bool", nil},
	{"spam_enabled", "防刷屏", "spam", "bool", nil}, {"rate_limit", "窗口消息上限（2–100）", "spam", "number", nil}, {"rate_window", "频率窗口（1–300 秒）", "spam", "number", nil}, {"duplicate_limit", "一分钟重复次数（2–50）", "spam", "number", nil},
	{"auto_delete", "自动删除", "punish", "bool", nil}, {"auto_warn", "自动警告", "punish", "bool", nil}, {"auto_mute", "自动禁言", "punish", "bool", nil}, {"auto_ban", "自动封禁", "punish", "bool", nil}, {"mute_seconds", "禁言时长（30–31622400 秒）", "punish", "number", nil}, {"mute_after", "开始禁言的累计次数", "punish", "number", nil}, {"ban_after", "开始封禁的累计次数", "punish", "number", nil},
	{"keyword_enabled", "关键词回复", "other", "bool", nil}, {"keyword_all", "回复所有命中关键词", "other", "bool", nil}, {"log_channel", "日志群／频道 ID（0 关闭）", "other", "number", nil}, {"language", "提示语言", "other", "enum", []string{"zh_CN", "en_US"}},
}

func FindSetting(key string) (SettingField, bool) {
	for _, f := range SettingFields {
		if f.Key == key {
			return f, true
		}
	}
	return SettingField{}, false
}
