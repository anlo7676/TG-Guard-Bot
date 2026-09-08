package upgrade

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestJobOmitsUnknownTime(t *testing.T) {
	b, err := json.Marshal(Job{Phase: "idle"})
	if err != nil || strings.Contains(string(b), "updated_at") {
		t.Fatalf("%s: %v", b, err)
	}
	b, err = json.Marshal(Job{Phase: "running", Updated: time.Now()})
	if err != nil || !strings.Contains(string(b), "updated_at") {
		t.Fatalf("%s: %v", b, err)
	}
}
