package rules

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"

	"tgguard/internal/domain"
)

var urlRE = regexp.MustCompile(`(?i)(?:https?://|www\.|(?:t|telegram)\.me/)[^\s<>]+`)
var mentionRE = regexp.MustCompile(`@[a-zA-Z0-9_]{5,32}`)
var tgRE = regexp.MustCompile(`(?i)(?:t|telegram)\.me/|tg://join|joinchat`)
var contactRE = regexp.MustCompile(`(?i)whatsapp|微信|加微|\bqq\b|[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}|(?:\+?\d[\s\-]?){10,15}`)
var walletRE = regexp.MustCompile(`\b(?:0x[a-fA-F0-9]{40}|T[1-9A-HJ-NP-Za-km-z]{33})\b`)

func Clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		if r >= 0xff01 && r <= 0xff5e {
			r -= 0xfee0
		}
		return unicode.ToLower(r)
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func Normalize(m domain.Message) domain.Normalized {
	n := domain.Normalized{ChatID: m.Chat.ID, MessageID: m.ID, Text: Clean(m.Body()), URLs: []string{}, Mentions: []string{}}
	// Base58 wallet addresses are case-sensitive; detect before lowercasing text.
	n.HasWallet = walletRE.MatchString(m.Body())
	if m.From != nil {
		n.UserID = m.From.ID
		n.Username = m.From.Username
	}
	letters, upper := 0, 0
	for _, c := range m.Text + m.Caption {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			letters++
			if c >= 'A' && c <= 'Z' {
				upper++
			}
		}
	}
	if letters >= 15 {
		n.UppercaseRatio = float64(upper) / float64(letters)
	}
	n.URLs = append(n.URLs, urlRE.FindAllString(n.Text, -1)...)
	n.Mentions = append(n.Mentions, mentionRE.FindAllString(n.Text, -1)...)
	collect := func(text string, entities []domain.Entity) {
		units := utf16.Encode([]rune(text))
		for _, e := range entities {
			if e.Type == "text_link" && e.URL != "" {
				n.URLs = append(n.URLs, Clean(e.URL))
			}
			if e.Type == "url" && e.Offset >= 0 && e.Length > 0 && e.Offset <= len(units) && e.Length <= len(units)-e.Offset {
				n.URLs = append(n.URLs, Clean(string(utf16.Decode(units[e.Offset:e.Offset+e.Length]))))
			}
		}
	}
	collect(m.Text, m.Entities)
	collect(m.Caption, m.CaptionEntities)
	n.URLs = unique(n.URLs)
	n.Mentions = unique(n.Mentions)
	switch {
	case len(m.Photo) > 0:
		n.MediaType = "photo"
	case len(m.Video) > 0:
		n.MediaType = "video"
	case len(m.Document) > 0:
		n.MediaType = "document"
	case len(m.Sticker) > 0:
		n.MediaType = "sticker"
	default:
		n.MediaType = "text"
	}
	return n
}
func unique(a []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range a {
		if !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	return out
}
func contains(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

func Evaluate(n domain.Normalized, s domain.Settings) domain.Risk {
	r := domain.Risk{Matches: []domain.Match{}}
	add := func(name string, matched bool, score int, reason string) {
		if o, ok := s.Rules[name]; ok {
			matched = matched && o.Enabled
			score = o.Score
		}
		if matched {
			r.Score += score
			r.Matches = append(r.Matches, domain.Match{Rule: name, Score: score, Reason: reason})
			r.LocalAction = strongerAction(r.LocalAction, domain.EffectiveRule(s, name).Action)
		}
	}
	joined := n.Text + " " + strings.Join(n.URLs, " ")
	for _, p := range compiledAdPresets {
		add(p.rule.Key, p.regex.MatchString(joined), p.rule.Score, p.rule.Label)
	}
	add("url", len(n.URLs) > 0, 20, "外部链接")
	add("telegram_link", tgRE.MatchString(joined), 40, "Telegram 引流链接")
	contact := contactRE.MatchString(n.Text)
	add("contact", contact, 25, "联系方式")
	add("mention", len(n.Mentions) > 0 && contains(n.Text, "联系", "私聊", "咨询", "contact", "dm"), 20, "用户名引流")
	ad := contains(n.Text, "代理", "推广", "包赔", "稳赚", "日赚", "兼职", "接单", "带单", "私聊", "收益翻倍", "guaranteed profit", "dm me")
	add("advertising", ad, 25, "广告招揽词")
	add("gambling", contains(n.Text, "博彩", "投注", "棋牌", "赌场", "casino", "betting"), 35, "博彩推广词")
	add("porn", contains(n.Text, "裸聊", "成人视频", "约炮", "成人视频"), 35, "色情推广词")
	add("crypto", ad && (n.HasWallet || walletRE.MatchString(n.Text) || contains(n.Text, "usdt", "trc20", "erc20", "换u", "充值", "兑换")), 20, "加密货币招揽")
	add("many_links", len(n.URLs) >= 3, 25, "大量链接")
	add("caps", n.UppercaseRatio >= .8, 15, "大量大写字符")
	emoji := 0
	for _, c := range n.Text {
		if c >= 0x1f300 && c <= 0x1faff {
			emoji++
		}
	}
	add("emoji", emoji >= 15, 15, "大量 Emoji")
	if s.NewMemberProtection && n.IsNew && r.Score > 0 {
		r.Score += 10
		r.Matches = append(r.Matches, domain.Match{Rule: "new_member", Score: 10, Reason: "入群不足十分钟"})
	}
	if s.NewMemberProtection && n.FirstMessage && len(n.URLs) > 0 && r.Score > 0 {
		r.Score += 30
		r.Matches = append(r.Matches, domain.Match{Rule: "first_link", Score: 30, Reason: "首条消息含链接"})
	}
	if s.NewMemberProtection && n.FirstMessage && contact && r.Score > 0 {
		r.Score += 30
		r.Matches = append(r.Matches, domain.Match{Rule: "first_contact", Score: 30, Reason: "首条消息含联系方式"})
	}
	for i, rule := range s.AdRules {
		if !rule.Enabled {
			continue
		}
		matched := false
		switch rule.Mode {
		case "contains":
			matched = Clean(rule.Pattern) != "" && strings.Contains(n.Text, Clean(rule.Pattern))
		case "exact":
			matched = Clean(rule.Pattern) != "" && n.Text == Clean(rule.Pattern)
		case "regex":
			re, e := regexp.Compile("(?i)" + rule.Pattern)
			matched = e == nil && re.MatchString(n.Text)
		}
		if matched {
			r.LocalAction = strongerAction(r.LocalAction, rule.Action)
			r.Matches = append(r.Matches, domain.Match{Rule: fmt.Sprintf("ad_rule_%d", i+1), Score: 100, Reason: "本地广告匹配：" + rule.Pattern})
		}
	}
	if r.LocalAction != "" {
		r.Score = 100
	}
	if r.Score > 100 {
		r.Score = 100
	}
	return r
}

func Decide(r domain.Risk, ai *domain.AIResult, s domain.Settings, violations int, protected bool) domain.Decision {
	d := domain.Decision{Action: "allow", Reason: "low_risk"}
	if r.LocalAction != "" {
		if protected {
			return domain.Decision{Action: "shadow_log", Reason: "protected_user"}
		}
		d = domain.Decision{Action: r.LocalAction, Delete: true, Reason: "local_ad_rule"}
		if d.Action == "delete" {
			d.Action = "warn"
			if violations >= 3 {
				d.Action = "mute"
				d.Reason = "local_ad_review"
				return d
			}
		}
		if d.Action == "mute" {
			d.Duration = s.MuteSeconds
		}
		return d
	}
	violation := r.Spam || r.Score >= s.DirectThreshold
	mute := false
	if ai != nil && r.Score < s.DirectThreshold && !r.Spam {
		if ai.IsAd && ai.Confidence >= s.AIDeleteConfidence {
			violation = true
			mute = ai.Confidence >= s.AIMuteConfidence && (ai.Severity == "high" || ai.Severity == "critical")
		}
		if ai.IsAd && ai.Confidence >= s.AIWarnConfidence && !violation && s.AutoWarn {
			d.Action = "warn"
			d.Reason = ai.Reason
		}
	}
	if violation {
		d.Reason = "advertising"
		if r.Spam {
			d.Reason = "spam"
		}
		d.Delete = s.AutoDelete
		if d.Delete {
			d.Action = "delete"
		} else {
			d.Action = "shadow_log"
		}
		if s.AutoWarn {
			d.Action = "warn"
		}
		if s.AutoMute && (mute || violations+1 >= s.MuteAfter) {
			d.Action = "mute"
			d.Duration = s.MuteSeconds
		}
		if s.AutoBan && violations+1 >= s.BanAfter {
			d.Action = "ban"
		}
	}
	if protected && (d.Action != "allow" || d.Delete) {
		d.Action = "shadow_log"
		d.Delete = false
		d.Reason = "protected_user"
	}
	return d
}

func KeywordMatch(k domain.Keyword, text string) bool {
	if !k.Enabled {
		return false
	}
	if k.MatchType == "regex" {
		r, e := regexp.Compile(k.Keyword)
		return e == nil && r.MatchString(text)
	}
	text = Clean(text)
	word := Clean(k.Keyword)
	switch k.MatchType {
	case "exact":
		return text == word
	case "contains":
		return strings.Contains(text, word)
	case "starts_with":
		return strings.HasPrefix(text, word)
	case "ends_with":
		return strings.HasSuffix(text, word)
	}
	return false
}
