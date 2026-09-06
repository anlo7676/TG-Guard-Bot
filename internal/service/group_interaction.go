package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"log/slog"
	"regexp"
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
	Draft       *domain.Keyword
}

func (s *Service) promptGroup(ctx context.Context, m domain.Message, chat int64, action, key, hint string, draft ...*domain.Keyword) error {
	generation, err := s.State.R.Get(ctx, fmt.Sprintf("group:cancel:%d", m.From.ID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	id, err := s.Bot.Send(ctx, m.Chat.ID, fmt.Sprintf("群 %d\n%s\n\n请回复这条消息，10 分钟内有效。/cancel 取消。", chat, hint), map[string]any{"force_reply": true, "selective": true}, 0)
	if err != nil {
		return err
	}
	p := groupPrompt{Chat: chat, Action: action, Key: key, Generation: generation}
	if len(draft) > 0 {
		p.Draft = draft[0]
	}
	if e := s.State.Put(ctx, fmt.Sprintf("group:prompt-kind:%d:%d", m.From.ID, id), true, 24*time.Hour); e != nil {
		return e
	}
	return s.State.Put(ctx, fmt.Sprintf("group:prompt:%d:%d", m.From.ID, id), p, 10*time.Minute)
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
	started := time.Now()
	ttl, err := s.State.R.PTTL(ctx, key).Result()
	if err != nil {
		return true, err
	}
	raw, err := s.State.R.GetDel(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			if n, e := s.State.R.Exists(ctx, fmt.Sprintf("group:prompt-kind:%d:%d", m.From.ID, m.Reply.ID)).Result(); e != nil {
				return true, e
			} else if n > 0 {
				return true, s.text(ctx, m.Chat.ID, "这一步已完成、取消或过期。请回复最新的设置提示，或重新点击设置按钮。")
			}
			return false, nil
		}
		return true, err
	}
	restorePrompt := true
	defer func() {
		remaining := ttl - time.Since(started)
		if restorePrompt && remaining > 0 {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			if e := s.State.R.Set(cleanup, key, raw, remaining).Err(); e != nil {
				slog.Error("group input recovery failed", "error", e)
			}
		}
	}()
	var p groupPrompt
	if err = json.Unmarshal([]byte(raw), &p); err != nil {
		return true, err
	}
	if m.Text == "/cancel" {
		restorePrompt = false
		return true, s.text(ctx, m.Chat.ID, "已取消这次输入。")
	}
	generation, err := s.State.R.Get(ctx, fmt.Sprintf("group:cancel:%d", m.From.ID)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return true, err
	}
	if generation != p.Generation {
		restorePrompt = false
		return true, s.text(ctx, m.Chat.ID, "这次输入已取消，请重新打开菜单。")
	}
	allowed, err := s.Admin(ctx, p.Chat, m.From.ID)
	if err != nil {
		return true, err
	}
	if !allowed {
		restorePrompt = false
		return true, s.text(ctx, m.Chat.ID, "权限检查未通过，未保存任何修改。")
	}
	if _, err = s.Store.MenuGroup(ctx, p.Chat); err != nil {
		if err != sql.ErrNoRows {
			return true, err
		}
		restorePrompt = false
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
	case "keywordWords":
		k := domain.Keyword{ChatID: p.Chat, Enabled: true, Reply: true, MatchType: "contains", ReplyType: "text"}
		if p.Draft != nil {
			k = *p.Draft
		}
		k.Keyword = value
		if p.Key == "new" && strings.ContainsAny(value, "|｜") {
			parts := strings.FieldsFunc(value, func(r rune) bool { return r == '|' || r == '｜' })
			words := []string{}
			for _, word := range parts {
				word = strings.TrimSpace(word)
				if word != "" {
					words = append(words, regexp.QuoteMeta(word))
				}
			}
			if len(words) == 0 {
				err = fmt.Errorf("请至少填写一个关键词")
			} else {
				k.Keyword = strings.Join(words, "|")
				k.MatchType = "regex"
			}
		}
		check := k
		check.Content = "待填写"
		if err == nil {
			err = check.Validate()
			if err != nil {
				err = fmt.Errorf("关键词为空、过长或格式无效，请修改后重试")
			}
		}
		if err == nil {
			err = s.promptGroup(ctx, m, p.Chat, "keywordContent", p.Key, "第 2/2 步：填写回复内容。\n关键词："+value+"\n直接发送回复文字，可换行，不需要添加分隔符。", &k)
			if err == nil {
				restorePrompt = false
				return true, nil
			}
		}
		section = "keywords"
	case "keywordContent":
		if p.Draft == nil {
			err = fmt.Errorf("关键词草稿已失效")
		} else {
			k := *p.Draft
			k.Content = value
			if value == "" {
				err = fmt.Errorf("回复内容不能为空，请输入要发送给群成员的内容")
			} else {
				if k.ID == 0 {
					_, err = s.Store.SaveKeyword(ctx, k, m.From.ID)
				} else {
					err = s.Store.ChangeKeyword(ctx, k.ChatID, k.ID, m.From.ID, func(latest *domain.Keyword) error { latest.Keyword = k.Keyword; latest.Content = k.Content; return nil })
				}
			}
		}
		section = "keywords"
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
	if errors.Is(err, sql.ErrNoRows) {
		restorePrompt = false
		return true, s.text(ctx, m.Chat.ID, "规则或群组已被删除，无法继续保存。请重新打开菜单。")
	}
	if err != nil {
		return true, s.text(ctx, m.Chat.ID, "未能保存："+err.Error()+"\n请修改后继续回复同一条设置提示，无需重新打开菜单。")
	}
	restorePrompt = false
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

var menuRules = domain.BuiltinRules

func ruleValue(v domain.Settings, key string) domain.RuleSetting { return domain.EffectiveRule(v, key) }
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
