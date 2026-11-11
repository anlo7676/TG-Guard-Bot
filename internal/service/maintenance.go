package service

import (
	"context"
	"errors"
	"sync"
)

// Maintenance drains active business operations before database replacement.
// A successful restore remains paused until restart, so runtime caches and the
// polling offset can never be used with a restored database.
type Maintenance struct {
	mu      sync.Mutex
	paused  bool
	active  int
	drained chan struct{}
}

func (m *Maintenance) Enter() (func(), bool) {
	if m == nil {
		return func() {}, true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.paused {
		return nil, false
	}
	m.active++
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.active--
		if m.paused && m.active == 0 {
			close(m.drained)
		}
	}, true
}

func (m *Maintenance) Pause(ctx context.Context) error {
	m.mu.Lock()
	if m.paused {
		m.mu.Unlock()
		return errors.New("数据库恢复正在进行或已完成，请重启服务")
	}
	m.paused = true
	m.drained = make(chan struct{})
	if m.active == 0 {
		close(m.drained)
	}
	done := m.drained
	m.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		m.Resume()
		return ctx.Err()
	}
}

func (m *Maintenance) Resume() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paused = false
}
