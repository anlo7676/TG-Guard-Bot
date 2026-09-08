package bot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"tgguard/internal/domain"
	"tgguard/internal/state"
)

var AllowedUpdates = []string{"message", "edited_message", "callback_query", "chat_member", "my_chat_member"}

func (h *Handler) Poll(ctx context.Context) error {
	offset, e := h.inbox().Offset(ctx)
	if e != nil {
		return e
	}
	for ctx.Err() == nil {
		var updates []domain.Update
		e = h.caller().Call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "limit": 100, "allowed_updates": AllowedUpdates}, &updates)
		h.health().Record(e == nil)
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Error("poll failed", "error", e)
			state.Sleep(ctx, 3*time.Second)
			continue
		}
		for _, u := range updates {
			if e = h.inbox().Enqueue(ctx, u); e != nil {
				return e
			}
			offset = u.ID + 1
		}
		if len(updates) > 0 {
			if e = h.inbox().SaveOffset(ctx, offset); e != nil {
				return e
			}
		}
	}
	return nil
}
func (h *Handler) Workers(ctx context.Context, count int) {
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(worker int) { defer wg.Done(); h.worker(ctx, worker) }(i)
	}
	if h.Service == nil {
		wg.Wait()
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			if e := h.Service.SweepTakeovers(ctx); e != nil && ctx.Err() == nil {
				slog.Error("takeover recovery failed", "error", e)
			}
			if e := h.Service.SweepVerification(ctx); e != nil && ctx.Err() == nil {
				slog.Error("verification sweep failed", "error", e)
			}
			state.Sleep(ctx, 5*time.Second)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			if e := h.Service.SweepWelcomeCleanup(ctx); e != nil && ctx.Err() == nil {
				slog.Error("welcome cleanup failed", "error", e)
			}
			state.Sleep(ctx, time.Second)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			if h.Service.RetentionDays > 0 {
				c, cancel := context.WithTimeout(ctx, 30*time.Second)
				if e := h.Service.Store.RetainData(c, h.Service.RetentionDays); e != nil && ctx.Err() == nil {
					slog.Error("retention failed", "error", e)
				}
				cancel()
			}
			state.Sleep(ctx, time.Hour)
		}
	}()
	if h.Service.Health != nil {
		snapshot, _ := h.Service.Health.Snapshot()
		if snapshot["mode"] == "webhook" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for ctx.Err() == nil {
					c, cancel := context.WithTimeout(ctx, 10*time.Second)
					var info struct {
						URL       string `json:"url"`
						LastError int64  `json:"last_error_date"`
					}
					e := h.caller().Call(c, "getWebhookInfo", map[string]any{}, &info)
					cancel()
					h.health().Record(e == nil && info.URL != "" && (info.LastError == 0 || time.Since(time.Unix(info.LastError, 0)) > 90*time.Second))
					state.Sleep(ctx, 30*time.Second)
				}
			}()
		}
	}
	wg.Wait()
}
func (h *Handler) worker(ctx context.Context, id int) {
	for ctx.Err() == nil {
		j, e := h.inbox().Claim(ctx)
		if errors.Is(e, sql.ErrNoRows) {
			state.Sleep(ctx, 300*time.Millisecond)
			continue
		}
		if e != nil {
			if ctx.Err() == nil {
				slog.Error("queue claim failed", "worker", id, "error", e)
			}
			state.Sleep(ctx, time.Second)
			continue
		}
		c, cancel := context.WithTimeout(ctx, 45*time.Second)
		e = h.safeHandle(c, j.Update)
		cancel()
		done, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		finishErr := h.inbox().Finish(done, j, e)
		stop()
		if finishErr != nil {
			slog.Error("queue completion failed", "update_id", j.Update.ID, "error", finishErr)
		}
		if e != nil {
			slog.Error("update failed", "update_id", j.Update.ID, "attempt", j.Attempts, "dead", j.Attempts >= 5, "error", e)
		}
	}
}
func (h *Handler) safeHandle(ctx context.Context, u domain.Update) (err error) {
	defer func() {
		if v := recover(); v != nil {
			slog.Error("update panic", "update_id", u.ID, "partition_id", u.Partition(), "panic_type", fmt.Sprintf("%T", v), "stack", string(debug.Stack()))
			err = fmt.Errorf("update handler panic: %T", v)
		}
	}()
	if h.Process != nil {
		return h.Process(ctx, u)
	}
	return h.Handle(ctx, u)
}
