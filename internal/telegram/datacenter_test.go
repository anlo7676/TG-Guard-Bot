package telegram

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func photoFixture(dc uint32) string {
	raw := make([]byte, 50)
	binary.LittleEndian.PutUint32(raw, 2|1<<25)
	binary.LittleEndian.PutUint32(raw[4:], dc)
	raw[48] = 30
	raw[49] = 4
	var packed []byte
	for i := 0; i < len(raw); {
		if raw[i] != 0 {
			packed = append(packed, raw[i])
			i++
			continue
		}
		j := i
		for j < len(raw) && raw[j] == 0 {
			j++
		}
		packed = append(packed, 0, byte(j-i))
		i = j
	}
	return base64.RawURLEncoding.EncodeToString(packed)
}
func TestPhotoDC(t *testing.T) {
	for dc := uint32(1); dc <= 5; dc++ {
		got, e := PhotoDC(photoFixture(dc))
		if e != nil || got != int(dc) {
			t.Fatal(got, e)
		}
	}
	for _, s := range []string{"", "***", base64.RawURLEncoding.EncodeToString([]byte{0}), strings.Repeat("A", 4097), photoFixture(0), photoFixture(255)} {
		if _, e := PhotoDC(s); e == nil {
			t.Fatal("accepted malformed/unknown photo", s)
		}
	}
}
func TestUserPhotoDC(t *testing.T) {
	var empty atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/getUserProfilePhotos" {
			t.Error(r.URL.Path)
		}
		var args map[string]int64
		if e := json.NewDecoder(r.Body).Decode(&args); e != nil || args["user_id"] != 42 || args["limit"] != 1 {
			t.Error(args, e)
		}
		photos := []any{}
		if !empty.Load() {
			photos = append(photos, []any{map[string]string{"file_id": photoFixture(4)}})
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"photos": photos}})
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client(), BaseURL: srv.URL}
	if dc, e := c.UserPhotoDC(context.Background(), 42); e != nil || dc != 4 {
		t.Fatal(dc, e)
	}
	empty.Store(true)
	if _, e := c.UserPhotoDC(context.Background(), 42); e != ErrDCUnavailable {
		t.Fatal(e)
	}
}
func FuzzPhotoDC(f *testing.F) {
	f.Add(photoFixture(2))
	f.Add("")
	f.Add("AAA")
	f.Fuzz(func(t *testing.T, s string) {
		dc, e := PhotoDC(s)
		if e == nil && (dc < 1 || dc > 5) {
			t.Fatal(dc)
		}
	})
}
