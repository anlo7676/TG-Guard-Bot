package service

import (
	"context"
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"tgguard/internal/domain"
	"tgguard/internal/state"
	"tgguard/internal/store"
	"tgguard/internal/telegram"
)

func TestGroupMenuRechecksRevokedAndForgedAccess(t *testing.T) {
	for _, status := range []string{"member", "left", "administrator"} {
		t.Run(status, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			checked, sent := 0, ""
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in map[string]any
				json.NewDecoder(r.Body).Decode(&in)
				switch r.URL.Path {
				case "/getChatMember":
					checked++
					if in["chat_id"] != float64(-1002) || in["user_id"] != float64(42) {
						t.Error("wrong permission scope")
					}
					json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"status": status, "user": map[string]any{"id": 42}}})
				case "/sendMessage":
					if in["chat_id"] != float64(42) {
						t.Error("settings leaked to group")
					}
					sent, _ = in["text"].(string)
					w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
				default:
					t.Error(r.URL.Path)
				}
			}))
			defer srv.Close()
			s := &Service{Store: &store.Store{DB: db}, State: menuTestState(t), Bot: &telegram.Client{BaseURL: srv.URL, HTTP: srv.Client()}}
			if status == "administrator" {
				mock.ExpectQuery("SELECT chat_id,title FROM bot_groups").WithArgs(int64(-1002)).WillReturnRows(sqlmock.NewRows([]string{"chat_id", "title"}).AddRow(-1002, "群 B"))
				mock.ExpectQuery("SELECT settings FROM group_settings").WithArgs(int64(-1002)).WillReturnRows(sqlmock.NewRows([]string{"settings"}))
			}
			action := "gm:-1002:set:ai_enabled:true"
			if status == "administrator" {
				action = "gm:-1002:settings"
			}
			if err := s.GroupMenu(context.Background(), domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42}}, action); err != nil {
				t.Fatal(err)
			}
			if checked != 1 {
				t.Fatal("live permission check missing")
			}
			if status == "administrator" {
				if !strings.Contains(sent, "群 B") {
					t.Fatal("selected group missing")
				}
			} else if !strings.Contains(sent, "没有管理") {
				t.Fatal("revoked access not denied")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestGroupMenuRejectsForeignPrivateChat(t *testing.T) {
	s := &Service{}
	if err := s.GroupMenu(context.Background(), domain.Message{Chat: domain.Chat{ID: 43, Type: "private"}, From: &domain.User{ID: 42}}, "gm:-1002:set:ai_enabled:true"); err != nil {
		t.Fatal(err)
	}
}
func TestMenuSettingIsExplicitAndScoped(t *testing.T) {
	v := domain.DefaultSettings()
	v.RateLimit = 17
	for i := 0; i < 2; i++ {
		if err := setMenuField(&v, "ai_enabled", "true"); err != nil {
			t.Fatal(err)
		}
	}
	if !v.AIEnabled || v.RateLimit != 17 {
		t.Fatal("retry toggled setting or unrelated field overwritten")
	}
	for _, key := range []string{"super_admins", "log_channel", "rules"} {
		if err := setMenuField(&v, key, "true"); err == nil {
			t.Fatal("accepted unsupported field")
		}
	}
	if err := setMenuField(&v, "ai_enabled", "null"); err == nil {
		t.Fatal("accepted nonboolean value")
	}
}
func TestMyGroupsHidesOtherGroups(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT chat_id,title FROM bot_groups").WithArgs(int64(0)).WillReturnRows(sqlmock.NewRows([]string{"chat_id", "title"}).AddRow(-1001, "我的群").AddRow(-1002, "其他人的群"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		if r.URL.Path == "/getChatMember" {
			status := "member"
			if in["chat_id"] == float64(-1001) {
				status = "creator"
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"status": status}})
			return
		}
		b, _ := json.Marshal(in)
		if strings.Contains(string(b), "其他人的群") || !strings.Contains(string(b), "gmc:") {
			t.Error("group list not permission filtered")
		}
		w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer srv.Close()
	s := &Service{Store: &store.Store{DB: db}, State: menuTestState(t), Bot: &telegram.Client{BaseURL: srv.URL, HTTP: srv.Client()}}
	if err := s.MyGroups(context.Background(), domain.Message{Chat: domain.Chat{ID: 42, Type: "private"}, From: &domain.User{ID: 42}}, 0); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func menuTestState(t *testing.T) *state.State {
	m := miniredis.RunT(t)
	r := state.New(m.Addr(), "")
	t.Cleanup(func() { r.R.Close() })
	return r
}
