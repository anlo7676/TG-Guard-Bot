package service

func ruleActionLabel(a string) string {
	if a == "" {
		return "累计评分"
	}
	return map[string]string{"delete": "删除并警告", "mute": "删除并禁言", "ban": "封禁并清理发言"}[a]
}
