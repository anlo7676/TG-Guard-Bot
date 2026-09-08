package service

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRecoveryIsolatesGroupsAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slow := make(chan struct{})
	fast := make(chan struct{})
	done := make(chan struct{})
	var mu sync.Mutex
	active := map[int64]int{}
	go func() {
		recoverGroups(ctx, []int64{1, 1, 2}, func(id int64) int64 { return id }, func(c context.Context, id int64) {
			mu.Lock()
			active[id]++
			if active[id] > 1 {
				t.Error("same group concurrent")
			}
			mu.Unlock()
			defer func() { mu.Lock(); active[id]--; mu.Unlock() }()
			if id == 1 {
				select {
				case <-slow:
				default:
					close(slow)
				}
				<-c.Done()
			} else {
				close(fast)
			}
		})
		close(done)
	}()
	select {
	case <-fast:
	case <-time.After(time.Second):
		t.Fatal("healthy group blocked by slow group")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recovery goroutine leaked")
	}
}
