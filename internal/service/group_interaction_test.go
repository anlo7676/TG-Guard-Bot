package service

import (
	"context"
	"testing"
	"tgguard/internal/domain"
	"time"
)

func TestMenuSessionBindsUserAndExpires(t *testing.T) {
	s := &Service{State: menuTestState(t)}
	ctx := context.Background()
	rows, e := s.secureMenu(ctx, 42, [][]menuButton{{button("设置", "gm:-1001:settings")}})
	if e != nil {
		t.Fatal(e)
	}
	data := rows[0][0]["callback_data"]
	if len(data) > 64 {
		t.Fatal("Telegram callback too large")
	}
	for _, id := range []int64{42, 43} {
		value, e := s.resolveMenu(ctx, domain.Callback{Data: data, From: domain.User{ID: id}})
		if e != nil {
			t.Fatal(e)
		}
		if (value != "") != (id == 42) {
			t.Fatal("user binding failed")
		}
	}
	// Expire the precise session instead of touching unrelated application keys.
	keys, _ := s.State.R.Keys(ctx, "menu:session:*").Result()
	for _, k := range keys {
		s.State.R.Expire(ctx, k, time.Nanosecond)
	}
	s.State.R.Del(ctx, keys...)
	value, e := s.resolveMenu(ctx, domain.Callback{Data: data, From: domain.User{ID: 42}})
	if e != nil || value != "" {
		t.Fatal("expired menu accepted")
	}
}
