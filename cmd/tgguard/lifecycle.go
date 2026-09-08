package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"tgguard/internal/state"
	"time"
)

func newHTTPServer(ctx context.Context, addr string, handler http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: handler, BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
}
func shutdownHTTP(ctx context.Context, server *http.Server) error {
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("HTTP graceful shutdown failed", "error", err)
		if e := server.Close(); e != nil {
			slog.Error("HTTP forced close failed", "error", e)
		}
		return err
	}
	return nil
}
func watchInstance(ctx context.Context, c *sql.Conn, name string, interval time.Duration) error {
	for ctx.Err() == nil {
		if state.Sleep(ctx, interval) != nil {
			return nil
		}
		check, cancel := context.WithTimeout(ctx, 3*time.Second)
		var mine int
		err := c.QueryRowContext(check, "SELECT IS_USED_LOCK(?)=CONNECTION_ID()", name).Scan(&mine)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return fmt.Errorf("instance lock check failed: %w", err)
		}
		if mine != 1 {
			return fmt.Errorf("instance lock lost")
		}
	}
	return nil
}
