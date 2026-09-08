package main

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServerRequestsFollowShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	finished := make(chan struct{})
	s := newHTTPServer(ctx, "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(finished) }))
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	served := make(chan error, 1)
	go func() { served <- s.Serve(listener) }()
	response := make(chan struct{})
	go func() {
		defer close(response)
		resp, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request not started")
	}
	cancel()
	stop, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	if e = shutdownHTTP(stop, s); e != nil {
		t.Fatal(e)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("request not cancelled")
	}
	<-response
	if e = <-served; !errors.Is(e, http.ErrServerClosed) {
		t.Fatal(e)
	}
}
func TestInstanceLockLossAndCancellation(t *testing.T) {
	db, mock, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ctx := context.Background()
	c, e := db.Conn(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	mock.ExpectQuery("SELECT IS_USED_LOCK").WithArgs("instance").WillReturnRows(sqlmock.NewRows([]string{"mine"}).AddRow(1))
	mock.ExpectQuery("SELECT IS_USED_LOCK").WithArgs("instance").WillReturnRows(sqlmock.NewRows([]string{"mine"}).AddRow(0))
	if e = watchInstance(ctx, c, "instance", time.Millisecond); e == nil {
		t.Fatal("lock loss ignored")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if e = watchInstance(cancelled, c, "instance", time.Millisecond); e != nil {
		t.Fatal(e)
	}
	if e = mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
