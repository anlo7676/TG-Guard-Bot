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

type ModerationPart struct {
	Source string
	Text   string
}

// Keep evidence boundaries explicit; ordinary replies never inherit another member's message.
func (m Message) ModerationParts() []ModerationPart {
	parts := []ModerationPart{{Source: "正文", Text: m.Body()}}
	add := func(source, text string) {
		r := []rune(strings.TrimSpace(text))
		if len(r) > 2000 {
			r = r[:2000]
		}
		if len(r) > 0 {
			parts = append(parts, ModerationPart{source, string(r)})
		}
	}
	var origin MessageOrigin
	forwarded := len(m.Forward) > 0 && json.Unmarshal(m.Forward, &origin) == nil && origin.Type != ""
	if forwarded {
		add("转发来源", origin.Label())
	}
	if m.ExternalReply != nil {
		add("外部引用来源", m.ExternalReply.Origin.Label())
	}
	if (forwarded || m.ExternalReply != nil) && m.Quote != nil {
		add("引用文字", m.Quote.Text)
	}
	return parts
}
func (m Message) ModerationText() string {
	out := []string{}
	for i, p := range m.ModerationParts() {
		if i == 0 {
			out = append(out, p.Text)
		} else {
			out = append(out, "["+p.Source+"] "+p.Text)
		}
	}
	return strings.Join(out, "\n")
}
