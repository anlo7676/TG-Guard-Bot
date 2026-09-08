package settings

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStaleConfigurationRejected(t *testing.T) {
	m, err := New(context.Background(), &memoryRepo{}, strings.Repeat("m", 32), defaults())
	if err != nil {
		t.Fatal(err)
	}
	a, b := m.Snapshot(), m.Snapshot()
	a.SuperAdmins = []int64{42}
	b.PanelURL = "https://example.com"
	if err = m.Save(context.Background(), a, false); err != nil {
		t.Fatal(err)
	}
	if err = m.Save(context.Background(), b, false); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if !m.IsAdmin(42) || m.Snapshot().PanelURL != "" {
		t.Fatal("stale editor overwrote state")
	}
}
