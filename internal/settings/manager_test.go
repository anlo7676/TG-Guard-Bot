package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type memoryRepo struct {
	value         string
	before, after any
	fail          bool
}

func (r *memoryRepo) SystemSettings(context.Context) (string, error) {
	if r.value == "" {
		return "", sql.ErrNoRows
	}
	return r.value, nil
}
func (r *memoryRepo) SaveSystemSettings(_ context.Context, v string, b, a any) error {
	if r.fail {
		return errors.New("database down")
	}
	r.value = v
	r.before = b
	r.after = a
	return nil
}
func defaults() Config {
	return Config{SuperAdmins: []int64{1}, AI: AI{BaseURL: "https://example.com/v1", TimeoutSeconds: 12, MaxTokens: 500, TokenParameter: "max_completion_tokens"}}
}
func TestEncryptedSettingsAndRedactedAudit(t *testing.T) {
	ctx := context.Background()
	repo := &memoryRepo{}
	master := strings.Repeat("m", 32)
	m, e := New(ctx, repo, master, defaults())
	if e != nil {
		t.Fatal(e)
	}
	c := m.Snapshot()
	c.SuperAdmins = []int64{42}
	c.AI.Enabled = true
	c.AI.Model = "model"
	c.AI.APIKey = "provider-secret-never-expose"
	if e = m.Save(ctx, c, false); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(repo.value, c.AI.APIKey) {
		t.Fatal("plaintext stored")
	}
	for _, v := range []any{m.Snapshot().Public(), repo.before, repo.after} {
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), c.AI.APIKey) || strings.Contains(string(b), `"api_key"`) {
			t.Fatal("key leaked")
		}
	}
	restored, e := New(ctx, repo, master, defaults())
	if e != nil || restored.Snapshot().AI.APIKey != c.AI.APIKey || !restored.IsAdmin(42) || restored.IsAdmin(1) {
		t.Fatal("settings did not restore", e)
	}
	if _, e = New(ctx, repo, strings.Repeat("x", 32), defaults()); e == nil {
		t.Fatal("wrong master accepted")
	}
}
func TestKeyRetainedAndExplicitlyCleared(t *testing.T) {
	ctx := context.Background()
	repo := &memoryRepo{}
	d := defaults()
	d.AI.APIKey = "saved-key"
	m, e := New(ctx, repo, strings.Repeat("m", 32), d)
	if e != nil {
		t.Fatal(e)
	}
	c := m.Snapshot()
	c.AI.APIKey = ""
	if e = m.Save(ctx, c, false); e != nil || m.Snapshot().AI.APIKey != "saved-key" {
		t.Fatal(e)
	}
	c = m.Snapshot()
	if e = m.Save(ctx, c, true); e != nil || m.Snapshot().AI.APIKey != "" {
		t.Fatal(e)
	}
}
func TestFailedPersistenceDoesNotGrantPermission(t *testing.T) {
	repo := &memoryRepo{fail: true}
	m, e := New(context.Background(), repo, strings.Repeat("m", 32), defaults())
	if e != nil {
		t.Fatal(e)
	}
	c := m.Snapshot()
	c.SuperAdmins = []int64{99}
	if e = m.Save(context.Background(), c, false); e == nil || m.IsAdmin(99) || !m.IsAdmin(1) {
		t.Fatal("failed update became active")
	}
	c = m.Snapshot()
	c.SuperAdmins[0] = 99
	if !m.IsAdmin(1) {
		t.Fatal("snapshot mutated live state")
	}
}
func TestSettingsValidation(t *testing.T) {
	for _, change := range []func(*Config){func(c *Config) { c.SuperAdmins = []int64{1, 1} }, func(c *Config) { c.AI.Enabled = true }, func(c *Config) { c.AI.BaseURL = "file:///secret" }, func(c *Config) { c.AI.BaseURL = "https://user:password@example.com" }, func(c *Config) { c.AI.APIKey = "bad\nheader" }, func(c *Config) { c.AI.TokenParameter = "tool" }, func(c *Config) { c.PanelURL = "https://example.com/?token=secret" }} {
		c := defaults()
		change(&c)
		if c.Validate() == nil {
			t.Fatalf("accepted invalid config")
		}
	}
}

func TestHTTPRequiresExplicitConsent(t *testing.T) {
	c := defaults()
	c.AI.Enabled = true
	c.AI.Model = "local"
	c.AI.APIKey = "test"
	for _, address := range []string{"http://127.0.0.1:1234/v1", "HTTP://127.0.0.1:1234/v1"} {
		c.AI.BaseURL = address
		if c.Validate() == nil {
			t.Fatal("HTTP accepted without consent", address)
		}
	}
	c.AI.AllowInsecureHTTP = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
