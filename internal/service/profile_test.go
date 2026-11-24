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

func TestProfileAvatarIsolationAndDomainRemoval(t *testing.T) {
	for _, tc := range []struct {
		name, domain string
		chat         int64
		want         bool
	}{
		{"same group", "heiying.co", -100, true}, {"other group", "", -200, false}, {"removed domain", "removed.example", -100, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			r := miniredis.RunT(t)
			cache := state.New(r.Addr(), "")
			defer cache.R.Close()
			hash := strings.Repeat("a", 64)
			if err := cache.Put(context.Background(), "profile:v1:42:true", profileData{BioAvailable: true, AvatarAvailable: true, AvatarHash: hash}, time.Minute); err != nil {
				t.Fatal(err)
			}
			rows := sqlmock.NewRows([]string{"matched_domain"})
			if tc.domain != "" {
				rows.AddRow(tc.domain)
			}
			mock.ExpectQuery("SELECT matched_domain FROM profile_avatar_samples").WithArgs(tc.chat, hash).WillReturnRows(rows)
			s := &Service{State: cache, Store: &store.Store{DB: db}}
			v, err := s.inspectProfile(context.Background(), tc.chat, 42, domain.Settings{ProfileBlockedDomains: []string{"heiying.co"}, ProfileAvatarMatch: true}, false, true)
			if err != nil || v.Matched != tc.want || v.Learned {
				t.Fatal(v, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProfilePreviewNeverLearns(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := miniredis.RunT(t)
	cache := state.New(r.Addr(), "")
	defer cache.R.Close()
	if err := cache.Put(context.Background(), "profile:v1:42:true", profileData{Bio: "黑鹰 heiying.co", BioAvailable: true, AvatarAvailable: true, AvatarHash: strings.Repeat("a", 64)}, time.Minute); err != nil {
		t.Fatal(err)
	}
	s := &Service{State: cache, Store: &store.Store{DB: db}}
	v, err := s.inspectProfile(context.Background(), -100, 42, domain.Settings{ProfileBlockedDomains: []string{"heiying.co"}, ProfileAvatarMatch: true}, false, false)
	if err != nil || !v.Matched || v.Learned {
		t.Fatal(v, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProfileBanRechecksProtectionBeforeListWrite(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := miniredis.RunT(t)
	cache := state.New(r.Addr(), "")
	defer cache.R.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/getChatMember" {
			t.Errorf("unexpected mutation %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"status": "administrator", "user": map[string]any{"id": 42}}})
	}))
	defer srv.Close()
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
	mock.ExpectExec("INSERT IGNORE INTO punishments").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT IGNORE INTO punishment_workflows").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT decision,status,deleted,acted,created_at").WillReturnRows(sqlmock.NewRows([]string{"decision", "status", "deleted", "acted", "created_at", "started"}).AddRow(`{"action":"ban","reason":"profile_domain:heiying.co"}`, "pending", false, false, time.Now(), false))
	mock.ExpectExec("UPDATE punishments SET status='skipped'").WithArgs("profile-test").WillReturnResult(sqlmock.NewResult(0, 1))
	s := &Service{State: cache, Store: &store.Store{DB: db}, Bot: &telegram.Client{BaseURL: srv.URL, HTTP: srv.Client()}}
	err = s.Punish(context.Background(), store.Log{EventKey: "profile-test", ChatID: -100, UserID: 42, Source: "profile_guard", Decision: domain.Decision{Action: "ban", Reason: "profile_domain:heiying.co"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
