package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-sql-driver/mysql"
	"tgguard/internal/api"
	"tgguard/internal/config"
	"tgguard/internal/domain"
	"tgguard/internal/service"
	"tgguard/internal/state"
	"tgguard/internal/store"
	"tgguard/internal/telegram"
)

// This test uses an isolated database and a fake Telegram endpoint. It never contacts real members.
func TestAcceptanceCoreWorkflows(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requires disposable TEST_MYSQL_DSN")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_test") || !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(cfg.DBName) {
		t.Fatal("unsafe test database")
	}
	cfg.DBName = fmt.Sprintf("%s_acceptance_%d_test", strings.TrimSuffix(cfg.DBName, "_test"), time.Now().UnixNano())
	dbName := cfg.DBName
	adminCfg := *cfg
	adminCfg.DBName = ""
	admin, err := sql.Open("mysql", adminCfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err = admin.Exec("CREATE DATABASE IF NOT EXISTS `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP DATABASE `" + dbName + "`")
	ctx := context.Background()
	db, err := store.Open(ctx, cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	redis := miniredis.RunT(t)
	cache := state.New(redis.Addr(), "")
	defer cache.R.Close()
	var mu sync.Mutex
	calls := []struct {
		Method string
		Body   map[string]any
	}{}
	mid := int64(500)
	failNextWelcome := false
	failVerificationNotice := ""
	roles := map[int64]string{42: "administrator", 77: "member"}
	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		method := strings.TrimPrefix(r.URL.Path, "/")
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, struct {
			Method string
			Body   map[string]any
		}{method, in})
		if (failVerificationNotice == "delete" && method == "deleteMessage") || (failVerificationNotice == "private" && method == "sendMessage" && strings.HasPrefix(fmt.Sprint(in["text"]), "验证成功")) {
			failVerificationNotice = ""
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 500, "description": "temporary verification notice failure"})
			return
		}
		var result any = true
		switch method {
		case "getChatMember":
			id := int64(in["user_id"].(float64))
			role := roles[id]
			if role == "" {
				role = "member"
			}
			result = map[string]any{"status": role, "is_member": true, "user": map[string]any{"id": id}}
		case "getChatAdministrators":
			result = []any{map[string]any{"status": "administrator", "user": map[string]any{"id": 42}}}
		case "getChat":
			result = map[string]any{"id": in["chat_id"], "title": "Acceptance A", "type": "supergroup", "permissions": map[string]any{"can_send_messages": true}}
		case "sendMessage":
			if failNextWelcome && strings.HasPrefix(fmt.Sprint(in["text"]), "验后欢迎") {
				failNextWelcome = false
				json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 500, "description": "temporary send failure"})
				return
			}
			mid++
			result = map[string]any{"message_id": mid}
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
	}))
	defer tg.Close()
	svc := &service.Service{Store: db, State: cache, Bot: &telegram.Client{BaseURL: tg.URL, HTTP: tg.Client(), ID: 900, Username: "guardbot"}}
	h := &Handler{Service: svc}
	chat := domain.Chat{ID: -1001, Type: "supergroup", Title: "Acceptance A"}
	other := domain.Chat{ID: -1002, Type: "supergroup", Title: "Acceptance B"}
	for _, g := range []domain.Chat{chat, other} {
		if err = svc.Group(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range []domain.Chat{chat, other} {
		if err = db.AuthorizeGroup(ctx, g.ID, 1, "approved", "test fixture"); err != nil {
			t.Fatal(err)
		}
	}
	update := int64(100)
	handle := func(m domain.Message) {
		t.Helper()
		update++
		if err := h.Handle(ctx, domain.Update{ID: update, Message: &m}); err != nil {
			t.Fatal(err)
		}
	}
	message := func(text string, user int64) domain.Message {
		return domain.Message{ID: update + 1, From: &domain.User{ID: user, FirstName: "tester"}, Chat: chat, Text: text}
	}
	lastSend := func() map[string]any {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		for i := len(calls) - 1; i >= 0; i-- {
			if calls[i].Method == "sendMessage" {
				return calls[i].Body
			}
		}
		t.Fatal("no reply")
		return nil
	}
	count := func(method string) int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, c := range calls {
			if c.Method == method {
				n++
			}
		}
		return n
	}

	t.Run("group authorization gates and safe verification cancellation", func(t *testing.T) {
		g := domain.Chat{ID: -1003, Type: "supergroup", Title: "Pending group"}
		if e := svc.Group(ctx, g); e != nil {
			t.Fatal(e)
		}
		if ok, e := db.GroupAuthorized(ctx, g.ID); e != nil || ok {
			t.Fatal("new group must require approval", e)
		}
		before := count("restrictChatMember") + count("banChatMember") + count("deleteMessage")
		if e := svc.Join(ctx, g, domain.User{ID: 77}); e != nil {
			t.Fatal(e)
		}
		handle(domain.Message{ID: 800, Chat: g, From: &domain.User{ID: 42}, Text: "/settings"})
		if !strings.Contains(fmt.Sprint(lastSend()["text"]), "尚未获准") {
			t.Fatal("missing approval guidance")
		}
		if e := svc.Command(ctx, 801, domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42}}, "approve", "-1003"); e != nil {
			t.Fatal(e)
		}
		if ok, _ := db.GroupAuthorized(ctx, g.ID); ok {
			t.Fatal("group admin self-approved")
		}
		if after := count("restrictChatMember") + count("banChatMember") + count("deleteMessage"); before != after {
			t.Fatal("unapproved group caused Telegram actions")
		}
		svc.SuperAdmins = map[int64]bool{1: true}
		if e := svc.Command(ctx, 802, domain.Message{Chat: domain.Chat{ID: 1, Type: "private"}, From: &domain.User{ID: 1}}, "approve", "-1003 测试批准"); e != nil {
			t.Fatal(e)
		}
		if ok, e := db.GroupAuthorized(ctx, g.ID); e != nil || !ok {
			t.Fatal("approval not effective", e)
		}
		if e := svc.Join(ctx, g, domain.User{ID: 77}); e != nil {
			t.Fatal(e)
		}
		v, e := db.ActiveVerification(ctx, g.ID, 77)
		if e != nil {
			t.Fatal(e)
		}
		mu.Lock()
		roles[77] = "restricted"
		mu.Unlock()
		if e = db.AuthorizeGroup(ctx, g.ID, 1, "revoked", "测试撤销"); e != nil {
			t.Fatal(e)
		}
		if e = svc.Group(ctx, g); e != nil {
			t.Fatal(e)
		}
		if ok, _ := db.GroupAuthorized(ctx, g.ID); ok {
			t.Fatal("registration reset revocation")
		}
		if e = db.ChangeSettings(ctx, g.ID, 42, func(v *domain.Settings) error { v.AutoBan = true; return nil }); e == nil {
			t.Fatal("revoked settings accepted")
		}
		if allowed, e := svc.Admin(ctx, g.ID, 1); e != nil || allowed {
			t.Fatal("superadmin bypassed group authorization")
		}
		if e := svc.GroupMenu(ctx, domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42}}, "gm:-1003:set:auto_ban:true"); e != nil {
			t.Fatal(e)
		}
		settings, e := db.Settings(ctx, g.ID)
		if e != nil || settings.AutoBan {
			t.Fatal("old menu bypassed authorization", e)
		}
		if e := svc.Punish(ctx, store.Log{EventKey: "revoked-ban", ChatID: g.ID, UserID: 77, Decision: domain.Decision{Action: "ban"}}, 1); e != nil {
			t.Fatal(e)
		}
		bans := count("banChatMember")
		restores := count("restrictChatMember")
		if e = svc.SweepVerification(ctx); e != nil {
			t.Fatal(e)
		}
		v, e = db.Verification(ctx, v.Token)
		if e != nil || v.Status != "cancelled" {
			t.Fatal("verification not cancelled", v.Status, e)
		}
		if count("banChatMember") != bans || count("restrictChatMember") != restores+1 {
			t.Fatal("revocation must restore instead of ban")
		}
		mu.Lock()
		roles[77] = "member"
		mu.Unlock()
		if e = db.AuthorizeGroup(ctx, g.ID, 1, "approved", ""); e != nil {
			t.Fatal(e)
		}
		if e = db.DeactivateGroup(ctx, g.ID); e != nil {
			t.Fatal(e)
		}
		if e = svc.Group(ctx, g); e != nil {
			t.Fatal(e)
		}
		if ok, _ := db.GroupAuthorized(ctx, g.ID); ok {
			t.Fatal("reinvite bypassed approval")
		}
		var audits int
		if e = db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_audits WHERE chat_id=? AND action='group.authorization'", g.ID).Scan(&audits); e != nil || audits != 4 {
			t.Fatal("approval audits missing", audits, e)
		}
	})
	t.Run("settings command replies buttons not JSON", func(t *testing.T) {
		handle(message("/settings@guardbot", 42))
		out := lastSend()
		if strings.Contains(fmt.Sprint(out["text"]), "verification_enabled") || out["reply_markup"] == nil {
			t.Fatal("raw settings or missing menu")
		}
		b, _ := json.Marshal(out)
		if !strings.Contains(string(b), "start=group_-1001") {
			t.Fatal("wrong deep link")
		}
	})
	t.Run("private deep link interactive menu and bound callback", func(t *testing.T) {
		m := message("/start group_-1001", 42)
		m.Chat = domain.Chat{ID: 42, Type: "private"}
		handle(m)
		out := lastSend()
		b, _ := json.Marshal(out)
		if !strings.Contains(string(b), "gmc:") || out["chat_id"] != float64(42) {
			t.Fatal("private menu missing")
		}
		if err := svc.GroupMenu(ctx, m, "gm:-1001:set:ai_enabled:true"); err != nil {
			t.Fatal(err)
		}
		v, _ := db.Settings(ctx, -1001)
		bgroup, _ := db.Settings(ctx, -1002)
		if !v.AIEnabled || bgroup.AIEnabled {
			t.Fatal("group isolation failed")
		}
		if err := db.ChangeSettings(ctx, -1001, 42, func(v *domain.Settings) error { v.AIEnabled = false; return nil }); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("join verify and restore", func(t *testing.T) {
		if err := svc.Join(ctx, chat, domain.User{ID: 77, FirstName: "new"}); err != nil {
			t.Fatal(err)
		}
		v, err := db.ActiveVerification(ctx, chat.ID, 77)
		if err != nil {
			t.Fatal(err)
		}
		if count("restrictChatMember") == 0 {
			t.Fatal("join not restricted")
		}
		m := domain.Message{Chat: domain.Chat{ID: 77, Type: "private"}, From: &domain.User{ID: 77}}
		if err = svc.StartVerification(ctx, m, v.Token); err != nil {
			t.Fatal(err)
		}
		var a, b int
		fmt.Sscanf(v.Question, "%d + %d = ?", &a, &b)
		if err = svc.AnswerVerification(ctx, v.Token, 77, fmt.Sprint(a+b)); err != nil {
			t.Fatal(err)
		}
		saved, _ := db.Verification(ctx, v.Token)
		if saved.Status != "verified" {
			t.Fatal(saved.Status)
		}
	})
	t.Run("keyword buttons and matching", func(t *testing.T) {
		_, err := db.SaveKeyword(ctx, domain.Keyword{ChatID: chat.ID, Keyword: "官网", MatchType: "contains", ReplyType: "text", Content: "欢迎", Enabled: true, Reply: true, Buttons: [][]domain.LinkButton{{{Text: "官网", URL: "https://example.com"}}}}, 42)
		if err != nil {
			t.Fatal(err)
		}
		handle(message("官网是什么", 77))
		out := lastSend()
		if out["text"] != "欢迎" || out["reply_markup"] == nil {
			t.Fatal("keyword reply missing buttons")
		}
	})
	t.Run("spam decision delete warn then mute", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			handle(message("私聊推广 https://t.me/spamchat 联系微信 usdt", 77))
		}
		if count("deleteMessage") < 2 || count("restrictChatMember") < 3 {
			t.Fatal("enforcement not executed")
		}
	})
	t.Run("whitelisted member can be unmuted", func(t *testing.T) {
		if err := db.SaveList(ctx, domain.ListEntry{ChatID: chat.ID, UserID: 77, Kind: "white"}, 42, false); err != nil {
			t.Fatal(err)
		}
		before := count("restrictChatMember")
		m := message("/unmute", 42)
		m.Reply = &domain.Message{ID: 200, From: &domain.User{ID: 77}}
		handle(m)
		if count("restrictChatMember") != before+1 {
			t.Fatal("whitelist prevented restoration")
		}
	})
	t.Run("minimum mute is actually applied", func(t *testing.T) {
		m := message("/mute 30s", 42)
		m.Reply = &domain.Message{ID: 201, From: &domain.User{ID: 88}}
		before := count("restrictChatMember")
		handle(m)
		if count("restrictChatMember") != before+1 {
			t.Fatal("30 second mute skipped")
		}
	})
	t.Run("private numeric input keyword CRUD and cancellation", func(t *testing.T) {
		m := domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42}}
		if err := svc.GroupMenu(ctx, m, "gm:-1001:field:rate_limit"); err != nil {
			t.Fatal(err)
		}
		m.Reply = &domain.Message{ID: mid, From: &domain.User{ID: 900}}
		m.Text = "23"
		handle(m)
		v, _ := db.Settings(ctx, chat.ID)
		if v.RateLimit != 23 {
			t.Fatal("numeric input not saved")
		}
		m.Reply = nil
		if err := svc.GroupMenu(ctx, m, "gm:-1001:kwAdd"); err != nil {
			t.Fatal(err)
		}
		m.Reply = &domain.Message{ID: mid, From: &domain.User{ID: 900}}
		m.Text = ""
		handle(m)
		if !strings.Contains(fmt.Sprint(lastSend()["text"]), "继续回复同一条") {
			t.Fatal("invalid input cannot be retried")
		}
		m.Text = "帮助|使用方法"
		handle(m)
		if !strings.Contains(fmt.Sprint(lastSend()["text"]), "第 2/2 步") {
			t.Fatal("keyword wizard did not advance")
		}
		m.Reply = &domain.Message{ID: mid, From: &domain.User{ID: 900}}
		m.Text = "私聊创建成功 | 支持多行\n第二行"
		handle(m)
		ks, _ := db.Keywords(ctx, chat.ID)
		if len(ks) != 2 {
			t.Fatal("keyword creation failed")
		}
		foundAlias := false
		for _, k := range ks {
			if k.Keyword == "帮助|使用方法" && k.MatchType == "regex" && k.Content == "私聊创建成功 | 支持多行\n第二行" {
				foundAlias = true
			}
		}
		if !foundAlias {
			t.Fatal("aliases or reply pipes lost")
		}
		m.Text = "重复提交 | 不应创建"
		handle(m)
		ks, _ = db.Keywords(ctx, chat.ID)
		if len(ks) != 2 {
			t.Fatal("consumed prompt reused")
		}
		if strings.Contains(fmt.Sprint(lastSend()["text"]), "验证无效") {
			t.Fatal("configuration reply treated as verification")
		}
		m.Reply = nil
		if err := svc.GroupMenu(ctx, m, "gm:-1001:field:rate_limit"); err != nil {
			t.Fatal(err)
		}
		prompt := mid
		if err := svc.CancelGroupInput(ctx, m); err != nil {
			t.Fatal(err)
		}
		m.Reply = &domain.Message{ID: prompt, From: &domain.User{ID: 900}}
		m.Text = "30"
		handle(m)
		v, _ = db.Settings(ctx, chat.ID)
		if v.RateLimit != 23 {
			t.Fatal("cancelled input changed settings")
		}
	})
	t.Run("authenticated web settings keyword testing and orphan rejection", func(t *testing.T) {
		token := strings.Repeat("x", 32)
		server := (&api.Server{Service: svc, Config: config.Config{AdminToken: token}}).Handler()
		request := func(method, path, body string) *httptest.ResponseRecorder {
			t.Helper()
			r := httptest.NewRequest(method, path, strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			return w
		}
		if w := request("GET", "/api/v1/rule-catalog", ""); w.Code != 200 {
			t.Fatal("rule catalog unavailable", w.Code)
		} else {
			var catalog []domain.BuiltinRule
			if e := json.Unmarshal(w.Body.Bytes(), &catalog); e != nil || len(catalog) != len(domain.BuiltinRules) {
				t.Fatal("invalid rule catalog", e)
			}
			for _, rule := range catalog {
				if rule.Pattern != "" && rule.Action != "delete" {
					t.Fatal("default action missing", rule.Key)
				}
			}
		}
		if w := request("PUT", "/api/v1/groups/-1001/settings", `{"rate_limit":24}`); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}

		if w := request("PUT", "/api/v1/groups/-1003/settings", `{"rate_limit":24}`); w.Code != 403 {
			t.Fatal("unapproved API mutation", w.Code)
		}
		if w := request("PUT", "/api/v1/groups/-1003/authorization", `{"status":"approved","reason":"网页审批"}`); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if w := request("PUT", "/api/v1/groups/-1003/authorization", `{"status":"rejected"}`); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		v, _ := db.Settings(ctx, chat.ID)
		if v.RateLimit != 24 || !v.VerificationEnabled {
			t.Fatal("patch lost unrelated settings")
		}
		if w := request("PUT", "/api/v1/groups/-1001/settings", `{"rate_limit":null}`); w.Code != 400 {
			t.Fatal("null setting accepted", w.Code)
		}
		if w := request("GET", "/api/v1/groups/-999999/settings", ""); w.Code != 404 {
			t.Fatal("unknown group accepted", w.Code)
		}
		if w := request("POST", "/api/v1/groups/-1001/keywords/test", `{"text":"官网"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"would_reply":true`) {
			t.Fatal("keyword dry-run failed", w.Code, w.Body.String())
		}
		if err := db.Enqueue(ctx, domain.Update{ID: 50000, Message: &domain.Message{Chat: chat, From: &domain.User{ID: 77}, Text: "fixture"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.DB.ExecContext(ctx, "UPDATE update_inbox SET status='dead',attempts=5 WHERE update_id=50000"); err != nil {
			t.Fatal(err)
		}
		if w := request("POST", "/api/v1/queue/50000/retry", "{}"); w.Code != 200 {
			t.Fatal("dead task not requeued", w.Code, w.Body.String())
		}
		if w := request("POST", "/api/v1/queue/50000/retry", "{}"); w.Code != 404 {
			t.Fatal("duplicate retry accepted", w.Code)
		}

	})

	t.Run("all read commands show readable summaries and section menus", func(t *testing.T) {
		if e := db.ChangeSettings(ctx, chat.ID, 42, func(v *domain.Settings) error {
			v.Rules["url"] = domain.RuleSetting{Enabled: false, Score: 7}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		if e := db.SaveList(ctx, domain.ListEntry{ChatID: chat.ID, UserID: 888, Kind: "black", Reason: "展示测试"}, 42, false); e != nil {
			t.Fatal(e)
		}
		for _, tc := range []struct{ command, section, label string }{{"rules", "rules", "审核规则"}, {"stats", "stats", "本群统计"}, {"keywords", "keywords", "关键词回复"}, {"whitelist", "white", "白名单"}, {"blacklist", "black", "黑名单"}} {
			t.Run(tc.command, func(t *testing.T) {
				handle(message("/"+tc.command+"@guardbot", 42))
				out := lastSend()
				text := fmt.Sprint(out["text"])
				if json.Valid([]byte(text)) || !strings.Contains(text, tc.label) {
					t.Fatal("raw or wrong command output", text)
				}
				if tc.command == "rules" && !strings.Contains(text, "外部 URL：已关闭 · 7 分") {
					t.Fatal("rules ignored effective overrides", text)
				}
				raw, _ := json.Marshal(out)
				arg := fmt.Sprintf("group_%d_%s", chat.ID, tc.section)
				if !strings.Contains(string(raw), "start="+arg) {
					t.Fatal("wrong section link", string(raw))
				}
				private := domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42}}
				if e := svc.Command(ctx, 900, private, "start", arg); e != nil {
					t.Fatal(e)
				}
				if text := fmt.Sprint(lastSend()["text"]); json.Valid([]byte(text)) || !strings.Contains(text, chat.Title) {
					t.Fatal("deep link did not open selected group", text)
				}
				if e := svc.Command(ctx, 901, private, tc.command, ""); e != nil {
					t.Fatal(e)
				}
				if !strings.Contains(fmt.Sprint(lastSend()["text"]), "我的群组") {
					t.Fatal("private command did not select group")
				}
				data := lastSend()["reply_markup"].(map[string]any)["inline_keyboard"].([]any)[0].([]any)[0].(map[string]any)["callback_data"].(string)
				if e := svc.MenuCallback(ctx, domain.Callback{ID: "select-command", From: *private.From, Message: &private, Data: data}); e != nil {
					t.Fatal(e)
				}
				if text := fmt.Sprint(lastSend()["text"]); !strings.Contains(text, chat.Title) || strings.Contains(text, "请选择本群管理功能") {
					t.Fatal("private selection lost requested section", text)
				}
				if e := svc.MenuCallback(ctx, domain.Callback{ID: "refresh-section", From: *private.From, Message: &private, Data: "menu:groups:0:" + tc.section}); e != nil {
					t.Fatal(e)
				}
				if !strings.Contains(fmt.Sprint(lastSend()["text"]), "我的群组") {
					t.Fatal("section refresh broken")
				}
			})
		}
	})

	t.Run("help matches group configuration and caller role", func(t *testing.T) {
		if e := db.ChangeSettings(ctx, chat.ID, 42, func(v *domain.Settings) error { v.AIEnabled = false; v.VerificationEnabled = true; return nil }); e != nil {
			t.Fatal(e)
		}
		for _, user := range []int64{42, 77} {
			if e := svc.Command(ctx, 980, message("/start", user), "start", ""); e != nil {
				t.Fatal(e)
			}
			out := lastSend()
			text := fmt.Sprint(out["text"])
			if strings.Contains(text, "/verify") || !strings.Contains(text, "点击欢迎消息中的验证按钮") || !strings.Contains(text, "尚未开启手动 AI 复核") {
				t.Fatal("misleading help", text)
			}
			if strings.Contains(text, "群管理员操作") != (user == 42) {
				t.Fatal("help role mismatch", text)
			}
			raw, _ := json.Marshal(out)
			if strings.Contains(string(raw), "本群设置") != (user == 42) {
				t.Fatal("settings button role mismatch")
			}
		}
		if e := db.ChangeSettings(ctx, chat.ID, 42, func(v *domain.Settings) error { v.AIEnabled = true; v.VerificationEnabled = false; return nil }); e != nil {
			t.Fatal(e)
		}
		if e := svc.Command(ctx, 981, message("/start", 77), "start", ""); e != nil {
			t.Fatal(e)
		}
		text := fmt.Sprint(lastSend()["text"])
		if !strings.Contains(text, "未开启新人验证") || !strings.Contains(text, "/check") {
			t.Fatal("help ignored settings", text)
		}
		private := domain.Message{Chat: domain.Chat{ID: 77, Type: "private"}, From: &domain.User{ID: 77}}
		if e := svc.Command(ctx, 982, private, "start", "help"); e != nil {
			t.Fatal(e)
		}
		text = fmt.Sprint(lastSend()["text"])
		if !strings.Contains(text, "使用帮助") || strings.Contains(text, "/approve") || strings.Contains(text, "/verify") {
			t.Fatal("wrong private help", text)
		}
		if e := svc.Command(ctx, 983, message("/verify", 77), "verify", ""); e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(fmt.Sprint(lastSend()["text"]), "无需在群里发送命令") {
			t.Fatal("legacy verify not redirected")
		}
	})

	t.Run("local ad enforcement welcome settings and ban cleanup", func(t *testing.T) {
		if e := db.ChangeSettings(ctx, other.ID, 42, func(v *domain.Settings) error {
			v.VerificationEnabled = false
			v.WelcomeEnabled = true
			v.WelcomeText = "欢迎 {name} 来到 {group}，你的 ID 是 {user_id}"
			v.AIEnabled = false
			v.SpamEnabled = false
			v.AutoDelete = false
			v.AutoMute = false
			v.AutoBan = false
			v.AutoWarn = false
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		newcomer := domain.User{ID: 98, FirstName: "新伙伴"}
		sends := count("sendMessage")
		if e := svc.Join(ctx, other, newcomer); e != nil {
			t.Fatal(e)
		}
		if e := svc.Join(ctx, other, newcomer); e != nil {
			t.Fatal(e)
		}
		text := fmt.Sprint(lastSend()["text"])
		if !strings.Contains(text, "欢迎 新伙伴 来到 Acceptance B") || count("sendMessage") != sends+1 {
			t.Fatal("welcome template/dedup failed", text)
		}
		if e := db.Leave(ctx, other.ID, 98); e != nil {
			t.Fatal(e)
		}
		if e := svc.Join(ctx, other, newcomer); e != nil {
			t.Fatal(e)
		}
		if count("sendMessage") != sends+2 {
			t.Fatal("rejoin not welcomed")
		}
		for i, action := range []string{"delete", "mute", "ban"} {
			if e := db.ChangeSettings(ctx, other.ID, 42, func(v *domain.Settings) error {
				v.AdRules = []domain.AdRule{{Pattern: "测试广告", Mode: "contains", Action: action, Enabled: true}}
				return nil
			}); e != nil {
				t.Fatal(e)
			}
			msg := domain.Message{ID: int64(9000 + i), Chat: other, From: &domain.User{ID: int64(90 + i)}, Text: "这里有测试广告"}
			event := int64(60000 + i)
			deleted, banned, muted := count("deleteMessage"), count("banChatMember"), count("restrictChatMember")
			if e := svc.Moderate(ctx, event, msg); e != nil {
				t.Fatal(e)
			}
			log, e := db.GetLog(ctx, fmt.Sprintf("auto:%d", event))
			if e != nil || log.Decision.Action != action || !log.Decision.Delete {
				t.Fatal("wrong local enforcement", log, e)
			}
			if count("deleteMessage") != deleted+1 {
				t.Fatal("ad message not deleted")
			}
			if action == "mute" && count("restrictChatMember") != muted+1 {
				t.Fatal("ad author not muted")
			}
			if action == "ban" && count("banChatMember") != banned+1 {
				t.Fatal("ad author not banned")
			}
			if e := svc.Moderate(ctx, event, msg); e != nil {
				t.Fatal(e)
			}
			if count("deleteMessage") != deleted+1 {
				t.Fatal("retry duplicated punishment")
			}
		}
		private := domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42}}
		if e := svc.GroupMenu(ctx, private, "gm:-1002:field:welcome_text"); e != nil {
			t.Fatal(e)
		}
		mu.Lock()
		prompt := mid
		mu.Unlock()
		private.Reply = &domain.Message{ID: prompt, From: &domain.User{ID: 900}}
		private.Text = "你好 {name}，欢迎加入 {group}"
		handle(private)
		configured, e := db.Settings(ctx, other.ID)
		if e != nil || configured.WelcomeText != private.Text {
			t.Fatal("private welcome editor failed", e)
		}
		private.Reply = nil
		if e := svc.GroupMenu(ctx, private, "gm:-1002:adEdit"); e != nil {
			t.Fatal(e)
		}
		mu.Lock()
		prompt = mid
		mu.Unlock()
		private.Reply = &domain.Message{ID: prompt, From: &domain.User{ID: 900}}
		private.Text = "正则|禁言|优惠(领取|代购)"
		handle(private)
		configured, e = db.Settings(ctx, other.ID)
		if e != nil || len(configured.AdRules) != 1 || configured.AdRules[0].Mode != "regex" {
			t.Fatal("private ad editor failed", e)
		}
		if e := db.ChangeSettings(ctx, other.ID, 42, func(v *domain.Settings) error { v.WelcomeEnabled = false; v.VerificationEnabled = true; return nil }); e != nil {
			t.Fatal(e)
		}
		if e := svc.Join(ctx, other, domain.User{ID: 97, FirstName: "待验证"}); e != nil {
			t.Fatal(e)
		}
		out := lastSend()
		if !strings.Contains(fmt.Sprint(out["text"]), "验证按钮") || out["reply_markup"] == nil || strings.Contains(fmt.Sprint(out["text"]), "你好") {
			t.Fatal("disabling welcome removed verification entry")
		}
		m := domain.Message{Chat: other, From: &domain.User{ID: 42}, Reply: &domain.Message{ID: 8999, From: &domain.User{ID: 99}}}
		if e := svc.Command(ctx, 61000, m, "ban", ""); e != nil {
			t.Fatal(e)
		}
		mu.Lock()
		found := false
		for _, c := range calls {
			if c.Method == "banChatMember" && c.Body["user_id"] == float64(99) {
				found = c.Body["revoke_messages"] == true && c.Body["chat_id"] == float64(other.ID)
			}
		}
		mu.Unlock()
		if !found {
			t.Fatal("manual ban did not request all user messages cleared")
		}
		m.From = &domain.User{ID: 77}
		if e := svc.Command(ctx, 61001, m, "id", ""); e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(fmt.Sprint(lastSend()["text"]), "私聊") {
			t.Fatal("ordinary member ID command not redirected")
		}
	})
	t.Run("warning sends user mention and configured escalation without duplicate retries", func(t *testing.T) {
		l := store.Log{EventKey: "warning-notice-test", ChatID: chat.ID, UserID: 987654, MessageID: 9999, Source: "automatic", Decision: domain.Decision{Action: "warn", Delete: true}, AI: &domain.AIResult{IsAd: true, Confidence: .92}}
		if e := svc.Punish(ctx, l, 0); e != nil {
			t.Fatal(e)
		}
		out := lastSend()
		if out["parse_mode"] != "HTML" || !strings.Contains(fmt.Sprint(out["text"]), "tg://user?id=987654") || !strings.Contains(fmt.Sprint(out["text"]), "AI 复核") {
			t.Fatal(out)
		}
		sends := count("sendMessage")
		if e := svc.Punish(ctx, l, 0); e != nil {
			t.Fatal(e)
		}
		if count("sendMessage") != sends {
			t.Fatal("warning duplicated on completed retry")
		}
	})
	t.Run("newly trusted pending member is released at verification expiry", func(t *testing.T) {
		const memberID int64 = 887766
		if e := db.ChangeSettings(ctx, chat.ID, 42, func(s *domain.Settings) error { s.VerificationEnabled = true; return nil }); e != nil {
			t.Fatal(e)
		}
		if e := svc.Join(ctx, chat, domain.User{ID: memberID}); e != nil {
			t.Fatal(e)
		}
		v, e := db.ActiveVerification(ctx, chat.ID, memberID)
		if e != nil {
			t.Fatal(e)
		}
		if e = db.SaveList(ctx, domain.ListEntry{ChatID: chat.ID, UserID: memberID, Kind: "white"}, 42, false); e != nil {
			t.Fatal(e)
		}
		mu.Lock()
		roles[memberID] = "restricted"
		mu.Unlock()
		if _, e = db.DB.ExecContext(ctx, "UPDATE verification_sessions SET expires_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND) WHERE token=?", v.Token); e != nil {
			t.Fatal(e)
		}
		restores, bans := count("restrictChatMember"), count("banChatMember")
		if e = svc.SweepVerification(ctx); e != nil {
			t.Fatal(e)
		}
		v, e = db.Verification(ctx, v.Token)
		if e != nil || v.Status != "cancelled" || count("restrictChatMember") != restores+1 || count("banChatMember") != bans {
			t.Fatal("trusted member not safely released", v.Status, e)
		}
	})
	t.Run("welcome after verification retries and deduplicates", func(t *testing.T) {
		if e := db.ChangeSettings(ctx, chat.ID, 42, func(s *domain.Settings) error {
			s.VerificationEnabled = true
			s.WelcomeEnabled = true
			s.WelcomeText = "验后欢迎 {user_id} 加入 {group}"
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		user := domain.User{ID: 889900, FirstName: "待验证新人"}
		if e := svc.Join(ctx, chat, user); e != nil {
			t.Fatal(e)
		}
		if strings.Contains(fmt.Sprint(lastSend()["text"]), "验后欢迎") {
			t.Fatal("welcome sent before verification")
		}
		v, e := db.ActiveVerification(ctx, chat.ID, user.ID)
		if e != nil {
			t.Fatal(e)
		}
		var a, b int
		fmt.Sscanf(v.Question, "%d + %d = ?", &a, &b)
		mu.Lock()
		failNextWelcome = true
		mu.Unlock()
		if e = svc.AnswerVerification(ctx, v.Token, user.ID, fmt.Sprint(a+b)); e == nil {
			t.Fatal("simulated welcome failure ignored")
		}
		v, e = db.Verification(ctx, v.Token)
		if e != nil || v.Status != "completing" {
			t.Fatal("welcome failure not recoverable", e, v.Status)
		}
		if e = svc.SweepVerification(ctx); e != nil {
			t.Fatal(e)
		}
		v, e = db.Verification(ctx, v.Token)
		if e != nil || v.Status != "verified" {
			t.Fatal(e, v.Status)
		}
		sent, e := db.WelcomeSent(ctx, chat.ID, user.ID)
		if e != nil || !sent {
			t.Fatal("welcome marker missing", e)
		}
		mu.Lock()
		found := false
		for _, c := range calls {
			if c.Method == "sendMessage" && fmt.Sprint(c.Body["text"]) == "验后欢迎 889900 加入 Acceptance A" && c.Body["chat_id"] == float64(chat.ID) {
				found = true
			}
		}
		mu.Unlock()
		if !found {
			t.Fatal("group template not delivered")
		}
		sends := count("sendMessage")
		if e = svc.SweepVerification(ctx); e != nil {
			t.Fatal(e)
		}
		if e = svc.Join(ctx, chat, user); e != nil {
			t.Fatal(e)
		}
		if count("sendMessage") != sends {
			t.Fatal("duplicate welcome")
		}
		// Disabled welcomes still allow successful verification.
		if e = db.ChangeSettings(ctx, chat.ID, 42, func(s *domain.Settings) error { s.WelcomeEnabled = false; return nil }); e != nil {
			t.Fatal(e)
		}
		user.ID++
		if e = svc.Join(ctx, chat, user); e != nil {
			t.Fatal(e)
		}
		v, e = db.ActiveVerification(ctx, chat.ID, user.ID)
		if e != nil {
			t.Fatal(e)
		}
		fmt.Sscanf(v.Question, "%d + %d = ?", &a, &b)
		sends = count("sendMessage")
		if e = svc.AnswerVerification(ctx, v.Token, user.ID, fmt.Sprint(a+b)); e != nil {
			t.Fatal(e)
		}
		if count("sendMessage") != sends+1 || lastSend()["chat_id"] != float64(user.ID) {
			t.Fatal("disabled welcome sent to group")
		}
	})
	t.Run("verification notices recover after committed success", func(t *testing.T) {
		for i, kind := range []string{"delete", "private"} {
			t.Run(kind, func(t *testing.T) {
				user := domain.User{ID: int64(778800 + i), FirstName: "通知测试"}
				if e := svc.Join(ctx, chat, user); e != nil {
					t.Fatal(e)
				}
				v, e := db.ActiveVerification(ctx, chat.ID, user.ID)
				if e != nil {
					t.Fatal(e)
				}
				var a, b int
				fmt.Sscanf(v.Question, "%d + %d = ?", &a, &b)
				mu.Lock()
				failVerificationNotice = kind
				mu.Unlock()
				if e = svc.AnswerVerification(ctx, v.Token, user.ID, fmt.Sprint(a+b)); e == nil {
					t.Fatal("notification failure ignored")
				}
				v, e = db.Verification(ctx, v.Token)
				if e != nil || v.Status != "verified" {
					t.Fatal("verification rolled back for notice failure", e, v.Status)
				}
				muted := count("restrictChatMember")
				if e = svc.SweepVerification(ctx); e != nil {
					t.Fatal(e)
				}
				if count("restrictChatMember") != muted {
					t.Fatal("notification retry changed permissions")
				}
				var done bool
				if e = db.DB.QueryRowContext(ctx, "SELECT notice_done FROM verification_sessions WHERE token=?", v.Token).Scan(&done); e != nil || !done {
					t.Fatal("notice not completed", e)
				}
				sends, deletes := count("sendMessage"), count("deleteMessage")
				if e = svc.SweepVerification(ctx); e != nil {
					t.Fatal(e)
				}
				if count("sendMessage") != sends || count("deleteMessage") != deletes {
					t.Fatal("completed notices repeated")
				}
				if e = svc.AnswerVerification(ctx, v.Token, user.ID, fmt.Sprint(a+b)); e != nil {
					t.Fatal(e)
				}
				if !strings.Contains(fmt.Sprint(lastSend()["text"]), "已通过") {
					t.Fatal("successful replay reported invalid")
				}
			})
		}
	})
	t.Run("expired menu and revoked permission", func(t *testing.T) {
		m := domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42}}
		if err := svc.GroupMenu(ctx, m, "gm:-1001:home"); err != nil {
			t.Fatal(err)
		}
		out := lastSend()
		markup := out["reply_markup"].(map[string]any)
		row := markup["inline_keyboard"].([]any)[0].([]any)
		data := row[0].(map[string]any)["callback_data"].(string)
		redis.FastForward(31 * time.Minute)
		if err := svc.MenuCallback(ctx, domain.Callback{ID: "expired", From: *m.From, Message: &m, Data: data}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(fmt.Sprint(lastSend()["text"]), "过期") {
			t.Fatal("expired menu accepted")
		}
		mu.Lock()
		roles[42] = "member"
		mu.Unlock()
		if err := svc.GroupMenu(ctx, m, "gm:-1001:set:auto_ban:true"); err != nil {
			t.Fatal(err)
		}
		v, _ := db.Settings(ctx, chat.ID)
		if v.AutoBan {
			t.Fatal("revoked admin changed settings")
		}
	})
}
