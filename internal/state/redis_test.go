package state

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func testState(t *testing.T) *State {
	t.Helper()
	r := miniredis.RunT(t)
	s := New(r.Addr(), "")
	t.Cleanup(func() { s.R.Close() })
	return s
}
func TestSpamAtomicIdempotencyAndScope(t *testing.T) {
	s := testState(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rate, dup, e := s.Spam(ctx, -1, 42, 1, "same text", 10)
			if e != nil || rate != 1 || dup != 1 {
				t.Errorf("retry counted: %d %d %v", rate, dup, e)
			}
		}()
	}
	wg.Wait()
	rate, dup, e := s.Spam(ctx, -1, 42, 2, "same text", 10)
	if e != nil || rate != 2 || dup != 2 {
		t.Fatalf("new event: %d %d %v", rate, dup, e)
	}
	rate, dup, e = s.Spam(ctx, -2, 42, 3, "same text", 10)
	if e != nil || rate != 1 || dup != 1 {
		t.Fatal("chat isolation failed")
	}
}
func TestExpiredLockOwnerCannotDeleteReplacement(t *testing.T) {
	r := miniredis.RunT(t)
	s := New(r.Addr(), "")
	defer s.R.Close()
	ctx := context.Background()
	unlock, e := s.Lock(ctx, "testlock", time.Second)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Lock(ctx, "testlock", time.Second); e == nil {
		t.Fatal("double acquired")
	}
	r.FastForward(2 * time.Second)
	newUnlock, e := s.Lock(ctx, "testlock", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	unlock()
	if !r.Exists("testlock") {
		t.Fatal("old owner removed new lease")
	}
	newUnlock()
	if r.Exists("testlock") {
		t.Fatal("lock not released")
	}
}
func TestRateLimitExpires(t *testing.T) {
	r := miniredis.RunT(t)
	s := New(r.Addr(), "")
	defer s.R.Close()
	for i := 0; i < 4; i++ {
		ok, e := s.Limit(context.Background(), "rate", 3, time.Minute)
		if e != nil || ok != (i < 3) {
			t.Fatalf("i=%d ok=%t e=%v", i, ok, e)
		}
	}
	r.FastForward(time.Minute)
	if ok, e := s.Limit(context.Background(), "rate", 3, time.Minute); !ok || e != nil {
		t.Fatal(ok, e)
	}
}
func TestTokenEntropyAndDeepLinkLength(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		v, e := Token()
		if e != nil || len(v) != 32 || len("verify_"+v) > 64 || seen[v] {
			t.Fatal("invalid token")
		}
		seen[v] = true
	}
}
func TestRealRedis(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR not configured")
	}
	s := New(addr, os.Getenv("TEST_REDIS_PASSWORD"))
	defer s.R.Close()
	ctx := context.Background()
	if e := s.R.Ping(ctx).Err(); e != nil {
		t.Fatal(e)
	}
	token, e := Token()
	if e != nil {
		t.Fatal(e)
	}
	key := "tgguard:test:" + token
	defer s.R.Del(ctx, key)
	if ok, e := s.Limit(ctx, key, 1, time.Minute); !ok || e != nil {
		t.Fatal(ok, e)
	}
	if ok, e := s.Limit(ctx, key, 1, time.Minute); ok || e != nil {
		t.Fatal(ok, e)
	}
}
