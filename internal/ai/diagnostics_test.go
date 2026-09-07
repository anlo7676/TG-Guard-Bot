package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"tgguard/internal/domain"
	"tgguard/internal/state"
)

func TestRedisOutageStopsBeforeAIAndRecovers(t *testing.T) {
	m := miniredis.RunT(t)
	s := state.New(m.Addr(), "")
	defer s.R.Close()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": valid}}}})
	}))
	defer srv.Close()
	p := &Compatible{BaseURL: srv.URL, Key: "secret", Model: "model", Timeout: time.Second, HTTP: srv.Client(), State: s, Slots: make(chan struct{}, 1)}
	m.SetError("temporary outage")
	_, err := p.Review(context.Background(), domain.Normalized{}, domain.Risk{})
	if err == nil || !strings.Contains(err.Error(), "stage=cache_read") || !strings.Contains(err.Error(), "Redis AI cache read failed") || calls != 0 {
		t.Fatalf("expected Redis failure before HTTP: calls=%d err=%v", calls, err)
	}
	m.SetError("")
	for i := 0; i < 2; i++ {
		if _, err = p.Review(context.Background(), domain.Normalized{}, domain.Risk{}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("expected recovery followed by cache hit, calls=%d", calls)
	}
	for _, key := range m.Keys() {
		if strings.HasPrefix(key, "ai:moderation:") {
			m.Set(key, "broken JSON")
		}
	}
	if _, err = p.Review(context.Background(), domain.Normalized{}, domain.Risk{}); err != nil || calls != 2 {
		t.Fatalf("corrupt cache must be recomputed: calls=%d err=%v", calls, err)
	}
}

type diagnosticTransport func(*http.Request) (*http.Response, error)

func (f diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAIRequestTimeoutHasStageAndNoSecrets(t *testing.T) {
	p := &Compatible{BaseURL: "https://secret-user:secret-pass@example.invalid/secret-path", Key: "secret-key", Model: "model", Timeout: 20 * time.Millisecond, Slots: make(chan struct{}, 1)}
	p.HTTP = &http.Client{Transport: diagnosticTransport(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	_, err := p.Review(context.Background(), domain.Normalized{}, domain.Risk{})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "stage=http_request") || !strings.Contains(err.Error(), "elapsed_ms=") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unexpected diagnostics: %v", err)
	}
}

func TestTransportErrorClassifiesWithoutLeakingURL(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.Canceled, "canceled"},
		{&net.DNSError{Err: "secret-host", Name: "secret-host"}, "DNS"},
		{&net.OpError{Op: "dial", Err: errors.New("secret-address")}, "connection"},
		{errors.New("secret-details"), "HTTP transport"},
	} {
		err := transportError(&url.Error{Op: "Post", URL: "https://secret-endpoint", Err: tc.err})
		if !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "secret") {
			t.Fatal(err)
		}
	}
}
