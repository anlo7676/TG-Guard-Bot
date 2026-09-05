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
			result = map[string]any{"id": -1001, "type": "supergroup", "permissions": map[string]any{"can_send_messages": true}}
		case "sendMessage":
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
		m.Text = "帮助 | 私聊创建成功"
		handle(m)
		ks, _ := db.Keywords(ctx, chat.ID)
		if len(ks) != 2 {
			t.Fatal("keyword creation failed")
		}
		m.Text = "重复提交 | 不应创建"
		handle(m)
		ks, _ = db.Keywords(ctx, chat.ID)
		if len(ks) != 2 {
			t.Fatal("consumed prompt reused")
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
		if w := request("PUT", "/api/v1/groups/-1001/settings", `{"rate_limit":24}`); w.Code != 200 {
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
