package service

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"tgguard/internal/store"
)

func TestBackupEncryptionRejectsTamperingAndWrongKey(t *testing.T) {
	key := strings.Repeat("test-key", 4)
	b := store.DatabaseBackup{Version: 1, BotID: 42, CreatedAt: time.Now().UTC()}
	data, err := sealBackup(b, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := openBackup(data, key)
	if err != nil || got.BotID != 42 || !got.CreatedAt.Equal(b.CreatedAt) {
		t.Fatal(got, err)
	}
	other, _ := sealBackup(b, key)
	if bytes.Equal(data, other) {
		t.Fatal("nonce reused")
	}
	if _, err := openBackup(data, strings.Repeat("different", 4)); err == nil {
		t.Fatal("wrong key accepted")
	}
	data[len(data)-1] ^= 1
	if _, err := openBackup(data, key); err == nil {
		t.Fatal("tampering accepted")
	}
	for _, raw := range [][]byte{nil, []byte(backupHeader), make([]byte, store.MaxBackupBytes+129)} {
		if _, err := openBackup(raw, key); err == nil {
			t.Fatal("invalid size accepted")
		}
	}
}

func TestMaintenanceDrainsAndRejectsNewOperations(t *testing.T) {
	m := &Maintenance{}
	leave, ok := m.Enter()
	if !ok {
		t.Fatal("initial admission failed")
	}
	done := make(chan error, 1)
	go func() { done <- m.Pause(context.Background()) }()
	deadline := time.Now().Add(time.Second)
	for {
		m.mu.Lock()
		paused := m.paused
		m.mu.Unlock()
		if paused {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pause did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if _, ok := m.Enter(); ok {
		t.Fatal("admitted while draining")
	}
	select {
	case <-done:
		t.Fatal("did not wait for active work")
	default:
	}
	leave()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := m.Pause(context.Background()); err == nil {
		t.Fatal("concurrent restore admitted")
	}
	m.Resume()
	leave, ok = m.Enter()
	if !ok {
		t.Fatal("resume failed")
	}
	leave()
}

func TestMaintenanceCancellationResumes(t *testing.T) {
	m := &Maintenance{}
	leave, _ := m.Enter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Pause(ctx); err == nil {
		t.Fatal("expected cancellation")
	}
	leave()
	leave, ok := m.Enter()
	if !ok {
		t.Fatal("stuck paused")
	}
	leave()
}
