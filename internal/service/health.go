package service

import (
	"sync"
	"time"
)

type IngestionHealth struct {
	mu       sync.Mutex
	mode     string
	started  time.Time
	success  time.Time
	failures int
}

func NewIngestionHealth(mode string) *IngestionHealth {
	return &IngestionHealth{mode: mode, started: time.Now()}
}
func (h *IngestionHealth) Record(ok bool) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if ok {
		h.success = time.Now()
		h.failures = 0
	} else {
		h.failures++
	}
}
func (h *IngestionHealth) Snapshot() (map[string]any, bool) {
	if h == nil {
		return map[string]any{"status": "unmonitored"}, true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	ready := h.failures < 3 && ((!h.success.IsZero() && time.Since(h.success) < 90*time.Second) || (h.success.IsZero() && time.Since(h.started) < 45*time.Second))
	status := "healthy"
	if h.success.IsZero() {
		status = "starting"
	}
	if !ready {
		status = "unavailable"
	}
	return map[string]any{"status": status, "mode": h.mode, "last_success": h.success, "consecutive_failures": h.failures}, ready
}
