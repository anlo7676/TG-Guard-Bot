package config

import (
	"strings"
	"testing"
)

func base(t *testing.T) {
	t.Helper()
	for _, k := range []string{"REDIS_DB", "BOT_MODE", "BOT_SUPER_ADMINS", "WEBHOOK_URL", "WEBHOOK_SECRET", "WORKERS", "AI_CONCURRENCY", "AI_TIMEOUT", "AI_API_KEY", "AI_MODEL", "AI_BASE_URL"} {
		t.Setenv(k, "")
	}
	t.Setenv("BOT_TOKEN", "fake-token")
	t.Setenv("MYSQL_DSN", "x:y@tcp(localhost:3306)/test")
	t.Setenv("ADMIN_API_TOKEN", strings.Repeat("x", 32))
}
func TestConfiguration(t *testing.T) {
	base(t)
	c, e := Load()
	if e != nil || c.Workers != 8 || c.Mode != "polling" {
		t.Fatalf("%+v %v", c, e)
	}
}

func TestAIHTTPRequiresOptIn(t *testing.T) {
	base(t)
	t.Setenv("AI_ALLOW_INSECURE_HTTP", "false")
	t.Setenv("AI_API_KEY", "test")
	t.Setenv("AI_MODEL", "local")
	for _, address := range []string{"http://127.0.0.1:1234/v1", "HTTP://127.0.0.1:1234/v1"} {
		t.Setenv("AI_BASE_URL", address)
		if _, err := Load(); err == nil {
			t.Fatal("accepted HTTP without consent", address)
		}
	}
	t.Setenv("AI_ALLOW_INSECURE_HTTP", "true")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}
func TestInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct{ k, v string }{{"REDIS_DB", "-1"}, {"REDIS_DB", "16"}, {"ADMIN_API_TOKEN", "weak"}, {"WORKERS", "0"}, {"WORKERS", "bad"}, {"BOT_MODE", "unknown"}, {"AI_TIMEOUT", "2m"}, {"BOT_SUPER_ADMINS", "abc"}, {"AI_API_KEY", "key"}} {
		t.Run(tc.k+tc.v, func(t *testing.T) {
			base(t)
			t.Setenv(tc.k, tc.v)
			if _, e := Load(); e == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}
func TestWebhookRequiresHTTPSAndSecret(t *testing.T) {
	base(t)
	t.Setenv("BOT_MODE", "webhook")
	t.Setenv("WEBHOOK_URL", "http://example.com/telegram/webhook")
	t.Setenv("WEBHOOK_SECRET", strings.Repeat("a", 32))
	if _, e := Load(); e == nil {
		t.Fatal("accepted HTTP")
	}
	t.Setenv("WEBHOOK_URL", "https://example.com/telegram/webhook")
	if _, e := Load(); e != nil {
		t.Fatal(e)
	}
}
