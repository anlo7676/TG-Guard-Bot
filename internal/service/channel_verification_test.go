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
	"github.com/alicebob/miniredis/v2"

	"tgguard/internal/domain"
	"tgguard/internal/state"
	"tgguard/internal/store"
	"tgguard/internal/telegram"
)

func TestChannelVerificationRequiresLiveMembershipAndBotAdmin(t *testing.T) {
	for _, tc := range []struct {
		name, chatType, botStatus, status string
		member, fail, want, wantErr       bool
	}{
		{"member", "channel", "administrator", "member", false, false, true, false},
		{"admin", "channel", "administrator", "administrator", false, false, true, false},
		{"creator", "channel", "administrator", "creator", false, false, true, false},
		{"left", "channel", "administrator", "left", false, false, false, false},
		{"banned", "channel", "administrator", "kicked", false, false, false, false},
		{"restricted nonmember", "channel", "administrator", "restricted", false, false, false, false},
		{"API failure", "channel", "administrator", "member", false, true, false, true},
		{"bot demoted", "channel", "member", "member", false, false, false, true},
		{"wrong target", "supergroup", "administrator", "member", false, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/getChat" {
					json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"type": tc.chatType}})
					return
				}
				var body struct {
					User int64 `json:"user_id"`
					Chat int64 `json:"chat_id"`
				}
				json.NewDecoder(r.Body).Decode(&body)
				if body.Chat != -1001 {
					t.Error("wrong channel")
				}
				status := tc.status
				if body.User == 99 {
					status = tc.botStatus
				} else if body.User != 42 {
					t.Error("wrong user")
				}
				if body.User == 42 && tc.fail {
					w.Write([]byte(`{"ok":false,"error_code":403,"description":"unavailable"}`))
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"status": status, "is_member": tc.member}})
			}))
			defer srv.Close()
			s := &Service{Bot: &telegram.Client{ID: 99, BaseURL: srv.URL, HTTP: srv.Client()}}
			settings := domain.DefaultSettings()
			settings.VerificationChannelEnabled = true
			settings.VerificationChannelID = -1001
			settings.VerificationChannelURL = "https://t.me/example"
			ok, err := s.verificationChannelMember(context.Background(), 42, settings)
			if ok != tc.want || (err != nil) != tc.wantErr {
				t.Fatal(ok, err)
			}
		})
	}
	if ok, err := (&Service{}).verificationChannelMember(context.Background(), 42, domain.DefaultSettings()); !ok || err != nil {
		t.Fatal("disabled channel contacted Telegram")
	}
}

func TestNonmemberCannotAnswerJoinOrSelfVerification(t *testing.T) {
	for _, token := range []string{"join-token", "self_token"} {
		t.Run(token, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			r := miniredis.RunT(t)
			cache := state.New(r.Addr(), "")
			defer cache.R.Close()
			for i := 0; i < 2; i++ {
				mock.ExpectQuery("SELECT token,chat_id").WithArgs(token).WillReturnRows(sqlmock.NewRows([]string{"token", "chat_id", "user_id", "challenge_type", "question", "answer_hash", "status", "attempts", "fail_action", "expires_at", "prompt_id", "kick_started"}).AddRow(token, -100, 42, "button", "human?", store.HashAnswer(token, "human"), "pending", 0, "mute", time.Now().Add(time.Minute), 0, false))
			}
			if !strings.HasPrefix(token, "self_") {
				mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"busy"}).AddRow(false))
			}
			settings := domain.DefaultSettings()
			settings.VerificationChannelEnabled = true
			settings.VerificationChannelID = -1001
			settings.VerificationChannelURL = "https://t.me/example"
			raw, _ := json.Marshal(settings)
			mock.ExpectQuery("SELECT settings FROM group_settings").WillReturnRows(sqlmock.NewRows([]string{"settings"}).AddRow(string(raw)))
			notices := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/getChat":
					w.Write([]byte(`{"ok":true,"result":{"type":"channel"}}`))
				case "/getChatMember":
					var body struct {
						User int64 `json:"user_id"`
					}
					json.NewDecoder(r.Body).Decode(&body)
					status := "left"
					if body.User == 99 {
						status = "administrator"
					}
					json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"status": status}})
				case "/sendMessage":
					notices++
					w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
				default:
					t.Errorf("unexpected Telegram operation %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			s := &Service{Store: &store.Store{DB: db}, State: cache, Bot: &telegram.Client{ID: 99, BaseURL: srv.URL, HTTP: srv.Client()}}
			if err := s.AnswerVerification(context.Background(), token, 42, "human"); err != nil {
				t.Fatal(err)
			}
			if notices != 1 {
				t.Fatal("missing join prompt")
			}
			// No answer update or restriction call is expected for a nonmember.
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
