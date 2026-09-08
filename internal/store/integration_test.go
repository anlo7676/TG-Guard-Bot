package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"tgguard/internal/domain"
)

func TestMySQLIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not configured (must use a disposable database ending in _test)")
	}
	cfg, e := mysql.ParseDSN(dsn)
	if e != nil || !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("integration database name must end in _test")
	}
	ctx := context.Background()
	s, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal("migration not idempotent", e)
	}
	for _, table := range []string{"web_credentials", "verification_answers", "welcome_cleanup", "bot_groups", "users", "group_members", "group_settings", "list_entries", "keyword_rules", "verification_sessions", "moderation_logs", "punishments", "ai_usage_logs", "admin_audits", "feedback", "update_inbox", "bot_state", "system_settings"} {
		if _, e = s.DB.ExecContext(ctx, "TRUNCATE TABLE "+table); e != nil {
			t.Fatal(e)
		}
	}
	t.Run("unbound legacy runtime cannot silently change bots", func(t *testing.T) {
		if e := s.SaveOffset(ctx, 100); e != nil {
			t.Fatal(e)
		}
		if e := s.BindBot(ctx, 11); e == nil {
			t.Fatal("legacy offset accepted")
		}
		if _, e := s.DB.ExecContext(ctx, "DELETE FROM bot_state WHERE name='poll_offset'"); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("persistent bot identity and log evidence survive reload", func(t *testing.T) {
		if e := s.BindBot(ctx, 11); e != nil {
			t.Fatal(e)
		}
		if e := s.BindBot(ctx, 11); e != nil {
			t.Fatal(e)
		}
		if e := s.BindBot(ctx, 22); e == nil {
			t.Fatal("different bot reused database")
		}
		for _, tc := range []struct {
			key      string
			risk     domain.Risk
			decision domain.Decision
		}{
			{"evidence-spam", domain.Risk{Spam: true, Score: 100, Matches: []domain.Match{{Rule: "spam"}}}, domain.Decision{Action: "warn", Reason: "spam"}},
			{"evidence-direct", domain.Risk{LocalAction: "delete", Score: 100, Matches: []domain.Match{{Rule: "ad_tasks"}}}, domain.Decision{Action: "delete", Reason: "local_ad_rule"}},
		} {
			l, e := s.SaveLog(ctx, Log{EventKey: tc.key, ChatID: -9000, UserID: 99, Risk: tc.risk, Decision: tc.decision, Source: "automatic"})
			if e != nil {
				t.Fatal(e)
			}
			if l.Risk.Spam != tc.risk.Spam || l.Risk.LocalAction != tc.risk.LocalAction {
				t.Fatal("evidence lost", l)
			}
		}
	})
	t.Run("settings and lists isolated", func(t *testing.T) {
		settings := domain.DefaultSettings()
		settings.AIEnabled = true
		if e := s.SaveSettings(ctx, -100, 42, settings); e != nil {
			t.Fatal(e)
		}
		other, e := s.Settings(ctx, -200)
		if e != nil || other.AIEnabled {
			t.Fatal(other, e)
		}
		if e = s.SaveList(ctx, domain.ListEntry{ChatID: -100, UserID: 42, Kind: "white"}, 42, false); e != nil {
			t.Fatal(e)
		}
		if kind, e := s.ListStatus(ctx, -200, 42, ""); e != nil || kind != "" {
			t.Fatal("list leaked", kind, e)
		}
		if e = s.SaveList(ctx, domain.ListEntry{ChatID: 0, UserID: 42, Kind: "black"}, 42, false); e != nil {
			t.Fatal(e)
		}
		if kind, e := s.ListStatus(ctx, -100, 42, ""); e != nil || kind != "black" {
			t.Fatal(kind, e)
		}
	})
	t.Run("private menu settings isolate and preserve concurrent edits", func(t *testing.T) {
		if e := s.RegisterGroup(ctx, domain.Chat{ID: -991, Title: "Menu group", Type: "supergroup"}); e != nil {
			t.Fatal(e)
		}
		if e := s.AuthorizeGroup(ctx, -991, 1, "approved", "test fixture"); e != nil {
			t.Fatal(e)
		}
		var wg sync.WaitGroup
		for _, change := range []func(*domain.Settings) error{
			func(v *domain.Settings) error { v.AIEnabled = true; return nil },
			func(v *domain.Settings) error { v.RateLimit = 19; return nil },
		} {
			wg.Add(1)
			go func(f func(*domain.Settings) error) {
				defer wg.Done()
				if e := s.ChangeSettings(ctx, -991, 42, f); e != nil {
					t.Error(e)
				}
			}(change)
		}
		wg.Wait()
		v, e := s.Settings(ctx, -991)
		if e != nil || !v.AIEnabled || v.RateLimit != 19 {
			t.Fatal("lost change", v, e)
		}
		other, e := s.Settings(ctx, -992)
		if e != nil || other.AIEnabled || other.RateLimit == 19 {
			t.Fatal("cross group change", e)
		}
		var audits int
		if e = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_audits WHERE chat_id=-991 AND actor_id=42").Scan(&audits); e != nil || audits != 2 {
			t.Fatal("missing actor audit", audits, e)
		}
		if e = s.DeactivateGroup(ctx, -991); e != nil {
			t.Fatal(e)
		}
		if e = s.ChangeSettings(ctx, -991, 42, func(v *domain.Settings) error { v.AIEnabled = false; return nil }); !errors.Is(e, sql.ErrNoRows) {
			t.Fatal("inactive group accepted", e)
		}
	})
	t.Run("keywords CRUD", func(t *testing.T) {
		k := domain.Keyword{ChatID: -100, Keyword: "官网", MatchType: "contains", ReplyType: "text", Content: "https://example.com", Enabled: true}
		id, e := s.SaveKeyword(ctx, k, 42)
		if e != nil {
			t.Fatal(e)
		}
		k.ID = id
		k.Priority = 9
		if _, e = s.SaveKeyword(ctx, k, 42); e != nil {
			t.Fatal(e)
		}
		ks, e := s.Keywords(ctx, -100)
		if e != nil || len(ks) != 1 || ks[0].Priority != 9 {
			t.Fatal(ks, e)
		}
		if e = s.DeleteKeyword(ctx, -200, id, 42); !errors.Is(e, sql.ErrNoRows) {
			t.Fatal("cross group delete", e)
		}
		if e = s.DeleteKeyword(ctx, -100, id, 42); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("durable queue order and deduplication", func(t *testing.T) {
		for _, u := range []domain.Update{{ID: 1, Message: &domain.Message{Chat: domain.Chat{ID: -100}}}, {ID: 2, Message: &domain.Message{Chat: domain.Chat{ID: -100}}}, {ID: 3, Message: &domain.Message{Chat: domain.Chat{ID: -200}}}} {
			if e := s.Enqueue(ctx, u); e != nil {
				t.Fatal(e)
			}
			if e := s.Enqueue(ctx, u); e != nil {
				t.Fatal(e)
			}
		}
		a, e := s.Claim(ctx)
		if e != nil || a.Update.ID != 1 {
			t.Fatal(a, e)
		}
		b, e := s.Claim(ctx)
		if e != nil || b.Update.ID != 3 {
			t.Fatal("same chat order violated", b, e)
		}
		if _, e = s.Claim(ctx); !errors.Is(e, sql.ErrNoRows) {
			t.Fatal(e)
		}
		if e = s.Finish(ctx, a, nil); e != nil {
			t.Fatal(e)
		}
		c, e := s.Claim(ctx)
		if e != nil || c.Update.ID != 2 {
			t.Fatal(c, e)
		}
		if e = s.Finish(ctx, b, nil); e != nil {
			t.Fatal(e)
		}
		if e = s.Finish(ctx, c, errors.New("temporary")); e != nil {
			t.Fatal(e)
		}
		if _, e = s.DB.ExecContext(ctx, "UPDATE update_inbox SET available_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND) WHERE update_id=2"); e != nil {
			t.Fatal(e)
		}
		c, e = s.Claim(ctx)
		if e != nil || c.Attempts != 2 {
			t.Fatal(c, e)
		}
		if e = s.Finish(ctx, c, nil); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("one verification winner", func(t *testing.T) {
		v := Verification{Token: "integration-token", ChatID: -100, UserID: 1001, Type: "math", Question: "1+2", AnswerHash: HashAnswer("integration-token", "3"), FailAction: "kick", ExpiresAt: time.Now().Add(time.Minute)}
		if e := s.CreateVerification(ctx, v); e != nil {
			t.Fatal(e)
		}
		var wg sync.WaitGroup
		results := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _, e := s.Answer(ctx, v.Token, v.UserID, "3"); results <- e }()
		}
		wg.Wait()
		close(results)
		wins := 0
		for e := range results {
			if e == nil {
				wins++
			} else if !errors.Is(e, ErrVerification) {
				t.Fatal(e)
			}
		}
		if wins != 1 {
			t.Fatalf("winners=%d", wins)
		}
		vs, e := s.DueVerifications(ctx)
		if e != nil || len(vs) != 1 || vs[0].Status != "completing" {
			t.Fatal(vs, e)
		}
	})
	t.Run("third wrong answer expires", func(t *testing.T) {
		v := Verification{Token: "wrong-token", ChatID: -100, UserID: 1002, Type: "math", Question: "1+2", AnswerHash: HashAnswer("wrong-token", "3"), FailAction: "kick", ExpiresAt: time.Now().Add(time.Minute)}
		if e := s.CreateVerification(ctx, v); e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 3; i++ {
			if _, e := s.Answer(ctx, v.Token, v.UserID, "wrong"); !errors.Is(e, ErrVerification) {
				t.Fatal(e)
			}
		}
		got, e := s.Verification(ctx, v.Token)
		if e != nil || got.Attempts != 3 || got.ExpiresAt.After(time.Now()) {
			t.Fatal(got, e)
		}
	})
	t.Run("moderation retries do not inflate counts", func(t *testing.T) {
		l := Log{EventKey: "integration", ChatID: -100, UserID: 1003, MessageID: 10, Text: "normal", Risk: domain.Risk{Matches: []domain.Match{}}, Decision: domain.Decision{Action: "allow"}, Source: "automatic"}
		a, e := s.SaveLog(ctx, l)
		if e != nil {
			t.Fatal(e)
		}
		b, e := s.SaveLog(ctx, l)
		if e != nil || a.ID != b.ID {
			t.Fatal(a, b, e)
		}
		var count int
		if e = s.DB.QueryRowContext(ctx, "SELECT message_count FROM group_members WHERE chat_id=-100 AND user_id=1003").Scan(&count); e != nil || count != 1 {
			t.Fatal(count, e)
		}
	})
}
