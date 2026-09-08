package service

import (
	"context"
	"database/sql"
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
	if err := s.sweepPermissionReleases(ctx); err != nil {
		return err
	}
	items, err := s.Store.PendingTakeovers(ctx)
	if err != nil {
		return err
	}
	recoverGroups(ctx, items, func(t store.Takeover) int64 { return t.Log.ChatID }, func(ctx context.Context, t store.Takeover) {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		if log, e := s.Store.GetLog(c, t.Log.EventKey); e == nil {
			t.Log = log
		} else if e != sql.ErrNoRows {
			slog.Warn("punishment evidence reload failed", "event_key", t.Log.EventKey, "error", e)
			return
		}
		if err := s.Punish(c, t.Log, t.Actor); err != nil {
			slog.Warn("punishment takeover recovery failed", "event_key", t.Log.EventKey, "chat_id", t.Log.ChatID, "error", err)
		}
	})
	return nil
}

func (s *Service) sweepPermissionReleases(ctx context.Context) error {
	items, err := s.Store.DuePermissionReleases(ctx)
	if err != nil {
		return err
	}
	recoverGroups(ctx, items, func(r store.PermissionRelease) int64 { return r.ChatID }, func(ctx context.Context, r store.PermissionRelease) {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		unlock, e := s.State.Lock(c, verifyLock(r.ChatID, r.UserID), 60*time.Second)
		if e != nil {
			return
		}
		defer unlock()
		resolved, e := s.Store.NewerPermissionAction(c, r.ChatID, r.UserID, r.CreatedAt)
		if e == nil && !resolved {
			if r.Action == "mute" {
				e = s.executor().Restore(c, r.ChatID, r.UserID)
			} else {
				e = s.executor().Unban(c, r.ChatID, r.UserID)
			}
		}
		audit, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer stop()
		if recordErr := s.Store.FinishPermissionRelease(audit, r.EventKey, e, !resolved && e == nil); recordErr != nil {
			slog.Error("permission release persistence failed", "event_key", r.EventKey, "error", recordErr)
		}
		if e != nil {
			slog.Warn("permission release failed", "event_key", r.EventKey, "chat_id", r.ChatID, "user_id", r.UserID, "error", e)
		}
	})
	return nil
}
