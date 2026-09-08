package service

import (
	"context"
	"log/slog"
	"sync"
	"tgguard/internal/store"
	"time"
)

// Recover unrelated groups concurrently, with one task per group at a time.
// A round is bounded so slow groups cannot indefinitely hold up new work.
func recoverGroups[T any](ctx context.Context, items []T, group func(T) int64, run func(context.Context, T)) {
	round, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	batches := map[int64][]T{}
	order := []int64{}
	for _, item := range items {
		id := group(item)
		if _, ok := batches[id]; !ok {
			order = append(order, id)
		}
		batches[id] = append(batches[id], item)
	}
	jobs := make(chan []T)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range jobs {
				for _, item := range batch {
					if round.Err() != nil {
						break
					}
					run(round, item)
				}
			}
		}()
	}
	for _, id := range order {
		select {
		case jobs <- batches[id]:
		case <-round.Done():
			close(jobs)
			wg.Wait()
			return
		}
	}
	close(jobs)
	wg.Wait()
}

func (s *Service) SweepTakeovers(ctx context.Context) error {
	items, err := s.Store.PendingTakeovers(ctx)
	if err != nil {
		return err
	}
	recoverGroups(ctx, items, func(t store.Takeover) int64 { return t.Log.ChatID }, func(ctx context.Context, t store.Takeover) {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		if err := s.Punish(c, t.Log, t.Actor); err != nil {
			slog.Warn("punishment takeover recovery failed", "event_key", t.Log.EventKey, "chat_id", t.Log.ChatID, "error", err)
		}
	})
	return nil
}
