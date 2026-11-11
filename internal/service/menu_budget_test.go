package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"tgguard/internal/domain"
	"tgguard/internal/store"
	"tgguard/internal/telegram"
)

func TestRepeatedPrivateMenusStopBeforeOutboundCalls(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer srv.Close()
	s := &Service{State: menuTestState(t), Bot: &telegram.Client{BaseURL: srv.URL, HTTP: srv.Client()}}
	m := domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42}}
	for i := 0; i < 20; i++ {
		if err := s.Home(context.Background(), m); err != nil {
			t.Fatal("throttle must not retry inbox job", err)
		}
	}
	if calls != 6 {
		t.Fatalf("requests bypassed admission: %d", calls)
	}
}

func TestRoleDiscoveryBoundedAndDisplayCached(t *testing.T) {
	for _, admin := range []bool{false, true} {
		t.Run(map[bool]string{false: "bounded", true: "cache"}[admin], func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			rows := sqlmock.NewRows([]string{"chat_id", "title"})
			for i := int64(1); i <= 11; i++ {
				rows.AddRow(-i, "group")
			}
			mock.ExpectQuery("SELECT chat_id,title FROM bot_groups").WithArgs(int64(0)).WillReturnRows(rows)
			count := 10
			if admin {
				count = 1
			}
			for i := 0; i < count; i++ {
				mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
			}
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				status := "member"
				if admin {
					status = "administrator"
				}
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]string{"status": status}})
			}))
			defer srv.Close()
			s := &Service{Store: &store.Store{DB: db}, State: menuTestState(t), Bot: &telegram.Client{BaseURL: srv.URL, HTTP: srv.Client()}}
			manage, err := s.canManageGroups(context.Background(), 42)
			if admin {
				if err != nil || !manage {
					t.Fatal(manage, err)
				}
				if cached, err := s.canManageGroups(context.Background(), 42); err != nil || !cached {
					t.Fatal("display cache missed", err)
				}
			} else if err == nil || manage {
				t.Fatal("incomplete discovery misclassified user", manage, err)
			}
			if calls != count {
				t.Fatal("unbounded or repeated lookup", calls)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDiscoveryPaginationDoesNotSkipUnqueriedGroups(t *testing.T) {
	for _, self := range []bool{false, true} {
		t.Run(map[bool]string{false: "management", true: "self"}[self], func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			rows := sqlmock.NewRows([]string{"chat_id", "title"})
			for i := int64(1); i <= 11; i++ {
				rows.AddRow(-1000-i, "secret-group")
			}
			mock.ExpectQuery("SELECT chat_id,title FROM bot_groups").WithArgs(int64(0)).WillReturnRows(rows)
			if !self {
				for i := 0; i < 6; i++ {
					mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				}
			}
			lookups, reply, next := 0, "", false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/getChatMember" {
					lookups++
					if next {
						status := "administrator"
						if self {
							status = "restricted"
						}
						json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"status": status, "is_member": true, "user": map[string]any{"id": 42}}})
						return
					}
					w.Write([]byte(`{"ok":true,"result":{"status":"member","user":{"id":42}}}`))
					return
				}
				var in map[string]any
				json.NewDecoder(r.Body).Decode(&in)
				b, _ := json.Marshal(in)
				reply = string(b)
				w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
			}))
			defer srv.Close()
			st := menuTestState(t)
			s := &Service{Store: &store.Store{DB: db}, State: st, Bot: &telegram.Client{BaseURL: srv.URL, HTTP: srv.Client(), State: st}}
			m := domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42}}
			var err error
			if self {
				err = s.SelfVerificationMenu(context.Background(), m)
			} else {
				err = s.MyGroups(context.Background(), m, 0)
			}
			if err != nil || lookups != 5 || strings.Contains(reply, "secret-group") {
				t.Fatalf("unsafe/unbounded page: %v lookups=%d reply=%s", err, lookups, reply)
			}
			if self {
				if !strings.Contains(reply, "sp:-1005") {
					t.Fatal("self cursor skipped unchecked group", reply)
				}
			} else {
				if !strings.Contains(reply, "menu:groups:-1005:home") {
					t.Fatal("management cursor skipped unchecked group", reply)
				}
			}
			// The first unchecked group remains reachable on the next page.
			next = true
			st.R.Del(context.Background(), "telegram:menu:lookup", "telegram:menu:all", "telegram:send:42")
			mock.ExpectQuery("SELECT chat_id,title FROM bot_groups").WithArgs(int64(-1005)).WillReturnRows(sqlmock.NewRows([]string{"chat_id", "title"}).AddRow(-1006, "my-later-group"))
			if self {
				err = s.SelfVerificationMenu(context.Background(), m, -1005)
			} else {
				mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				err = s.MyGroups(context.Background(), m, -1005)
			}
			if err != nil || lookups != 6 || !strings.Contains(reply, "my-later-group") {
				t.Fatalf("legitimate next page lost: %v %s", err, reply)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExpiredMenuResponsesShareBudget(t *testing.T) {
	st := menuTestState(t)
	for i := 0; i < 8; i++ {
		st.Limit(context.Background(), "telegram:menu:all", 8, time.Second)
	}
	// No HTTP transport is configured: neither acknowledgement nor error text may escape.
	s := &Service{State: st, Bot: &telegram.Client{State: st}}
	for _, data := range []string{"gmc:invalid", "gm:-100:settings"} {
		for user := int64(1); user <= 10; user++ {
			err := s.MenuCallback(context.Background(), domain.Callback{Data: data, From: domain.User{ID: user}, Message: &domain.Message{Chat: domain.Chat{ID: user, Type: "private"}}})
			if err != nil {
				t.Fatal("limited response requeues job", err)
			}
		}
	}
}

func TestSlowDiscoveryRetainsTimeToSendCursor(t *testing.T) {
	for _, self := range []bool{false, true} {
		t.Run(map[bool]string{false: "management", true: "self"}[self], func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			rows := sqlmock.NewRows([]string{"chat_id", "title"})
			for i := int64(1); i <= 5; i++ {
				rows.AddRow(-1000-i, "hidden")
			}
			mock.ExpectQuery("SELECT chat_id,title FROM bot_groups").WillReturnRows(rows)
			if !self {
				for i := 0; i < 3; i++ {
					mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
				}
			}
			reply := ""
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/getChatMember" {
					select {
					case <-time.After(700 * time.Millisecond):
						w.Write([]byte(`{"ok":true,"result":{"status":"member","user":{"id":42}}}`))
					case <-r.Context().Done():
					}
					return
				}
				var in map[string]any
				json.NewDecoder(r.Body).Decode(&in)
				b, _ := json.Marshal(in)
				reply = string(b)
				w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
			}))
			defer srv.Close()
			st := menuTestState(t)
			s := &Service{State: st, Store: &store.Store{DB: db}, Bot: &telegram.Client{State: st, BaseURL: srv.URL, HTTP: srv.Client()}}
			m := domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42}}
			var err error
			cursor := "menu:groups:-1002:home"
			if self {
				cursor = "sp:-1002"
				err = s.SelfVerificationMenu(context.Background(), m)
			} else {
				err = s.MyGroups(context.Background(), m, 0)
			}
			if err != nil || !strings.Contains(reply, cursor) {
				t.Fatalf("slow scan lost continuation: %v %s", err, reply)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
