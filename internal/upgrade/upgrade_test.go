package upgrade

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestReleaseValidation(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{"tag_name":"v1.10.0","assets":[{"name":"SHA256SUMS"},{"name":"tgguard-linux-amd64"},{"name":"tgguard-linux-arm64"}]}`, true},
		{`{"tag_name":"v1.10.0","prerelease":true}`, false},
		{`{"tag_name":"v1.10.0"}`, false},
		{`{"tag_name":"v1.10.0;id"}`, false},
	} {
		c := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != LatestURL {
				t.Error(r.URL)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})}
		_, e := Latest(context.Background(), c)
		if (e == nil) != tc.ok {
			t.Fatal(tc, e)
		}
	}
	if !Newer("v1.10.0", "1.9.0") || Newer("v1.9.0", "1.10.0") || Newer("v1.10.0", "1.10.0") || Newer("v1.10.0;id", "1.9.0") {
		t.Fatal("version ordering")
	}
}
func queueFixture(t *testing.T) (Queue, *os.Root) {
	t.Helper()
	q := Queue{Dir: t.TempDir()}
	r, e := os.OpenRoot(q.Dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close() })
	if e = write(r, "heartbeat", time.Now()); e != nil {
		t.Fatal(e)
	}
	return q, r
}
func TestQueueAndAgentLifecycle(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			q, r := queueFixture(t)
			var count atomic.Int32
			var wg sync.WaitGroup
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					e := q.Request("v1.10.0")
					if e == nil {
						count.Add(1)
					} else if !errors.Is(e, ErrBusy) {
						t.Error(e)
					}
				}()
			}
			wg.Wait()
			if count.Load() != 1 {
				t.Fatal("duplicate jobs", count.Load())
			}
			if e := r.Rename("request.json", "active.json"); e != nil {
				t.Fatal(e)
			}
			if e := q.Request("v1.10.0"); !errors.Is(e, ErrBusy) {
				t.Fatal("active race", e)
			}
			ran := false
			e := process(context.Background(), r, func(context.Context) (Release, error) { return Release{Version: "v1.10.0"}, nil }, func(_ context.Context, v string) error {
				ran = true
				if q.Status().Phase != "running" || v != "v1.10.0" {
					t.Error("missing running checkpoint")
				}
				if fail {
					return errors.New("failure")
				}
				return nil
			})
			if e != nil || !ran {
				t.Fatal(e, ran)
			}
			want := "succeeded"
			if fail {
				want = "failed"
			}
			if q.Status().Phase != want {
				t.Fatal(q.Status())
			}
			if _, e = r.Stat("busy"); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("not released", e)
			}
		})
	}
}
func TestRejectChangedReleaseAndInactiveAgent(t *testing.T) {
	q, r := queueFixture(t)
	if e := q.Request("v1.10.0;echo"); e == nil {
		t.Fatal("invalid version")
	}
	if e := q.Request("v1.10.0"); e != nil {
		t.Fatal(e)
	}
	r.Rename("request.json", "active.json")
	e := process(context.Background(), r, func(context.Context) (Release, error) { return Release{Version: "v1.11.0"}, nil }, func(context.Context, string) error { t.Error("must not run changed release"); return nil })
	if e != nil || q.Status().Phase != "failed" {
		t.Fatal(e, q.Status())
	}
	r.Remove("heartbeat")
	if q.Ready() {
		t.Fatal("offline agent accepted")
	}
}
