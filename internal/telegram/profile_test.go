package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProfileKeepsBioWhenAvatarUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getChat") {
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"bio": "heiying.co", "photo": map[string]string{"big_file_id": "photo"}}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 400, "description": "unavailable"})
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/botsecret", HTTP: srv.Client()}
	p := c.Profile(context.Background(), 42, true)
	if !p.BioAvailable || p.Bio != "heiying.co" || p.PhotoAvailable {
		t.Fatalf("unexpected result %+v", p)
	}
}

func TestProfileDownloadBoundsAndRedirects(t *testing.T) {
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/redirect") {
			http.Redirect(w, r, target.URL, 302)
			return
		}
		w.Write([]byte(strings.Repeat("x", (2<<20)+1)))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/botsecret", HTTP: srv.Client()}
	for _, path := range []string{"redirect", "large", "../escape", "/absolute", "photos/%2e%2e/secret", "photos/a?token=x"} {
		if _, err := c.profileFile(context.Background(), path); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe result for %s: %v", path, err)
		}
	}
	if calls != 0 {
		t.Fatal("followed profile redirect")
	}
}

func TestProfileCanInspectPhotoWithoutBio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/botsecret/getChat":
			w.Write([]byte(`{"ok":false,"error_code":400,"description":"chat not found"}`))
		case "/botsecret/getUserProfilePhotos":
			w.Write([]byte(`{"ok":true,"result":{"photos":[[{"file_id":"small"},{"file_id":"large"}]]}}`))
		case "/botsecret/getFile":
			w.Write([]byte(`{"ok":true,"result":{"file_path":"photos/a.jpg"}}`))
		case "/file/botsecret/photos/a.jpg":
			w.Write([]byte("image data"))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/botsecret", HTTP: srv.Client()}
	p := c.Profile(context.Background(), 42, true)
	if p.BioAvailable || !p.PhotoAvailable || string(p.Photo) != "image data" {
		t.Fatalf("unexpected result %+v", p)
	}
}
