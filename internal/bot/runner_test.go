package bot

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"tgguard/internal/domain"
	"tgguard/internal/service"
	"tgguard/internal/store"
	"time"
)

type testInbox struct {
	mu       sync.Mutex
	offset   int64
	updates  []domain.Update
	jobs     []store.Job
	finished chan error
	saveErr  error
}

func (q *testInbox) Offset(context.Context) (int64, error)       { return q.offset, nil }
func (q *testInbox) SaveOffset(_ context.Context, n int64) error { q.offset = n; return q.saveErr }
func (q *testInbox) Enqueue(_ context.Context, u domain.Update) error {
	q.updates = append(q.updates, u)
	return nil
}
func (q *testInbox) Claim(context.Context) (store.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.jobs) == 0 {
		return store.Job{}, sql.ErrNoRows
	}
	j := q.jobs[0]
	q.jobs = q.jobs[1:]
	return j, nil
}
func (q *testInbox) Finish(ctx context.Context, _ store.Job, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	q.finished <- err
	return nil
}

type callFunc func(context.Context, string, any, any) error

func (f callFunc) Call(c context.Context, m string, in, out any) error { return f(c, m, in, out) }

func TestPollingDurabilityAndFailure(t *testing.T) {
	q := &testInbox{saveErr: errors.New("offset commit failure")}
	health := service.NewIngestionHealth("polling")
	h := &Handler{Queue: q, Health: health, Telegram: callFunc(func(_ context.Context, m string, in, out any) error {
		if m != "getUpdates" {
			t.Fatal(m)
		}
		*out.(*[]domain.Update) = []domain.Update{{ID: 15}, {ID: 16}}
		return nil
	})}
	if err := h.Poll(context.Background()); !errors.Is(err, q.saveErr) {
		t.Fatal(err)
	}
	if len(q.updates) != 2 || q.offset != 17 {
		t.Fatal("updates not persisted before offset")
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.Telegram = callFunc(func(ctx context.Context, _ string, _, _ any) error { cancel(); return ctx.Err() })
	if err := h.Poll(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestWorkersStopAndFinishCancelledJobs(t *testing.T) {
	q := &testInbox{jobs: []store.Job{{Update: domain.Update{ID: 1}, Attempts: 1}}, finished: make(chan error, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	h := &Handler{Queue: q, Process: func(c context.Context, _ domain.Update) error { close(started); <-c.Done(); return c.Err() }}
	done := make(chan struct{})
	go func() { h.Workers(ctx, 2); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker not started")
	}
	cancel()
	select {
	case err := <-q.finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled job not completed")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("workers leaked")
	}
}
func TestPanicBecomesRetryableError(t *testing.T) {
	h := &Handler{Process: func(context.Context, domain.Update) error { panic("sensitive payload") }}
	if err := h.safeHandle(context.Background(), domain.Update{ID: 77}); err == nil || err.Error() != "update handler panic: string" {
		t.Fatal(err)
	}
}
