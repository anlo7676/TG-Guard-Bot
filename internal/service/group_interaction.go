package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"strconv"
	"strings"
	"tgguard/internal/domain"
	"tgguard/internal/state"
	"time"
)

type menuSession struct {
	User    int64
	Actions []string
}

func (s *Service) secureMenu(ctx context.Context, user int64, rows [][]menuButton) ([][]menuButton, error) {
	session := menuSession{User: user}
	token, err := state.Token()
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		for _, b := range row {
			if strings.HasPrefix(b["callback_data"], "gm:") {
				session.Actions = append(session.Actions, b["callback_data"])
				b["callback_data"] = fmt.Sprintf("gmc:%s:%d", token, len(session.Actions)-1)
			}
		}
	}
	if len(session.Actions) > 0 {
		if err = s.State.Put(ctx, "menu:session:"+token, session, 30*time.Minute); err != nil {
			return nil, err
		}
	}
	return rows, nil
}
func (s *Service) resolveMenu(ctx context.Context, c domain.Callback) (string, error) {
	p := strings.Split(c.Data, ":")
	if len(p) != 3 || len(p[1]) != 32 {
		return "", nil
	}
	i, err := strconv.Atoi(p[2])
	if err != nil {
		return "", nil
	}
	var session menuSession
	if err = s.State.Get(ctx, "menu:session:"+p[1], &session); err != nil || session.User != c.From.ID || i < 0 || i >= len(session.Actions) {
		return "", nil
	}
	return session.Actions[i], nil
}

type groupPrompt struct {
	Chat        int64
	Action, Key string
	Generation  string
}

func (s *Service) promptGroup(ctx context.Context, m domain.Message, chat int64, action, key, hint string) error {
	generation, err := s.State.R.Get(ctx, fmt.Sprintf("group:cancel:%d", m.From.ID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	id, err := s.Bot.Send(ctx, m.Chat.ID, fmt.Sprintf("群 %d\n%s\n\n请回复这条消息，10 分钟内有效。/cancel 取消。", chat, hint), map[string]any{"force_reply": true, "selective": true}, 0)
	if err != nil {
		return err
	}
	return s.State.Put(ctx, fmt.Sprintf("group:prompt:%d:%d", m.From.ID, id), groupPrompt{Chat: chat, Action: action, Key: key, Generation: generation}, 10*time.Minute)
}
func settingPatch(v *domain.Settings, key, value string) error {
	f, ok := domain.FindSetting(key)
	if !ok {
		return fmt.Errorf("未知设置项")
	}
	value = strings.TrimSpace(value)
	switch f.Kind {
	case "bool":
		if value != "true" && value != "false" {
			return fmt.Errorf("请使用开关按钮")
		}
	case "enum":
		found := false
		for _, x := range f.Choices {
			if value == x {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("无效选项")
		}
		value = strconv.Quote(value)
	case "text":
		value = strconv.Quote(value)
	case "number":
		if !json.Valid([]byte(value)) {
			return fmt.Errorf("请输入数字")
		}
	}
	if value == "null" {
		return fmt.Errorf("请输入有效值")
	}
	// Decode into a single-field object; numbers must fit the actual field type.
	if err := json.Unmarshal([]byte(fmt.Sprintf("{%q:%s}", key, value)), v); err != nil {
		return fmt.Errorf("输入格式不正确")
	}
	return v.Validate()
}
func (s *Service) GroupReply(ctx context.Context, m domain.Message) (bool, error) {
	if m.From == nil || m.Chat.Type != "private" || m.Chat.ID != m.From.ID || m.Reply == nil || m.Reply.From == nil || m.Reply.From.ID != s.Bot.ID {
		return false, nil
	}
	key := fmt.Sprintf("group:prompt:%d:%d", m.From.ID, m.Reply.ID)
	raw, err := s.State.R.GetDel(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		return true, err
	}
	var p groupPrompt
	if err = json.Unmarshal([]byte(raw), &p); err != nil {
		return true, err
	}
	if m.Text == "/cancel" {
		return true, s.text(ctx, m.Chat.ID, "已取消这次输入。")
	}
	generation, err := s.State.R.Get(ctx, fmt.Sprintf("group:cancel:%d", m.From.ID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return true, err
	}
	if generation != p.Generation {
		return true, s.text(ctx, m.Chat.ID, "这次输入已取消，请重新打开菜单。")
	}
	allowed, err := s.Admin(ctx, p.Chat, m.From.ID)
	if err != nil || !allowed {
		return true, s.text(ctx, m.Chat.ID, "权限检查未通过，未保存任何修改。")
	}
	if _, err = s.Store.MenuGroup(ctx, p.Chat); err != nil {
		return true, s.text(ctx, m.Chat.ID, "该群已不可用，未保存。")
	}
	value := strings.TrimSpace(m.Text)
	section := "settings"
	switch p.Action {
	case "setting":
		err = s.Store.ChangeSettings(ctx, p.Chat, m.From.ID, func(v *domain.Settings) error { return settingPatch(v, p.Key, value) })
	case "adRules":
		var rules []domain.AdRule
		rules, err = domain.ParseAdRules(value)
		if err == nil {
			err = s.Store.ChangeSettings(ctx, p.Chat, m.From.ID, func(v *domain.Settings) error { v.AdRules = rules; return nil })
		}
		section = "rules"
	case "rule":
		var score int
		score, err = strconv.Atoi(value)
		if err == nil && (score < 0 || score > 100) {
			err = fmt.Errorf("风险分必须在 0–100 之间")
		}
		if err == nil {
			err = s.Store.ChangeSettings(ctx, p.Chat, m.From.ID, func(v *domain.Settings) error {
				r := ruleValue(*v, p.Key)
				r.Score = score
				v.Rules[p.Key] = r
				return nil
			})
		}
		section = "rules"
	case "keyword":
		k := domain.Keyword{ChatID: p.Chat, Enabled: true, Reply: true, MatchType: "contains", ReplyType: "text"}
		if p.Key != "new" {
			id, e := strconv.ParseInt(p.Key, 10, 64)
			if e != nil {
				return true, e
			}
			ks, e := s.Store.Keywords(ctx, p.Chat)
			if e != nil {
				return true, e
			}
			found := false
			for _, v := range ks {
				if v.ID == id {
					k = v
					found = true
					break
				}
			}
			if !found {
				return true, s.text(ctx, m.Chat.ID, "规则已不存在。")
			}
		}
		word, content, ok := strings.Cut(value, "|")
		if !ok {
			err = fmt.Errorf("格式：关键词 | 回复内容")
		} else {
			k.Keyword = strings.TrimSpace(word)
			k.Content = strings.TrimSpace(content)
			_, err = s.Store.SaveKeyword(ctx, k, m.From.ID)
		}
		section = "keywords"
	case "list":
		target, reason, _ := strings.Cut(value, " ")
		l := domain.ListEntry{ChatID: p.Chat, Kind: p.Key, Reason: strings.TrimSpace(reason)}
		if strings.HasPrefix(target, "@") {
			l.Username = strings.TrimPrefix(target, "@")
		} else {
			l.UserID, err = strconv.ParseInt(target, 10, 64)
		}
		if err == nil {
			err = l.Validate()
		}
		if err == nil {
			err = s.Store.SaveList(ctx, l, m.From.ID, false)
		}
		section = p.Key
	default:
		return true, s.text(ctx, m.Chat.ID, "输入已失效，请重新打开菜单。")
	}
	if err != nil {
		return true, s.text(ctx, m.Chat.ID, "未能保存："+err.Error()+"\n请重新点击对应设置按钮后输入。")
	}
	return true, s.GroupMenu(ctx, m, fmt.Sprintf("gm:%d:%s", p.Chat, section))
}

func (s *Service) CancelGroupInput(ctx context.Context, m domain.Message) error {
	token, e := state.Token()
	if e != nil {
		return e
	}
	if e = s.State.R.Set(ctx, fmt.Sprintf("group:cancel:%d", m.From.ID), token, 24*time.Hour).Err(); e != nil {
		return e
	}
	return s.text(ctx, m.Chat.ID, "已取消待填写的群设置。发送 /menu 返回菜单。")
}

var menuRules = []struct {
	Key, Label string
	Score      int
}{{"url", "外部 URL", 20}, {"telegram_link", "Telegram 链接", 40}, {"contact", "联系方式", 25}, {"mention", "用户名引流", 20}, {"advertising", "广告招揽", 25}, {"gambling", "博彩推广", 35}, {"porn", "色情推广", 35}, {"crypto", "币圈招揽", 20}, {"caps", "大量大写", 15}, {"emoji", "大量表情", 15}, {"many_links", "大量链接", 25}}

func ruleValue(v domain.Settings, key string) domain.RuleSetting {
	if r, ok := v.Rules[key]; ok {
		return r
	}
	for _, r := range menuRules {
		if r.Key == key {
			return domain.RuleSetting{Enabled: true, Score: r.Score}
		}
	}
	return domain.RuleSetting{}
}
func knownRule(key string) bool {
	for _, r := range menuRules {
		if r.Key == key {
			return true
		}
	}
	return false
}
func settingValues(v domain.Settings) map[string]any {
	b, _ := json.Marshal(v)
	var out map[string]any
	json.Unmarshal(b, &out)
	return out
}
func displayValue(v any) string {
	switch x := v.(type) {
	case bool:
		if x {
			return "已开启"
		}
		return "已关闭"
	case string:
		labels := map[string]string{"math": "数学题", "button": "按钮验证", "kick": "踢出可重入", "ban": "封禁", "mute": "保持禁言", "all": "所有成员", "admin": "仅管理员", "trusted": "管理员及可信成员", "zh_CN": "简体中文", "en_US": "英语"}
		if label, ok := labels[x]; ok {
			return label
		}
		return x
	default:
		return fmt.Sprint(v)
	}
}
