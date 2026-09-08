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
	"tgguard/internal/settings"
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
	failAuditWrong := false
	restrictionFailure := 0
	failReviewNoticeOnce := false
	failAdminLookupOnce := false
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
		if failReviewNoticeOnce && method == "sendMessage" && strings.Contains(fmt.Sprint(in["text"]), "累计违规超过 3 次") {
			failReviewNoticeOnce = false
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 500, "description": "temporary notice failure"})
			return
		}
		if restrictionFailure != 0 && method == "restrictChatMember" && (fmt.Sprint(in["user_id"]) == "901998" || fmt.Sprint(in["user_id"]) == "901988") {
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": restrictionFailure, "description": "simulated restriction failure"})
			return
		}
		if failAuditWrong && method == "sendMessage" {
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 500, "description": "simulated notification failure"})
			return
		}
		var result any = true
		switch method {
		case "getChatMember":
			if failAdminLookupOnce {
				failAdminLookupOnce = false
				json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 500, "description": "temporary permission failure"})
				return
			}
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
		intruder := domain.Message{Chat: domain.Chat{ID: 88, Type: "private"}, From: &domain.User{ID: 88}}
		if e := svc.StartVerification(ctx, intruder, v.Token); e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(fmt.Sprint(lastSend()["text"]), "这不是你的入群验证") {
			t.Fatal("wrong user got a challenge")
		}
		if e := svc.AnswerVerification(ctx, v.Token, 88, "wrong"); e != nil {
			t.Fatal(e)
		}
		untouched, e := db.Verification(ctx, v.Token)
		if e != nil || untouched.Attempts != 0 || untouched.Status != "pending" {
			t.Fatal("other user changed verification", e, untouched)
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

		beforeCalls := count("sendMessage") + count("deleteMessage") + count("banChatMember")
		for _, sample := range []struct{ body, want string }{
			{`{"text":"足球红单推荐交流群.加入免费领红包 @losusnh9071bot"}`, `"action":"warn"`},
			{`{"text":"你好哈喽"}`, `"action":"allow"`},
			{`{"text":"搞 米","forward_source":"需要群发 联系@SHxxbb"}`, `"action":"warn"`},
			{`{"text":"搞 米","quote":"有收款码的来做，打钱爽快"}`, `"action":"warn"`},
		} {
			w := request("POST", "/api/v1/groups/-1001/rules/test", sample.body)
			if w.Code != 200 || !strings.Contains(w.Body.String(), sample.want) {
				t.Fatal("rule simulation", w.Code, w.Body.String())
			}
		}
		if w := request("POST", "/api/v1/groups/-1001/rules/test", `{"text":" "}`); w.Code != 400 {
			t.Fatal("empty rule input accepted")
		}
		if w := request("POST", "/api/v1/groups/-99999/rules/test", `{"text":"test"}`); w.Code != 404 {
			t.Fatal("orphan group accepted")
		}
		if count("sendMessage")+count("deleteMessage")+count("banChatMember") != beforeCalls {
			t.Fatal("simulation sent Telegram actions")
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
			warned := count("sendMessage")
			deleted, banned, muted := count("deleteMessage"), count("banChatMember"), count("restrictChatMember")
			if e := svc.Moderate(ctx, event, msg); e != nil {
				t.Fatal(e)
			}
			log, e := db.GetLog(ctx, fmt.Sprintf("auto:%d", event))
			want := action
			if want == "delete" {
				want = "warn"
			}
			if e != nil || log.Decision.Action != want || !log.Decision.Delete {
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
			if action == "delete" && (count("sendMessage") != warned+1 || !strings.Contains(fmt.Sprint(lastSend()["text"]), "累计超过 3 次") || !strings.Contains(fmt.Sprint(lastSend()["text"]), "tg://user?id=")) {
				t.Fatal("warning missing, misleading or duplicated")
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
	t.Run("welcome cleanup persists deadline and retries across service restart", func(t *testing.T) {
		if e := db.ChangeSettings(ctx, chat.ID, 42, func(v *domain.Settings) error { v.WelcomeEnabled = true; v.VerificationEnabled = false; return nil }); e != nil {
			t.Fatal(e)
		}
		if e := svc.Join(ctx, chat, domain.User{ID: 991122, FirstName: "自动删除测试"}); e != nil {
			t.Fatal(e)
		}
		mu.Lock()
		welcomeID := mid
		mu.Unlock()
		var seconds int
		if e := db.DB.QueryRowContext(ctx, "SELECT TIMESTAMPDIFF(SECOND,UTC_TIMESTAMP(6),delete_at) FROM welcome_cleanup WHERE chat_id=? AND message_id=?", chat.ID, welcomeID).Scan(&seconds); e != nil || seconds < 290 || seconds > 300 {
			t.Fatal("welcome deadline not five minutes", seconds, e)
		}
		deletes := count("deleteMessage")
		if e := svc.SweepWelcomeCleanup(ctx); e != nil {
			t.Fatal(e)
		}
		if count("deleteMessage") != deletes {
			t.Fatal("welcome deleted early")
		}
		if _, e := db.DB.ExecContext(ctx, "UPDATE welcome_cleanup SET next_attempt_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND) WHERE chat_id=? AND message_id=?", chat.ID, welcomeID); e != nil {
			t.Fatal(e)
		}
		mu.Lock()
		failVerificationNotice = "delete"
		mu.Unlock()
		if e := svc.SweepWelcomeCleanup(ctx); e != nil {
			t.Fatal(e)
		}
		var attempts int
		var done bool
		if e := db.DB.QueryRowContext(ctx, "SELECT attempts,done FROM welcome_cleanup WHERE chat_id=? AND message_id=?", chat.ID, welcomeID).Scan(&attempts, &done); e != nil || attempts != 1 || done {
			t.Fatal("deletion failure not persisted", e)
		}
		if _, e := db.DB.ExecContext(ctx, "UPDATE welcome_cleanup SET next_attempt_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND) WHERE chat_id=? AND message_id=?", chat.ID, welcomeID); e != nil {
			t.Fatal(e)
		}
		restarted := &service.Service{Store: db, Bot: svc.Bot}
		if e := restarted.SweepWelcomeCleanup(ctx); e != nil {
			t.Fatal(e)
		}
		if e := db.DB.QueryRowContext(ctx, "SELECT done FROM welcome_cleanup WHERE chat_id=? AND message_id=?", chat.ID, welcomeID).Scan(&done); e != nil || !done {
			t.Fatal("restart did not finish deletion", e)
		}
		deletes = count("deleteMessage")
		if e := restarted.SweepWelcomeCleanup(ctx); e != nil {
			t.Fatal(e)
		}
		if count("deleteMessage") != deletes {
			t.Fatal("completed cleanup repeated")
		}
	})

	t.Run("private input survives permission lookup failure", func(t *testing.T) {
		m := domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42}}
		if e := svc.GroupMenu(ctx, m, "gm:-1001:field:rate_limit"); e != nil {
			t.Fatal(e)
		}
		m.Reply = &domain.Message{ID: mid, From: &domain.User{ID: 900}}
		m.Text = "24"
		mu.Lock()
		failAdminLookupOnce = true
		mu.Unlock()
		if handled, e := svc.GroupReply(ctx, m); !handled || e == nil {
			t.Fatal("expected retriable permission failure", e)
		}
		if _, e := svc.GroupReply(ctx, m); e != nil {
			t.Fatal(e)
		}
		v, e := db.Settings(ctx, chat.ID)
		if e != nil || v.RateLimit != 24 {
			t.Fatal("input lost", e, v.RateLimit)
		}
	})
	t.Run("keyword wizard preserves concurrent status and priority edits", func(t *testing.T) {
		k := domain.Keyword{ChatID: chat.ID, Keyword: "原词", Content: "旧回复", MatchType: "contains", ReplyType: "text", Enabled: true, Reply: true}
		id, e := db.SaveKeyword(ctx, k, 42)
		if e != nil {
			t.Fatal(e)
		}
		m := domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42}}
		if e = svc.GroupMenu(ctx, m, fmt.Sprintf("gm:-1001:kwEdit:%d", id)); e != nil {
			t.Fatal(e)
		}
		m.Reply = &domain.Message{ID: mid, From: &domain.User{ID: 900}}
		m.Text = "新词"
		handle(m)
		m.Reply = &domain.Message{ID: mid, From: &domain.User{ID: 900}}
		m.Text = "新回复"
		if e = db.ChangeKeyword(ctx, chat.ID, id, 42, func(k *domain.Keyword) error { k.Enabled = false; k.Priority = 9; return nil }); e != nil {
			t.Fatal(e)
		}
		handle(m)
		ks, e := db.Keywords(ctx, chat.ID)
		if e != nil {
			t.Fatal(e)
		}
		for _, k := range ks {
			if k.ID == id {
				if k.Keyword != "新词" || k.Content != "新回复" || k.Enabled || k.Priority != 9 {
					t.Fatal("concurrent fields overwritten", k)
				}
				return
			}
		}
		t.Fatal("missing keyword")
	})
	t.Run("disabling verification releases pending members without punishment", func(t *testing.T) {
		if e := db.ChangeSettings(ctx, chat.ID, 42, func(s *domain.Settings) error { s.VerificationEnabled = true; return nil }); e != nil {
			t.Fatal(e)
		}
		const uid int64 = 887799
		if e := svc.Join(ctx, chat, domain.User{ID: uid}); e != nil {
			t.Fatal(e)
		}
		v, e := db.ActiveVerification(ctx, chat.ID, uid)
		if e != nil {
			t.Fatal(e)
		}
		mu.Lock()
		roles[uid] = "restricted"
		mu.Unlock()
		if e = db.ChangeSettings(ctx, chat.ID, 42, func(s *domain.Settings) error { s.VerificationEnabled = false; return nil }); e != nil {
			t.Fatal(e)
		}
		restores, bans := count("restrictChatMember"), count("banChatMember")
		if e = svc.SweepVerification(ctx); e != nil {
			t.Fatal(e)
		}
		v, e = db.Verification(ctx, v.Token)
		if e != nil || v.Status != "cancelled" || count("restrictChatMember") != restores+1 || count("banChatMember") != bans {
			t.Fatal("pending member not released", v.Status, e)
		}
		m := domain.Message{Chat: domain.Chat{ID: uid, Type: "private"}, From: &domain.User{ID: uid}}
		if e = svc.StartVerification(ctx, m, v.Token); e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(fmt.Sprint(lastSend()["text"]), "已结束或由管理员接管") {
			t.Fatal("cancelled link misreported")
		}
		if e = svc.AnswerVerification(ctx, v.Token, uid, "25"); e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(fmt.Sprint(lastSend()["text"]), "已结束或由管理员接管") {
			t.Fatal("cancelled answer misreported")
		}
		before := count("sendMessage")
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		m.Reply = &domain.Message{ID: 999, From: &domain.User{ID: 900}}
		if e = svc.VerificationReply(cancelled, m); e == nil || count("sendMessage") != before {
			t.Fatal("cache error misreported as expiry", e)
		}

	})

	t.Run("fourth local ad is permanently muted for admin review", func(t *testing.T) {
		if e := db.ChangeSettings(ctx, other.ID, 42, func(v *domain.Settings) error {
			v.ModerationEnabled = true
			v.SpamEnabled = false
			v.AutoBan = true
			v.BanAfter = 3
			v.AdRules = []domain.AdRule{{Pattern: "累计广告样例", Mode: "contains", Action: "delete", Enabled: true}}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		const uid int64 = 990077
		bans, mutes := count("banChatMember"), count("restrictChatMember")
		for i := 0; i < 4; i++ {
			m := domain.Message{ID: int64(80100 + i), Chat: other, From: &domain.User{ID: uid}, Text: fmt.Sprintf("累计广告样例 %d", i)}
			if e := svc.Moderate(ctx, int64(80100+i), m); e != nil {
				t.Fatal(e)
			}
			l, e := db.GetLog(ctx, fmt.Sprintf("auto:%d", 80100+i))
			if e != nil {
				t.Fatal(e)
			}
			want := "warn"
			if i == 3 {
				want = "mute"
				if l.Decision.Duration != 0 || l.Decision.Reason != "local_ad_review" {
					t.Fatal("not pending admin review", l.Decision)
				}
			}
			if l.Decision.Action != want {
				t.Fatal(i, l.Decision)
			}
		}
		if count("banChatMember") != bans || count("restrictChatMember") != mutes+1 {
			t.Fatal("unexpected ban/mute")
		}
		mu.Lock()
		for _, c := range calls {
			if c.Method == "restrictChatMember" && c.Body["user_id"] == float64(uid) {
				if _, ok := c.Body["until_date"]; ok {
					t.Error("review mute has expiry")
				}
			}
		}
		mu.Unlock()
		n, e := db.ViolationCount(ctx, other.ID, uid)
		if e != nil || n != 4 {
			t.Fatal("punishment not recorded", n, e)
		}
		sends := count("sendMessage")
		if e = svc.Moderate(ctx, 80103, domain.Message{ID: 80103, Chat: other, From: &domain.User{ID: uid}, Text: "累计广告样例"}); e != nil {
			t.Fatal(e)
		}
		if count("sendMessage") != sends || count("restrictChatMember") != mutes+1 {
			t.Fatal("retry duplicated review action")
		}
	})

	t.Run("forward source evidence survives stored moderation", func(t *testing.T) {
		const event int64 = 891234
		m := domain.Message{ID: 891234, Chat: other, From: &domain.User{ID: 891234}, Text: "搞 米", Forward: json.RawMessage(`{"type":"hidden_user","sender_user_name":"需要群发 联系@SHxxbb"}`)}
		if e := svc.Moderate(ctx, event, m); e != nil {
			t.Fatal(e)
		}
		l, e := db.GetLog(ctx, "auto:891234")
		if e != nil || l.Decision.Action != "warn" || !l.Decision.Delete || !strings.Contains(l.Text, "[转发来源] 需要群发 联系@SHxxbb") {
			t.Fatal("forward evidence/action missing", l, e)
		}
	})

	t.Run("admin numeric commands supersede failed review notification", func(t *testing.T) {
		const uid int64 = 998800
		l := store.Log{EventKey: "review-notice-retry", ChatID: other.ID, UserID: uid, MessageID: 88001, Decision: domain.Decision{Action: "mute", Delete: true, Reason: "local_ad_review"}, Source: "automatic"}
		mu.Lock()
		failReviewNoticeOnce = true
		mu.Unlock()
		if e := svc.Punish(ctx, l, 0); e == nil {
			t.Fatal("expected notice failure")
		}
		p, e := db.PreparePunishment(ctx, l, 0)
		if e != nil || !p.Acted || p.Status == "done" {
			t.Fatal("restriction not persisted separately", p, e)
		}
		before := count("restrictChatMember")
		denied := domain.Message{Chat: other, From: &domain.User{ID: 77}}
		if e = svc.Command(ctx, 991000, denied, "unmute", fmt.Sprint(uid)); e != nil {
			t.Fatal(e)
		}
		if count("restrictChatMember") != before {
			t.Fatal("member could unmute")
		}
		adminMessage := domain.Message{Chat: other, From: &domain.User{ID: 42}}
		if e = svc.Command(ctx, 991001, adminMessage, "unmute", fmt.Sprint(uid)); e != nil {
			t.Fatal(e)
		}
		if count("restrictChatMember") != before+1 {
			t.Fatal("numeric unmute failed")
		}
		sends := count("sendMessage")
		if e = svc.Punish(ctx, l, 0); e != nil {
			t.Fatal(e)
		}
		if count("restrictChatMember") != before+1 || count("sendMessage") != sends {
			t.Fatal("retry overrode admin resolution")
		}
		p, e = db.PreparePunishment(ctx, l, 0)
		if e != nil || p.Status != "skipped" {
			t.Fatal("superseded review not recorded", p, e)
		}
		bans := count("banChatMember")
		if e = svc.Command(ctx, 991002, adminMessage, "ban", fmt.Sprint(uid)); e != nil {
			t.Fatal(e)
		}
		if count("banChatMember") != bans+1 {
			t.Fatal("numeric ban failed")
		}
		if e = svc.Command(ctx, 991003, adminMessage, "mute", fmt.Sprint(uid)+" 2h"); e != nil {
			t.Fatal(e)
		}
		if count("restrictChatMember") != before+2 {
			t.Fatal("numeric timed mute failed")
		}
	})

	t.Run("all stale automatic punishments respect newer manual resolution", func(t *testing.T) {
		for i, action := range []string{"mute", "ban", "warn"} {
			uid := int64(77889901 + i)
			l := store.Log{EventKey: fmt.Sprintf("ordinary-retry-%d", i), ChatID: other.ID, UserID: uid, Decision: domain.Decision{Action: action, Duration: 3600, Reason: "advertising"}, Source: "automatic"}
			mu.Lock()
			failAdminLookupOnce = true
			mu.Unlock()
			if e := svc.Punish(ctx, l, 0); e == nil {
				t.Fatal("expected injected failure")
			}
			if e := svc.Command(ctx, int64(919190+i), domain.Message{Chat: other, From: &domain.User{ID: 42}}, "unmute", fmt.Sprint(uid)); e != nil {
				t.Fatal(e)
			}
			before := count("restrictChatMember") + count("banChatMember") + count("sendMessage")
			if e := svc.Punish(ctx, l, 0); e != nil {
				t.Fatal(e)
			}
			if count("restrictChatMember")+count("banChatMember")+count("sendMessage") != before {
				t.Fatal("stale automatic action overrides manual resolution", action)
			}
			p, e := db.PreparePunishment(ctx, l, 0)
			if e != nil || p.Status != "skipped" {
				t.Fatal("superseded action not recorded", p, e)
			}
		}
	})
	t.Run("channel advertising uses delete and warning without user penalties", func(t *testing.T) {
		m := domain.Message{ID: 909091, Chat: other, SenderChat: &domain.Chat{ID: -200999, Type: "channel"}, From: &domain.User{ID: 136817688, IsBot: true}, Text: "需要群发 联系@SHxxbb"}
		beforeDelete, beforeWarn := count("deleteMessage"), count("sendMessage")
		beforeUser := count("getChatMember") + count("restrictChatMember") + count("banChatMember")
		handler := &Handler{Service: svc}
		if e := handler.Handle(ctx, domain.Update{ID: 909091, Message: &m}); e != nil {
			t.Fatal(e)
		}
		if count("deleteMessage") != beforeDelete+1 || count("sendMessage") != beforeWarn+1 {
			t.Fatal("channel ad not removed and warned")
		}
		if count("getChatMember")+count("restrictChatMember")+count("banChatMember") != beforeUser {
			t.Fatal("channel treated as user")
		}
		if e := handler.Handle(ctx, domain.Update{ID: 909091, Message: &m}); e != nil {
			t.Fatal(e)
		}
		if count("deleteMessage") != beforeDelete+1 || count("sendMessage") != beforeWarn+1 {
			t.Fatal("duplicate channel penalty")
		}
		m.SenderChat = &domain.Chat{ID: other.ID, Type: "supergroup"}
		if e := handler.Handle(ctx, domain.Update{ID: 909092, Message: &m}); e != nil {
			t.Fatal(e)
		}
		if count("deleteMessage") != beforeDelete+1 {
			t.Fatal("anonymous admin punished")
		}
	})
	t.Run("answer retries and channel policy regressions", func(t *testing.T) {
		token := "retry_answer_token"
		v := store.Verification{Token: token, ChatID: other.ID, UserID: 991121, Type: "math", Question: "1+1", AnswerHash: store.HashAnswer(token, "2"), FailAction: "kick", ExpiresAt: time.Now().Add(time.Minute)}
		if e := db.CreateVerification(ctx, v); e != nil {
			t.Fatal(e)
		}
		mu.Lock()
		failAuditWrong = true
		mu.Unlock()
		for i := 0; i < 3; i++ {
			if e := svc.AnswerVerification(ctx, token, v.UserID, "1", "message:991121:1"); e == nil {
				t.Fatal("expected notification failure")
			}
		}
		mu.Lock()
		failAuditWrong = false
		mu.Unlock()
		v, e := db.Verification(ctx, token)
		if e != nil || v.Attempts != 1 {
			t.Fatal("same event counted twice", v, e)
		}
		_ = svc.AnswerVerification(ctx, token, v.UserID, "1", "message:991121:2")
		v, e = db.Verification(ctx, token)
		if e != nil || v.Attempts != 2 {
			t.Fatal("new answer not counted", v, e)
		}
		if e := svc.AnswerVerification(ctx, token, v.UserID, "2", "message:991121:3"); e != nil {
			t.Fatal(e)
		}
		chat := domain.Chat{ID: -100887766, Type: "supergroup", Title: "Regression"}
		if e := db.RegisterGroup(ctx, chat); e != nil {
			t.Fatal(e)
		}
		if e := db.AuthorizeGroup(ctx, chat.ID, 42, "approved", ""); e != nil {
			t.Fatal(e)
		}
		h := &Handler{Service: svc}
		m := domain.Message{Chat: chat, SenderChat: &domain.Chat{ID: -777887766, Type: "channel"}, Text: "早上好"}
		before := count("deleteMessage")
		for i := 0; i < 7; i++ {
			m.ID = int64(888870 + i)
			if e := h.Handle(ctx, domain.Update{ID: m.ID, Message: &m}); e != nil {
				t.Fatal(e)
			}
		}
		if count("deleteMessage") <= before {
			t.Fatal("channel spam bypassed")
		}
		if e := db.ChangeSettings(ctx, chat.ID, 42, func(s *domain.Settings) error {
			s.AutoDelete = false
			s.AutoMute = false
			s.AutoBan = false
			s.AutoWarn = true
			s.Rules["url"] = domain.RuleSetting{Enabled: true, Score: 100}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		before = count("deleteMessage")
		m.ID = 888880
		m.Text = "https://example.com"
		if e := h.Handle(ctx, domain.Update{ID: m.ID, Message: &m}); e != nil {
			t.Fatal(e)
		}
		if count("deleteMessage") != before {
			t.Fatal("channel warning ignored AutoDelete")
		}
	})
	t.Run("retention preserves counters feedback and unfinished inbox", func(t *testing.T) {
		l, e := db.SaveLog(ctx, store.Log{EventKey: "retention-1", ChatID: other.ID, UserID: 555119, Text: "old text", Source: "automatic", Decision: domain.Decision{Action: "warn"}})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.PreparePunishment(ctx, l, 0); e != nil {
			t.Fatal(e)
		}
		if e = db.PunishmentStep(ctx, l.EventKey, "done"); e != nil {
			t.Fatal(e)
		}
		feedback := l
		feedback.EventKey = "retention-feedback"
		feedback.ID = 0
		feedback, e = db.SaveLog(ctx, feedback)
		if e != nil {
			t.Fatal(e)
		}
		if e = db.Feedback(ctx, other.ID, feedback.ID, 42, "keep evidence"); e != nil {
			t.Fatal(e)
		}
		if _, e = db.DB.ExecContext(ctx, "UPDATE moderation_logs SET created_at=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 100 DAY) WHERE id IN (?,?)", l.ID, feedback.ID); e != nil {
			t.Fatal(e)
		}
		for i := int64(771111); i <= 771112; i++ {
			if e = db.Enqueue(ctx, domain.Update{ID: i, Message: &domain.Message{Text: "private content", Chat: other}}); e != nil {
				t.Fatal(e)
			}
		}
		if _, e = db.DB.ExecContext(ctx, "UPDATE update_inbox SET status=IF(update_id=771111,'done','dead'),created_at=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 100 DAY) WHERE update_id IN (771111,771112)"); e != nil {
			t.Fatal(e)
		}
		if e = db.RetainData(ctx, 90); e != nil {
			t.Fatal(e)
		}
		current, e := db.GetLog(ctx, l.EventKey)
		if e != nil || current.Text == "old text" {
			t.Fatal("old content not cleared", e)
		}
		kept, e := db.GetLog(ctx, feedback.EventKey)
		if e != nil || kept.Text != "old text" {
			t.Fatal("feedback evidence lost", e)
		}
		n, e := db.ViolationCount(ctx, other.ID, l.UserID)
		if e != nil || n != 1 {
			t.Fatal("counter changed", n, e)
		}
		var raw string
		if e = db.DB.QueryRowContext(ctx, "SELECT payload FROM update_inbox WHERE update_id=771112").Scan(&raw); e != nil || !strings.Contains(raw, "private content") {
			t.Fatal("dead task damaged", e)
		}
		if e = db.Enqueue(ctx, domain.Update{ID: 771111}); e != nil {
			t.Fatal(e)
		}
		if e = db.DB.QueryRowContext(ctx, "SELECT payload FROM update_inbox WHERE update_id=771111").Scan(&raw); e != nil || strings.Contains(raw, "private content") {
			t.Fatal("tombstone lost", e)
		}
		name, e := db.LockName(ctx, "instance")
		if e != nil {
			t.Fatal(e)
		}
		if len(name) > 64 || !strings.HasPrefix(name, "tg_guard:instance:") {
			t.Fatal("invalid lock name", name)
		}
		if e = db.Migrate(ctx); e != nil {
			t.Fatal("migration not restart safe", e)
		}
	})
	t.Run("database locks isolate instances", func(t *testing.T) {
		otherCfg := *cfg
		otherCfg.DBName = dbName + "_lock_test"
		if _, e := admin.ExecContext(ctx, "CREATE DATABASE `"+otherCfg.DBName+"`"); e != nil {
			t.Fatal(e)
		}
		defer admin.ExecContext(ctx, "DROP DATABASE `"+otherCfg.DBName+"`")
		second, e := store.Open(ctx, otherCfg.FormatDSN())
		if e != nil {
			t.Fatal(e)
		}
		defer second.DB.Close()
		for _, kind := range []string{"instance", "migrations"} {
			a, e := db.LockName(ctx, kind)
			if e != nil {
				t.Fatal(e)
			}
			b, e := second.LockName(ctx, kind)
			if e != nil || a == b {
				t.Fatal("database lock collision", e)
			}
			c1, e := db.DB.Conn(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer c1.Close()
			c2, e := second.DB.Conn(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer c2.Close()
			var acquired int
			if e = c1.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", a).Scan(&acquired); e != nil || acquired != 1 {
				t.Fatal(e, acquired)
			}
			defer c1.ExecContext(ctx, "SELECT RELEASE_LOCK(?)", a)
			if e = c2.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", b).Scan(&acquired); e != nil || acquired != 1 {
				t.Fatal(e, acquired)
			}
			defer c2.ExecContext(ctx, "SELECT RELEASE_LOCK(?)", b)
			if e = c2.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", a).Scan(&acquired); e != nil || acquired != 0 {
				t.Fatal("same database must remain exclusive", e, acquired)
			}
		}
	})
	t.Run("readiness reflects Telegram and stale queue", func(t *testing.T) {
		svc.Health = service.NewIngestionHealth("polling")
		defer func() { svc.Health = nil }()
		server := (&api.Server{Service: svc}).Handler()
		check := func(want int) {
			t.Helper()
			w := httptest.NewRecorder()
			server.ServeHTTP(w, httptest.NewRequest("GET", "/health/ready", nil))
			if w.Code != want {
				t.Fatalf("readiness %d: %s", w.Code, w.Body.String())
			}
		}
		svc.Health.Record(true)
		check(200)
		for i := 0; i < 3; i++ {
			svc.Health.Record(false)
		}
		check(503)
		svc.Health.Record(true)
		check(200)
		if e := db.Enqueue(ctx, domain.Update{ID: 771119}); e != nil {
			t.Fatal(e)
		}
		if _, e := db.DB.ExecContext(ctx, "UPDATE update_inbox SET created_at=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 6 MINUTE) WHERE update_id=771119"); e != nil {
			t.Fatal(e)
		}
		check(503)
		if _, e := db.DB.ExecContext(ctx, "UPDATE update_inbox SET status='done' WHERE update_id=771119"); e != nil {
			t.Fatal(e)
		}
		check(200)
	})
	t.Run("failed and uncertain punishment takeover", func(t *testing.T) {
		token := "takeover_regression"
		v := store.Verification{Token: token, ChatID: other.ID, UserID: 901998, Type: "math", Question: "1+1", AnswerHash: store.HashAnswer(token, "2"), FailAction: "kick", ExpiresAt: time.Now().Add(time.Hour)}
		if e := db.CreateVerification(ctx, v); e != nil {
			t.Fatal(e)
		}
		l := store.Log{EventKey: "takeover-permanent", ChatID: v.ChatID, UserID: v.UserID, Source: "manual", Decision: domain.Decision{Action: "unmute"}}
		mu.Lock()
		restrictionFailure = 403
		mu.Unlock()
		if e := svc.Punish(ctx, l, 42); e == nil {
			t.Fatal("expected failure")
		}
		current, e := db.Verification(ctx, token)
		if e != nil || current.Status != "pending" {
			t.Fatal("verification lost", current, e)
		}
		busy, e := db.VerificationTakenOver(ctx, v.ChatID, v.UserID)
		if e != nil || busy {
			t.Fatal("definite failure blocks verification", e)
		}
		l.EventKey = "takeover-uncertain"
		mu.Lock()
		restrictionFailure = 500
		mu.Unlock()
		if e := svc.Punish(ctx, l, 42); e == nil {
			t.Fatal("expected uncertain failure")
		}
		busy, e = db.VerificationTakenOver(ctx, v.ChatID, v.UserID)
		if e != nil || !busy {
			t.Fatal("uncertain result must retain takeover intent", e)
		}
		if e := svc.AnswerVerification(ctx, token, v.UserID, "2", "during-takeover"); e != nil {
			t.Fatal(e)
		}
		current, e = db.Verification(ctx, token)
		if e != nil || current.Status != "pending" {
			t.Fatal("verification bypassed takeover", current, e)
		}
		mu.Lock()
		restrictionFailure = 0
		mu.Unlock()
		if _, e := db.DB.ExecContext(ctx, "UPDATE punishment_workflows SET next_attempt_at=UTC_TIMESTAMP(6) WHERE event_key=?", l.EventKey); e != nil {
			t.Fatal(e)
		}
		if e := svc.SweepTakeovers(ctx); e != nil {
			t.Fatal(e)
		}
		current, e = db.Verification(ctx, token)
		if e != nil || current.Status != "cancelled" {
			t.Fatal("recovery failed", current, e)
		}
	})
	t.Run("historical partial migration recovery", func(t *testing.T) {
		if _, e := db.DB.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version IN ('003_keyword_buttons.sql','004_group_authorization.sql','005_welcome.sql','006_verification_notices.sql')"); e != nil {
			t.Fatal(e)
		}
		if e := db.Migrate(ctx); e != nil {
			t.Fatal("partial DDL not recoverable", e)
		}
	})
	t.Run("individual web credential audit and revocation", func(t *testing.T) {
		manager, e := settings.New(ctx, db, strings.Repeat("m", 32), settings.Config{SuperAdmins: []int64{42}, AI: settings.AI{BaseURL: "https://example.com/v1", TimeoutSeconds: 12, MaxTokens: 500, TokenParameter: "max_completion_tokens"}})
		if e != nil {
			t.Fatal(e)
		}
		svc.Runtime = manager
		defer func() { svc.Runtime = nil }()
		master := strings.Repeat("r", 32)
		web := (&api.Server{Service: svc, Config: config.Config{AdminToken: master}}).Handler()
		call := func(method, path, body string, cookie *http.Cookie, csrf string, root bool) *httptest.ResponseRecorder {
			r := httptest.NewRequest(method, path, strings.NewReader(body))
			if root {
				r.Header.Set("Authorization", "Bearer "+master)
			}
			if cookie != nil {
				r.AddCookie(cookie)
				r.Header.Set("X-CSRF-Token", csrf)
			}
			w := httptest.NewRecorder()
			web.ServeHTTP(w, r)
			return w
		}
		w := call("POST", "/api/v1/admin-credentials", `{"user_id":42}`, nil, "", true)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var issued map[string]any
		json.Unmarshal(w.Body.Bytes(), &issued)
		token := issued["token"].(string)
		w = call("POST", "/auth/login", testJSON(map[string]string{"token": token}), nil, "", false)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		cookie := w.Result().Cookies()[0]
		var session map[string]string
		json.Unmarshal(w.Body.Bytes(), &session)
		c := manager.Snapshot()
		c.PanelURL = "https://panel.example.com"
		w = call("PUT", "/api/v1/system", testJSON(c), cookie, session["csrf"], false)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var actor int64
		if e := db.DB.QueryRowContext(ctx, "SELECT actor_id FROM admin_audits WHERE action='system.settings' ORDER BY id DESC LIMIT 1").Scan(&actor); e != nil || actor != 42 {
			t.Fatal("missing named actor", actor, e)
		}
		c = manager.Snapshot()
		c.SuperAdmins = []int64{}
		w = call("PUT", "/api/v1/system", testJSON(c), nil, "", true)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		w = call("POST", "/api/v1/panel-ticket", "{}", cookie, session["csrf"], false)
		if w.Code != 401 {
			t.Fatal("removed admin session remains active", w.Code)
		}
		c = manager.Snapshot()
		c.SuperAdmins = []int64{42}
		if e = manager.Save(ctx, c, false); e != nil {
			t.Fatal(e)
		}
		w = call("POST", "/auth/login", testJSON(map[string]string{"token": token}), nil, "", false)
		if w.Code != 401 {
			t.Fatal("old credential resurrected", w.Code)
		}
	})
	t.Run("revocation cannot erase permission recovery", func(t *testing.T) {
		chat := domain.Chat{ID: -190997, Type: "supergroup", Title: "撤权恢复测试"}
		if e := db.RegisterGroup(ctx, chat); e != nil {
			t.Fatal(e)
		}
		const uid int64 = 901997
		if e := db.AuthorizeGroup(ctx, chat.ID, 42, "approved", "test"); e != nil {
			t.Fatal(e)
		}
		if e := db.ChangeSettings(ctx, chat.ID, 42, func(s *domain.Settings) error { s.VerificationEnabled = true; return nil }); e != nil {
			t.Fatal(e)
		}
		if e := svc.Join(ctx, chat, domain.User{ID: uid}); e != nil {
			t.Fatal(e)
		}
		v, e := db.ActiveVerification(ctx, chat.ID, uid)
		if e != nil {
			t.Fatal(e)
		}
		svc.Executor = revokingExecutor{MemberExecutor: svc.Bot, revoke: func() error { return db.AuthorizeGroup(ctx, chat.ID, 42, "revoked", "concurrent test") }}
		defer func() { svc.Executor = nil; _ = db.AuthorizeGroup(ctx, chat.ID, 42, "approved", "test cleanup") }()
		l := store.Log{EventKey: "revoke-race-regression", ChatID: chat.ID, UserID: uid, Source: "manual", Decision: domain.Decision{Action: "mute", Duration: 3600}}
		if e = svc.Punish(ctx, l, 42); e != store.ErrAuthorityChanged {
			t.Fatal("missing cancellation", e)
		}
		v, e = db.Verification(ctx, v.Token)
		if e != nil || v.Status != "releasing" {
			t.Fatal("release intent lost", v, e)
		}
		var status string
		if e = db.DB.QueryRowContext(ctx, "SELECT status FROM punishments WHERE event_key=?", l.EventKey).Scan(&status); e != nil || status != "skipped" {
			t.Fatal(status, e)
		}
		before := count("restrictChatMember")
		if e = svc.SweepTakeovers(ctx); e != nil {
			t.Fatal(e)
		}
		if count("restrictChatMember") <= before {
			t.Fatal("missing compensation")
		}
	})
	t.Run("legacy username trust is inert", func(t *testing.T) {
		l := domain.ListEntry{ChatID: chat.ID, Username: "auditsharedname", Kind: "white"}
		if e := db.SaveList(ctx, l, 42, false); e == nil {
			t.Fatal("username-only trust accepted")
		}
		if _, e := db.DB.ExecContext(ctx, "INSERT INTO list_entries(chat_id,user_id,username,kind,reason,created_by) VALUES(?,0,?,'white','legacy',42)", chat.ID, l.Username); e != nil {
			t.Fatal(e)
		}
		for _, id := range []int64{901995, 901996} {
			kind, e := db.ListStatus(ctx, chat.ID, id, l.Username)
			if e != nil || kind != "" {
				t.Fatal("inherited trust", kind, e)
			}
		}
		if e := db.SaveList(ctx, l, 42, true); e != nil {
			t.Fatal("cannot delete legacy entry", e)
		}
	})
	t.Run("finite mute has durable release", func(t *testing.T) {
		chat := domain.Chat{ID: -190993, Type: "supergroup", Title: "到期恢复测试"}
		if e := db.RegisterGroup(ctx, chat); e != nil {
			t.Fatal(e)
		}
		if e := db.AuthorizeGroup(ctx, chat.ID, 42, "approved", "test"); e != nil {
			t.Fatal(e)
		}
		l := store.Log{EventKey: "short-mute-regression", ChatID: chat.ID, UserID: 901993, Source: "manual", Decision: domain.Decision{Action: "mute", Duration: 30}}
		if e := svc.Punish(ctx, l, 42); e != nil {
			t.Fatal(e)
		}
		var needed bool
		if e := db.DB.QueryRowContext(ctx, "SELECT release_needed FROM punishment_workflows WHERE event_key=?", l.EventKey).Scan(&needed); e != nil || !needed {
			t.Fatal("no expiry task", e)
		}
		if _, e := db.DB.ExecContext(ctx, "UPDATE punishment_workflows SET release_at=UTC_TIMESTAMP(6) WHERE event_key=?", l.EventKey); e != nil {
			t.Fatal(e)
		}
		before := count("restrictChatMember")
		if e := svc.SweepTakeovers(ctx); e != nil {
			t.Fatal(e)
		}
		if count("restrictChatMember") <= before {
			t.Fatal("expiry did not restore permissions")
		}
		if e := db.DB.QueryRowContext(ctx, "SELECT release_needed FROM punishment_workflows WHERE event_key=?", l.EventKey).Scan(&needed); e != nil || needed {
			t.Fatal("expiry not committed", e)
		}
	})
	t.Run("old expiry preserves a newer verification", func(t *testing.T) {
		l := store.Log{EventKey: "expiry-before-new-verification", ChatID: chat.ID, UserID: 901989, Source: "manual", Decision: domain.Decision{Action: "mute", Duration: 30}}
		if e := svc.Punish(ctx, l, 42); e != nil {
			t.Fatal(e)
		}
		v := store.Verification{Token: "new-verification-after-mute", ChatID: chat.ID, UserID: l.UserID, Type: "button", FailAction: "kick", ExpiresAt: time.Now().Add(time.Minute)}
		if e := db.CreateVerification(ctx, v); e != nil {
			t.Fatal(e)
		}
		if _, e := db.DB.ExecContext(ctx, "UPDATE punishment_workflows SET release_at=UTC_TIMESTAMP(6) WHERE event_key=?", l.EventKey); e != nil {
			t.Fatal(e)
		}
		before := count("restrictChatMember")
		if e := svc.SweepTakeovers(ctx); e != nil {
			t.Fatal(e)
		}
		if count("restrictChatMember") != before {
			t.Fatal("old expiry unmuted newer verification")
		}
		var status string
		if e := db.DB.QueryRowContext(ctx, "SELECT status FROM verification_sessions WHERE token=?", v.Token).Scan(&status); e != nil || status != "pending" {
			t.Fatal(status, e)
		}
	})
	t.Run("definite mute rejection clears release intent", func(t *testing.T) {
		mu.Lock()
		restrictionFailure = 403
		mu.Unlock()
		defer func() { mu.Lock(); restrictionFailure = 0; mu.Unlock() }()
		l := store.Log{EventKey: "rejected-finite-mute", ChatID: chat.ID, UserID: 901988, Source: "manual", Decision: domain.Decision{Action: "mute", Duration: 30}}
		if e := svc.Punish(ctx, l, 42); e == nil {
			t.Fatal("expected rejection")
		}
		var needed bool
		var status string
		if e := db.DB.QueryRowContext(ctx, "SELECT p.status,w.release_needed FROM punishments p JOIN punishment_workflows w ON w.event_key=p.event_key WHERE p.event_key=?", l.EventKey).Scan(&status, &needed); e != nil || status != "failed" || needed {
			t.Fatal(status, needed, e)
		}
	})
	t.Run("kick resumes unban without repeating ban", func(t *testing.T) {
		x := &partialKickExecutor{MemberExecutor: svc.Bot, fail: true}
		svc.Executor = x
		defer func() { svc.Executor = nil }()
		l := store.Log{EventKey: "partial-kick-regression", ChatID: chat.ID, UserID: 901992, Source: "manual", Decision: domain.Decision{Action: "kick"}}
		if e := svc.Punish(ctx, l, 42); e == nil {
			t.Fatal("missing simulated failure")
		}
		p, e := db.PreparePunishment(ctx, l, 42)
		if e != nil || p.Status != "pending" {
			t.Fatal("partial kick became terminal", p, e)
		}
		if e = svc.Punish(ctx, l, 42); e != nil {
			t.Fatal(e)
		}
		if x.bans != 1 {
			t.Fatal("ban repeated", x.bans)
		}
	})
	t.Run("recovery batch reserves space for different groups", func(t *testing.T) {
		for i := 0; i < 105; i++ {
			l := store.Log{EventKey: fmt.Sprintf("fairness:%d", i), ChatID: -190991, UserID: int64(88000 + i), Source: "automatic", Decision: domain.Decision{Action: "warn"}}
			if i == 104 {
				l.ChatID = -190990
			}
			if _, e := db.PreparePunishment(ctx, l, 0); e != nil {
				t.Fatal(e)
			}
		}
		rows, e := db.PendingTakeovers(ctx)
		if e != nil {
			t.Fatal(e)
		}
		slow := 0
		fast := false
		for _, r := range rows {
			if r.Log.ChatID == -190991 {
				slow++
			}
			if r.Log.ChatID == -190990 {
				fast = true
			}
		}
		if slow > 2 || !fast {
			t.Fatal("batch starvation", slow, fast)
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
	t.Run("manual AI punishment and repeat evidence", func(t *testing.T) {
		g := domain.Chat{ID: -10110, Type: "supergroup", Title: "AI policy test"}
		if e := svc.Group(ctx, g); e != nil {
			t.Fatal(e)
		}
		if e := db.AuthorizeGroup(ctx, g.ID, 42, "approved", "test"); e != nil {
			t.Fatal(e)
		}
		if e := db.ChangeSettings(ctx, g.ID, 42, func(v *domain.Settings) error {
			v.AIEnabled = true
			v.AIThreshold = 30
			v.ModerationEnabled = true
			v.SpamEnabled = false
			v.NewMemberProtection = false
			v.Rules = map[string]domain.RuleSetting{"url": {Enabled: true, Score: 30}}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		oldAI := svc.AI
		defer func() { svc.AI = oldAI }()
		aiCalls := 0
		svc.AI = reviewFunc(func(_ context.Context, n domain.Normalized, _ domain.Risk) (domain.AIResult, error) {
			aiCalls++
			return domain.AIResult{IsAd: strings.Contains(n.Text, "special sample"), Confidence: .7, Category: "promotion", Severity: "medium", Reason: "test", RecommendedAction: "delete"}, nil
		})
		target := domain.Message{ID: 99001, Chat: g, From: &domain.User{ID: 99110}, Text: "special sample"}
		request := domain.Message{ID: 99002, Chat: g, From: &domain.User{ID: 42}, Reply: &target, Text: "/check"}
		before := count("deleteMessage")
		messagesBefore := count("sendMessage")
		if e := svc.Review(ctx, 99002, request); e != nil {
			t.Fatal(e)
		}
		if count("deleteMessage") != before+1 {
			t.Fatal("manual ad was not deleted")
		}
		if count("sendMessage") != messagesBefore+1 {
			t.Fatal("manual review should deliver one combined warning card")
		}
		var warnings int
		if e := db.DB.QueryRow("SELECT COUNT(*) FROM punishments WHERE chat_id=? AND source='review' AND status='done' AND decision->>'$.action'='warn'", g.ID).Scan(&warnings); e != nil || warnings != 1 {
			t.Fatal(warnings, e)
		}
		if e := svc.Review(ctx, 99003, request); e != nil {
			t.Fatal(e)
		}
		if count("deleteMessage") != before+1 {
			t.Fatal("same message punished twice")
		}
		target.ID = 99004
		l, e := svc.ModerateResult(ctx, 99004, target)
		if e != nil || l.Decision.Action != "mute" || l.Decision.Reason != "repeated_ad" {
			t.Fatal(l.Decision, e)
		}
		target.ID = 99005
		target.From = &domain.User{ID: 99111}
		l, e = svc.ModerateResult(ctx, 99005, target)
		if e != nil || l.Decision.Action != "allow" {
			t.Fatal("another user inherited evidence", l.Decision, e)
		}
		original, e := db.GetLog(ctx, fmt.Sprintf("review:%d:%d", g.ID, 99001))
		if e != nil {
			t.Fatal(e)
		}
		if e = db.Feedback(ctx, g.ID, original.ID, 42, "false positive"); e != nil {
			t.Fatal(e)
		}
		target.ID = 99006
		target.From = &domain.User{ID: 99110}
		l, e = svc.ModerateResult(ctx, 99006, target)
		if e != nil || l.Decision.Action != "allow" {
			t.Fatal("feedback did not clear evidence", l.Decision, e)
		}
		for i, score := range []int{29, 30, 90} {
			if e = db.ChangeSettings(ctx, g.ID, 42, func(v *domain.Settings) error {
				v.Rules["url"] = domain.RuleSetting{Enabled: true, Score: score}
				return nil
			}); e != nil {
				t.Fatal(e)
			}
			callsBefore := aiCalls
			m := domain.Message{ID: int64(99100 + i), Chat: g, From: &domain.User{ID: 99200}, Text: fmt.Sprintf("https://example.invalid/%d", i)}
			if _, e = svc.ModerateResult(ctx, m.ID, m); e != nil {
				t.Fatal(e)
			}
			want := callsBefore
			if score >= 30 {
				want++
			}
			if aiCalls != want {
				t.Fatal("AI threshold", score, aiCalls, want)
			}
		}
	})
	t.Run("self verification restores only verification restrictions", func(t *testing.T) {
		g := domain.Chat{ID: -10120, Type: "supergroup", Title: "Self verification"}
		if err := svc.Group(ctx, g); err != nil {
			t.Fatal(err)
		}
		if err := db.AuthorizeGroup(ctx, g.ID, 42, "approved", "test"); err != nil {
			t.Fatal(err)
		}
		if err := db.ChangeSettings(ctx, g.ID, 42, func(v *domain.Settings) error {
			v.VerificationEnabled = true
			v.VerificationType = "button"
			v.WelcomeEnabled = false
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		create := func(uid int64, action string) store.Verification {
			t.Helper()
			u := domain.User{ID: uid}
			mu.Lock()
			roles[uid] = "restricted"
			mu.Unlock()
			if err := db.Join(ctx, g.ID, u, "restricted"); err != nil {
				t.Fatal(err)
			}
			v := store.Verification{Token: fmt.Sprintf("self-%d", uid), ChatID: g.ID, UserID: uid, Type: "math", Question: "1+1", AnswerHash: store.HashAnswer(fmt.Sprintf("self-%d", uid), "2"), FailAction: action, ExpiresAt: time.Now().Add(-time.Minute)}
			if err := db.CreateVerification(ctx, v); err != nil {
				t.Fatal(err)
			}
			if _, err := db.DB.Exec("UPDATE verification_sessions SET status='expired',notice_done=TRUE WHERE token=?", v.Token); err != nil {
				t.Fatal(err)
			}
			return v
		}
		msg := func(uid int64) domain.Message {
			return domain.Message{From: &domain.User{ID: uid}, Chat: domain.Chat{ID: uid, Type: "private"}}
		}
		v := create(99301, "mute")
		if err := svc.SelfVerification(ctx, msg(99302), v.Token); err != nil {
			t.Fatal(err)
		}
		current, err := db.Verification(ctx, v.Token)
		if err != nil || current.Status != "expired" {
			t.Fatal(current, err)
		}
		if err := svc.SelfVerificationMenu(ctx, msg(v.UserID)); err != nil {
			t.Fatal(err)
		}
		if err := svc.SelfVerification(ctx, msg(v.UserID), v.Token); err != nil {
			t.Fatal(err)
		}
		current, err = db.Verification(ctx, v.Token)
		if err != nil || current.Status != "pending" || current.Type != "button" {
			t.Fatal(current, err)
		}
		before := count("restrictChatMember")
		if err := svc.AnswerVerification(ctx, v.Token, v.UserID, "human", "self-answer"); err != nil {
			t.Fatal(err)
		}
		current, err = db.Verification(ctx, v.Token)
		if err != nil || current.Status != "verified" || count("restrictChatMember") != before+1 {
			t.Fatal(current, err)
		}
		for i, action := range []string{"kick", "ban"} {
			v = create(int64(99310+i), action)
			if ok, err := db.CanSelfVerify(ctx, v.Token, v.UserID); err != nil || ok {
				t.Fatal("unsafe failure action", action, ok, err)
			}
		}
		v = create(99320, "mute")
		l := store.Log{EventKey: "self-manual-mute", ChatID: g.ID, UserID: v.UserID, Source: "manual", Decision: domain.Decision{Action: "mute", Duration: 3600}}
		if err := svc.Punish(ctx, l, 42); err != nil {
			t.Fatal(err)
		}
		if ok, err := db.CanSelfVerify(ctx, v.Token, v.UserID); err != nil || ok {
			t.Fatal("manual mute bypass", ok, err)
		}
		v = create(99321, "mute")
		if err := svc.ObserveVerificationTakeover(ctx, domain.MemberUpdate{Date: time.Now().Unix(), Chat: g, From: domain.User{ID: 42}, Old: domain.Member{Status: "restricted", IsMember: true}, New: domain.Member{Status: "restricted", IsMember: true, User: domain.User{ID: v.UserID}}}); err != nil {
			t.Fatal(err)
		}
		if ok, err := db.CanSelfVerify(ctx, v.Token, v.UserID); err != nil || ok {
			t.Fatal("external mute bypass", ok, err)
		}
	})
}

type reviewFunc func(context.Context, domain.Normalized, domain.Risk) (domain.AIResult, error)

func (f reviewFunc) Review(ctx context.Context, n domain.Normalized, r domain.Risk) (domain.AIResult, error) {
	return f(ctx, n, r)
}

func testJSON(v any) string {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return string(b)
}

type revokingExecutor struct {
	service.MemberExecutor
	revoke func() error
}

func (x revokingExecutor) Restrict(ctx context.Context, chat, user int64, seconds int) error {
	if err := x.revoke(); err != nil {
		return err
	}
	return x.MemberExecutor.Restrict(ctx, chat, user, seconds)
}

type partialKickExecutor struct {
	service.MemberExecutor
	fail bool
	bans int
}

func (x *partialKickExecutor) Ban(ctx context.Context, chat, user int64) error {
	x.bans++
	return x.MemberExecutor.Ban(ctx, chat, user)
}
func (x *partialKickExecutor) Unban(ctx context.Context, chat, user int64) error {
	if x.fail {
		x.fail = false
		return &telegram.APIError{Code: 403, Description: "simulated temporary permission loss"}
	}
	return x.MemberExecutor.Unban(ctx, chat, user)
}
