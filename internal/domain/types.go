package domain

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"tgguard/internal/patterns"
	"time"
)

type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}
type Chat struct {
	ID          int64           `json:"id"`
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Permissions map[string]bool `json:"permissions,omitempty"`
}
type Entity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
	URL    string `json:"url,omitempty"`
}
type Message struct {
	ID                 int64           `json:"message_id"`
	From               *User           `json:"from"`
	SenderChat         *Chat           `json:"sender_chat"`
	IsAutomaticForward bool            `json:"is_automatic_forward"`
	Chat               Chat            `json:"chat"`
	Date               int64           `json:"date"`
	Text               string          `json:"text"`
	Caption            string          `json:"caption"`
	Entities           []Entity        `json:"entities"`
	CaptionEntities    []Entity        `json:"caption_entities"`
	NewMembers         []User          `json:"new_chat_members"`
	LeftMember         *User           `json:"left_chat_member"`
	Reply              *Message        `json:"reply_to_message"`
	Photo              json.RawMessage `json:"photo"`
	Video              json.RawMessage `json:"video"`
	Document           json.RawMessage `json:"document"`
	Sticker            json.RawMessage `json:"sticker"`
	Quote              *TextQuote      `json:"quote"`
	ExternalReply      *ExternalReply  `json:"external_reply"`
	Forward            json.RawMessage `json:"forward_origin"`
}

// Body joins only the fields that are actually present, preserving regex anchors.
func (m Message) Body() string {
	if m.Text == "" {
		return m.Caption
	}
	if m.Caption == "" {
		return m.Text
	}
	return m.Text + "\n" + m.Caption
}

type Member struct {
	User        User   `json:"user"`
	Status      string `json:"status"`
	IsMember    bool   `json:"is_member"`
	CanDelete   bool   `json:"can_delete_messages"`
	CanRestrict bool   `json:"can_restrict_members"`
}

func (m Member) Admin() bool { return m.Status == "creator" || m.Status == "administrator" }
func (m Member) Present() bool {
	return m.Status == "member" || m.Admin() || m.Status == "restricted" && m.IsMember
}

type MemberUpdate struct {
	Chat Chat   `json:"chat"`
	From User   `json:"from"`
	Old  Member `json:"old_chat_member"`
	New  Member `json:"new_chat_member"`
}
type Callback struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}
type Update struct {
	ID       int64         `json:"update_id"`
	Message  *Message      `json:"message"`
	Edited   *Message      `json:"edited_message"`
	Callback *Callback     `json:"callback_query"`
	Member   *MemberUpdate `json:"chat_member"`
	MyMember *MemberUpdate `json:"my_chat_member"`
}

func (u Update) Partition() int64 {
	switch {
	case u.Message != nil:
		return u.Message.Chat.ID
	case u.Edited != nil:
		return u.Edited.Chat.ID
	case u.Member != nil:
		return u.Member.Chat.ID
	case u.MyMember != nil:
		return u.MyMember.Chat.ID
	case u.Callback != nil && u.Callback.Message != nil:
		return u.Callback.Message.Chat.ID
	}
	return 0
}

type Settings struct {
	WelcomeEnabled         bool                   `json:"welcome_enabled"`
	WelcomeText            string                 `json:"welcome_text"`
	AdRules                []AdRule               `json:"ad_rules"`
	VerificationEnabled    bool                   `json:"verification_enabled"`
	VerificationTimeout    int                    `json:"verification_timeout"`
	VerificationType       string                 `json:"verification_type"`
	VerificationFailAction string                 `json:"verification_fail_action"`
	ModerationEnabled      bool                   `json:"moderation_enabled"`
	AIEnabled              bool                   `json:"ai_enabled"`
	AIThreshold            int                    `json:"ai_threshold"`
	DirectThreshold        int                    `json:"direct_threshold"`
	AIWarnConfidence       float64                `json:"ai_warn_confidence"`
	AIDeleteConfidence     float64                `json:"ai_delete_confidence"`
	AIMuteConfidence       float64                `json:"ai_mute_confidence"`
	SpamEnabled            bool                   `json:"spam_enabled"`
	RateLimit              int                    `json:"rate_limit"`
	RateWindow             int                    `json:"rate_window"`
	DuplicateLimit         int                    `json:"duplicate_limit"`
	KeywordEnabled         bool                   `json:"keyword_enabled"`
	KeywordAll             bool                   `json:"keyword_all"`
	AutoDelete             bool                   `json:"auto_delete"`
	AutoWarn               bool                   `json:"auto_warn"`
	AutoMute               bool                   `json:"auto_mute"`
	AutoBan                bool                   `json:"auto_ban"`
	MuteSeconds            int                    `json:"mute_seconds"`
	MuteAfter              int                    `json:"mute_after"`
	BanAfter               int                    `json:"ban_after"`
	NewMemberProtection    bool                   `json:"new_member_protection"`
	AdminBypass            bool                   `json:"admin_bypass"`
	ReviewAccess           string                 `json:"review_access"`
	LogChannel             int64                  `json:"log_channel"`
	Language               string                 `json:"language"`
	Rules                  map[string]RuleSetting `json:"rules"`
}
type RuleSetting struct {
	Action  string `json:"action,omitempty"`
	Enabled bool   `json:"enabled"`
	Score   int    `json:"score"`
}

func DefaultSettings() Settings {
	return Settings{WelcomeEnabled: true, WelcomeText: "欢迎 {name} 加入 {group}！", AdRules: []AdRule{}, VerificationEnabled: true, VerificationTimeout: 180, VerificationType: "math", VerificationFailAction: "kick", ModerationEnabled: true, AIThreshold: 30, DirectThreshold: 80, AIWarnConfidence: .6, AIDeleteConfidence: .8, AIMuteConfidence: .95, SpamEnabled: true, RateLimit: 5, RateWindow: 10, DuplicateLimit: 3, KeywordEnabled: true, AutoDelete: true, AutoWarn: true, AutoMute: true, MuteSeconds: 3600, MuteAfter: 2, BanAfter: 3, NewMemberProtection: true, AdminBypass: true, ReviewAccess: "all", Language: "zh_CN", Rules: map[string]RuleSetting{}}
}
func (s Settings) Validate() error {
	if e := s.validateLocalSettings(); e != nil {
		return e
	}
	if s.VerificationTimeout < 30 || s.VerificationTimeout > 3600 || (s.VerificationType != "math" && s.VerificationType != "button") || !one(s.VerificationFailAction, "kick", "ban", "mute") {
		return errors.New("invalid verification settings")
	}
	if s.AIThreshold < 0 || s.DirectThreshold < s.AIThreshold || s.DirectThreshold > 100 || s.AIWarnConfidence < 0 || s.AIWarnConfidence > s.AIDeleteConfidence || s.AIDeleteConfidence > s.AIMuteConfidence || s.AIMuteConfidence > 1 {
		return errors.New("invalid risk thresholds")
	}
	if s.RateLimit < 2 || s.RateLimit > 100 || s.RateWindow < 1 || s.RateWindow > 300 || s.DuplicateLimit < 2 || s.DuplicateLimit > 50 || s.MuteSeconds < 30 || s.MuteSeconds > 366*86400 || s.MuteAfter < 1 || s.BanAfter < s.MuteAfter {
		return errors.New("invalid punishment or spam settings")
	}
	if !one(s.ReviewAccess, "all", "admin", "trusted") || !one(s.Language, "zh_CN", "en_US") {
		return errors.New("invalid review_access or language")
	}
	for k, r := range s.Rules {
		if _, known := FindBuiltinRule(k); !known || r.Score < 0 || r.Score > 100 {
			return errors.New("invalid rule")
		}
	}
	return nil
}
func one(s string, opts ...string) bool {
	for _, v := range opts {
		if s == v {
			return true
		}
	}
	return false
}

type Normalized struct {
	Contexts       []Normalized `json:"contexts,omitempty"`
	ContextSource  string       `json:"context_source,omitempty"`
	HasWallet      bool         `json:"has_wallet"`
	ChatID         int64        `json:"chat_id"`
	MessageID      int64        `json:"message_id"`
	UserID         int64        `json:"user_id"`
	Username       string       `json:"username"`
	Text           string       `json:"text"`
	URLs           []string     `json:"urls"`
	Mentions       []string     `json:"mentions"`
	MediaType      string       `json:"media_type"`
	IsNew          bool         `json:"is_new"`
	FirstMessage   bool         `json:"first_message"`
	UppercaseRatio float64      `json:"uppercase_ratio"`
}
type Match struct {
	Rule   string `json:"rule"`
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}
type Risk struct {
	LocalAction string  `json:"local_action,omitempty"`
	Score       int     `json:"score"`
	Matches     []Match `json:"matched_rules"`
	Spam        bool    `json:"spam"`
}
type AIResult struct {
	IsAd              bool    `json:"is_ad"`
	Confidence        float64 `json:"confidence"`
	Category          string  `json:"category"`
	Severity          string  `json:"severity"`
	Reason            string  `json:"reason"`
	RecommendedAction string  `json:"recommended_action"`
}

func (r AIResult) Validate() error {
	if r.Confidence < 0 || r.Confidence > 1 || r.Reason == "" || len(r.Reason) > 2000 || !one(r.Category, "normal", "promotion", "crypto", "gambling", "porn", "recruitment", "scam", "traffic_diversion", "external_group", "financial", "unknown") || !one(r.Severity, "low", "medium", "high", "critical") || !one(r.RecommendedAction, "allow", "warn", "delete", "mute", "ban") {
		return errors.New("invalid AI result")
	}
	return nil
}

type Decision struct {
	Action   string `json:"action"`
	Delete   bool   `json:"delete"`
	Duration int    `json:"duration"`
	Reason   string `json:"reason"`
}
type Keyword struct {
	Buttons   [][]LinkButton `json:"buttons,omitempty"`
	ID        int64          `json:"id"`
	ChatID    int64          `json:"chat_id"`
	Keyword   string         `json:"keyword"`
	MatchType string         `json:"match_type"`
	ReplyType string         `json:"reply_type"`
	Content   string         `json:"content"`
	Priority  int            `json:"priority"`
	Enabled   bool           `json:"enabled"`
	Reply     bool           `json:"reply"`
}

type LinkButton struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

func (k Keyword) Validate() error {
	if len(k.Buttons) > 8 {
		return errors.New("最多 8 行链接按钮")
	}
	for _, row := range k.Buttons {
		if len(row) == 0 || len(row) > 4 {
			return errors.New("每行需要 1–4 个按钮")
		}
		for _, b := range row {
			u, e := url.Parse(b.URL)
			if strings.TrimSpace(b.Text) == "" || len([]rune(b.Text)) > 40 || len(b.URL) > 1000 || e != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "tg") || u.Host == "" {
				return errors.New("按钮文字或链接不正确")
			}
		}
	}
	if k.ReplyType == "random" {
		for _, line := range strings.Split(k.Content, "\n") {
			if strings.TrimSpace(line) == "" {
				return errors.New("随机回复不能包含空行")
			}
		}
	}
	if strings.TrimSpace(k.Keyword) == "" || len(k.Keyword) > 500 || k.Content == "" || len(k.Content) > 4000 || !one(k.MatchType, "exact", "contains", "starts_with", "ends_with", "regex") || !one(k.ReplyType, "text", "HTML", "MarkdownV2", "photo", "video", "document", "random") {
		return errors.New("invalid keyword")
	}
	if k.MatchType == "regex" {
		_, e := patterns.Compile(k.Keyword)
		return e
	}
	return nil
}

type ListEntry struct {
	ChatID    int64      `json:"chat_id"`
	UserID    int64      `json:"user_id"`
	Username  string     `json:"username"`
	Kind      string     `json:"kind"`
	Reason    string     `json:"reason"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (l ListEntry) Validate() error {
	if !one(l.Kind, "white", "black", "trusted") || (l.UserID <= 0 && l.Username == "") || len(l.Reason) > 500 || len(l.Username) > 32 {
		return errors.New("invalid list entry")
	}
	if l.Username != "" && !regexp.MustCompile(`^[A-Za-z0-9_]{5,32}$`).MatchString(l.Username) {
		return errors.New("invalid username")
	}
	return nil
}
