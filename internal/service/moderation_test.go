package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"tgguard/internal/domain"
	"tgguard/internal/state"
	"tgguard/internal/store"
	"tgguard/internal/telegram"
)

func TestPunishmentRechecksAdminBeforeDestructiveCalls(t *testing.T) {
	db, mock, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	r := miniredis.RunT(t)
	cache := state.New(r.Addr(), "")
	defer cache.R.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/getChatMember" {
			t.Errorf("destructive API called: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"status": "administrator", "user": map[string]any{"id": 42}}})
	}))
	defer srv.Close()
	decision := domain.Decision{Action: "ban", Delete: true}
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
	mock.ExpectExec("INSERT IGNORE INTO punishments").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT decision,status,deleted,acted,created_at FROM punishments").WillReturnRows(sqlmock.NewRows([]string{"decision", "status", "deleted", "acted", "created_at"}).AddRow(store.JSON(decision), "pending", false, false, time.Now()))
	mock.ExpectExec("UPDATE punishments SET status='skipped'").WithArgs("test").WillReturnResult(sqlmock.NewResult(0, 1))
	s := &Service{Store: &store.Store{DB: db}, State: cache, Bot: &telegram.Client{BaseURL: srv.URL, HTTP: srv.Client()}}
	if e = s.Punish(context.Background(), store.Log{EventKey: "test", ChatID: -100, UserID: 42, MessageID: 1, Decision: decision, Source: "automatic"}, 0); e != nil {
		t.Fatal(e)
	}
	if e = mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestCompletedPunishmentRetryDoesNotCallTelegram(t *testing.T) {
	db, mock, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	r := miniredis.RunT(t)
	cache := state.New(r.Addr(), "")
	defer cache.R.Close()
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
	mock.ExpectExec("INSERT IGNORE INTO punishments").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT decision,status,deleted,acted,created_at FROM punishments").WillReturnRows(sqlmock.NewRows([]string{"decision", "status", "deleted", "acted", "created_at"}).AddRow(`{"action":"ban"}`, "done", true, true, time.Now()))
	s := &Service{Store: &store.Store{DB: db}, State: cache}
	if e = s.Punish(context.Background(), store.Log{EventKey: "done", ChatID: -100, UserID: 42, Decision: domain.Decision{Action: "ban"}}, 0); e != nil {
		t.Fatal(e)
	}
	if e = mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
