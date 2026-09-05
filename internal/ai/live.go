package ai

import (
	"context"
	"errors"
	"net/http"
	"tgguard/internal/domain"
	"tgguard/internal/settings"
	"tgguard/internal/state"
	"tgguard/internal/store"
	"time"
)

type Live struct {
	Settings *settings.Manager
	State    *state.State
	Store    *store.Store
	Slots    chan struct{}
}

func (l *Live) Review(ctx context.Context, n domain.Normalized, r domain.Risk) (domain.AIResult, error) {
	return l.review(ctx, n, r, false)
}

// Test always calls the saved provider, so stale cache cannot hide bad credentials.
func (l *Live) Test(ctx context.Context, n domain.Normalized, r domain.Risk) (domain.AIResult, error) {
	return l.review(ctx, n, r, true)
}

func (l *Live) review(ctx context.Context, n domain.Normalized, r domain.Risk, bypassCache bool) (domain.AIResult, error) {
	a := l.Settings.Snapshot().AI
	if !a.Enabled {
		return domain.AIResult{}, errors.New("AI provider disabled")
	}
	p := &Compatible{BaseURL: a.BaseURL, Key: a.APIKey, Model: a.Model, Timeout: time.Duration(a.TimeoutSeconds) * time.Second, MaxTokens: a.MaxTokens, TokenParameter: a.TokenParameter, State: l.State, Store: l.Store, Slots: l.Slots, HTTP: &http.Client{Timeout: time.Duration(a.TimeoutSeconds) * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}}
	p.BypassCache = bypassCache
	return p.Review(ctx, n, r)
}
