package telegram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"tgguard/internal/state"
)

func TestMenuFloodLeavesCapacityForPunishment(t *testing.T) {
	m := miniredis.RunT(t)
	st := state.New(m.Addr(), "")
	defer st.R.Close()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client(), State: st}
	for i := 0; i < 100; i++ {
		// Each request can belong to another user; the budget is shared.
		err := c.Call(MenuContext(context.Background()), "getChatMember", nil, nil)
		if i < 5 && err != nil || i >= 5 && !errors.Is(err, ErrMenuLimited) {
			t.Fatalf("query %d: %v", i, err)
		}
	}
	for i := 0; i < 4; i++ {
		err := c.Call(MenuContext(context.Background()), "answerCallbackQuery", nil, nil)
		if i < 3 && err != nil || i == 3 && !errors.Is(err, ErrMenuLimited) {
			t.Fatalf("response %d: %v", i, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := c.Delete(ctx, -100, 1); err != nil {
		t.Fatal("menu flood blocked punishment", err)
	}
	if calls != 9 {
		t.Fatalf("unexpected outbound count: %d", calls)
	}
	m.FastForward(time.Second)
	if err := c.Call(MenuContext(context.Background()), "getChatMember", nil, nil); err != nil {
		t.Fatal("budget did not recover", err)
	}
}

func TestMenuFloodWaitDoesNotHoldWorker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":60}}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	ctx, cancel := context.WithTimeout(MenuContext(context.Background()), 200*time.Millisecond)
	defer cancel()
	if err := c.Call(ctx, "getChatMember", nil, nil); !errors.Is(err, ErrMenuLimited) {
		t.Fatalf("menu waited/retried instead of stopping: %v", err)
	}
}

func TestMenuDoesNotWaitForSharedOrChatQuota(t *testing.T) {
	for _, chatQuota := range []bool{false, true} {
		t.Run(map[bool]string{false: "global", true: "chat"}[chatQuota], func(t *testing.T) {
			m := miniredis.RunT(t)
			st := state.New(m.Addr(), "")
			defer st.R.Close()
			if chatQuota {
				st.Limit(context.Background(), "telegram:send:42", 1, time.Second)
			} else {
				for i := 0; i < 25; i++ {
					st.Limit(context.Background(), "telegram:rate", 25, time.Second)
				}
			}
			c := &Client{State: st}
			ctx, cancel := context.WithTimeout(MenuContext(context.Background()), 100*time.Millisecond)
			defer cancel()
			if err := c.Call(ctx, "sendMessage", map[string]any{"chat_id": 42}, nil); !errors.Is(err, ErrMenuLimited) {
				t.Fatal("menu blocked waiting for quota", err)
			}
		})
	}
}
