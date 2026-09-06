package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"tgguard/internal/state"
	"tgguard/internal/store"
	"tgguard/internal/telegram"
)

func TestExpiredVerificationOnlyUnbansOwnedKick(t *testing.T) {
	for _, owned := range []bool{false, true} {
		name := "external ban"
		if owned {
			name = "own interrupted kick"
		}
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			r := miniredis.RunT(t)
			cache := state.New(r.Addr(), "")
			defer cache.R.Close()
			unbans := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/getChatMember":
					json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"status": "kicked", "user": map[string]any{"id": 42}}})
				case "/unbanChatMember":
					unbans++
					w.Write([]byte(`{"ok":true,"result":true}`))
				default:
					t.Errorf("unexpected Telegram method %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			mock.ExpectQuery("SELECT settings FROM group_settings").WithArgs(int64(-100)).WillReturnRows(sqlmock.NewRows([]string{"settings"}))
			mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
			mock.ExpectQuery("SELECT kind FROM list_entries").WillReturnRows(sqlmock.NewRows([]string{"kind"}))
			mock.ExpectBegin()
			mock.ExpectExec("UPDATE verification_sessions SET status=").WithArgs("expired", "expired", "token", "expiring").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
			mock.ExpectQuery("SELECT notice_done").WithArgs("token").WillReturnRows(sqlmock.NewRows([]string{"notice_done"}).AddRow(false))
			mock.ExpectExec("UPDATE verification_sessions SET notice_done=TRUE").WithArgs("token").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("SELECT settings FROM group_settings").WithArgs(int64(-100)).WillReturnRows(sqlmock.NewRows([]string{"settings"}))
			s := &Service{Store: &store.Store{DB: db}, State: cache, Bot: &telegram.Client{BaseURL: srv.URL, HTTP: srv.Client()}}
			err = s.finishVerification(context.Background(), store.Verification{Token: "token", ChatID: -100, UserID: 42, Status: "expiring", FailAction: "kick", KickStarted: owned})
			if err != nil {
				t.Fatal(err)
			}
			if (unbans == 1) != owned {
				t.Fatalf("owned=%t unbans=%d", owned, unbans)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCompletedVerificationNoticeIgnoresStaleRecovery(t *testing.T) {
	db, mock, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT settings FROM group_settings").WillReturnRows(sqlmock.NewRows([]string{"settings"}))
	mock.ExpectQuery("SELECT notice_done").WithArgs("done").WillReturnRows(sqlmock.NewRows([]string{"notice_done"}).AddRow(true))
	s := &Service{Store: &store.Store{DB: db}}
	if e = s.finishVerification(context.Background(), store.Verification{Token: "done", Status: "verified"}); e != nil {
		t.Fatal(e)
	}
	if e = mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
