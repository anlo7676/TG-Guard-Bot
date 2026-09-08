package service

import (
	"testing"
	"time"
)

func TestIngestionHealth(t *testing.T) {
	h := NewIngestionHealth("polling")
	h.Record(true)
	if _, ok := h.Snapshot(); !ok {
		t.Fatal("empty successful poll must be healthy")
	}
	for i := 0; i < 3; i++ {
		h.Record(false)
	}
	if _, ok := h.Snapshot(); ok {
		t.Fatal("persistent failure reported healthy")
	}
	h.Record(true)
	if _, ok := h.Snapshot(); !ok {
		t.Fatal("recovery not reflected")
	}
	h.success = time.Now().Add(-2 * time.Minute)
	if _, ok := h.Snapshot(); ok {
		t.Fatal("stalled ingestion reported healthy")
	}
}
