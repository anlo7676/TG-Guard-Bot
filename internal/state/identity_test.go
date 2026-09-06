package state

import (
	"context"
	"github.com/alicebob/miniredis/v2"
	"testing"
	"time"
)

func TestBotIdentityAndRedisDatabaseIsolation(t *testing.T) {
	r := miniredis.RunT(t)
	a := NewDB(r.Addr(), "", 0)
	defer a.R.Close()
	b := NewDB(r.Addr(), "", 1)
	defer b.R.Close()
	ctx := context.Background()
	legacy := NewDB(r.Addr(), "", 2)
	defer legacy.R.Close()
	if e := legacy.Put(ctx, "old-token", "old", time.Minute); e != nil {
		t.Fatal(e)
	}
	if e := legacy.BindBot(ctx, 33); e == nil {
		t.Fatal("legacy cache silently claimed")
	}
	if e := a.BindBot(ctx, 11); e != nil {
		t.Fatal(e)
	}
	if e := a.BindBot(ctx, 11); e != nil {
		t.Fatal(e)
	}
	if e := a.BindBot(ctx, 22); e == nil {
		t.Fatal("different bot accepted")
	}
	if e := b.BindBot(ctx, 22); e != nil {
		t.Fatal(e)
	}
	if e := a.Put(ctx, "verify:prompt", "old", time.Minute); e != nil {
		t.Fatal(e)
	}
	if n, e := b.R.Exists(ctx, "verify:prompt").Result(); e != nil || n != 0 {
		t.Fatal("old prompt leaked", e)
	}
}
