package domain

import (
	"encoding/json"
	"strings"
)

type TextQuote struct {
	Text     string   `json:"text"`
	Entities []Entity `json:"entities"`
}
type MessageOrigin struct {
	Type       string `json:"type"`
	User       *User  `json:"sender_user"`
	HiddenName string `json:"sender_user_name"`
	SenderChat *Chat  `json:"sender_chat"`
	Chat       *Chat  `json:"chat"`
}
type ExternalReply struct {
	Origin MessageOrigin `json:"origin"`
}

func (o MessageOrigin) Label() string {
	switch o.Type {
	case "user":
		if o.User != nil {
			n := strings.TrimSpace(o.User.FirstName + " " + o.User.LastName)
			if o.User.Username != "" {
				n += " @" + o.User.Username
			}
			return n
		}
	case "hidden_user":
		return o.HiddenName
	case "chat":
		if o.SenderChat != nil {
			return o.SenderChat.Title
		}
	case "channel":
		if o.Chat != nil {
			return o.Chat.Title
		}
	}
	return ""
}

// Body remains sender-authored text for keyword replies; this view is for review evidence.
// Never inherit ordinary in-group reply content or attribute it to the replying member.
func (m Message) ModerationText() string {
	body := m.Body()
	parts := []string{body}
	add := func(label, text string) {
		r := []rune(strings.TrimSpace(text))
		if len(r) > 2000 {
			r = r[:2000]
		}
		if len(r) > 0 {
			parts = append(parts, label+string(r))
		}
	}
	// Explicit reports do not inherit external evidence as the reporter's promotion.
	report := strings.HasPrefix(strings.TrimSpace(body), "举报") || strings.HasPrefix(strings.TrimSpace(body), "这是诈骗") || strings.HasPrefix(strings.TrimSpace(body), "请勿参与")
	if report {
		return body
	}
	var origin MessageOrigin
	forwarded := len(m.Forward) > 0 && json.Unmarshal(m.Forward, &origin) == nil && origin.Type != ""
	if forwarded {
		add("[转发来源] ", origin.Label())
	}
	if m.ExternalReply != nil {
		add("[外部引用来源] ", m.ExternalReply.Origin.Label())
	}
	if (forwarded || m.ExternalReply != nil) && m.Quote != nil {
		add("[引用文字] ", m.Quote.Text)
	}
	return strings.Join(parts, "\n")
}
