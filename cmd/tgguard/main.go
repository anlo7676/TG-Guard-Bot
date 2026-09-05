package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"tgguard/internal/ai"
	"tgguard/internal/api"
	"tgguard/internal/bot"
	"tgguard/internal/config"
	"tgguard/internal/service"
	"tgguard/internal/state"
	"tgguard/internal/store"
	"tgguard/internal/telegram"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if e := run(); e != nil {
		slog.Error("application stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	migrateOnly := flag.Bool("migrate-only", false, "apply MySQL migrations and exit")
	flag.Parse()
	c, e := config.Load()
	if e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	db, e := store.Open(startup, c.DSN)
	if e != nil {
		return e
	}
	defer db.DB.Close()
	if e = db.Migrate(startup); e != nil {
		return e
	}
	if *migrateOnly {
		slog.Info("database migrations complete")
		return nil
	}
	cache := state.New(c.RedisAddr, c.RedisPassword)
	defer cache.R.Close()
	if e = cache.R.Ping(startup).Err(); e != nil {
		return fmt.Errorf("Redis unavailable: %w", e)
	}
	tg := telegram.New(c.Token, cache)
	if e = tg.Identify(startup); e != nil {
		return e
	}
	provider := &ai.Compatible{BaseURL: c.AIBaseURL, Key: c.AIKey, Model: c.AIModel, Timeout: c.AITimeout, HTTP: &http.Client{Timeout: c.AITimeout, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}, State: cache, Store: db, Slots: make(chan struct{}, c.AIConcurrency)}
	svc := &service.Service{Store: db, State: cache, Bot: tg, AI: provider, SuperAdmins: c.SuperAdmins}
	handler := &bot.Handler{Service: svc}
	web := &api.Server{Service: svc, Config: c}
	server := &http.Server{Addr: c.HTTPAddr, Handler: web.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	// One process owns ingestion and recovery. Workers inside it process different chats concurrently.
	leaseConn, e := db.DB.Conn(ctx)
	if e != nil {
		return e
	}
	defer leaseConn.Close()
	var owned int
	if e = leaseConn.QueryRowContext(ctx, "SELECT GET_LOCK('tg_guard_single_instance',0)").Scan(&owned); e != nil {
		return e
	}
	if owned != 1 {
		return errors.New("another TG Guard instance is running; stop it before starting this instance")
	}
	defer leaseConn.ExecContext(context.Background(), "SELECT RELEASE_LOCK('tg_guard_single_instance')")
	if c.Mode == "polling" {
		e = tg.Call(startup, "deleteWebhook", map[string]any{"drop_pending_updates": false}, nil)
	} else {
		e = tg.Call(startup, "setWebhook", map[string]any{"url": c.WebhookURL, "secret_token": c.WebhookSecret, "allowed_updates": bot.AllowedUpdates, "drop_pending_updates": false, "max_connections": 1}, nil)
	}
	if e != nil {
		return e
	}
	errCh := make(chan error, 3)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); handler.Workers(ctx, c.Workers) }()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if e := server.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
			errCh <- e
		}
	}()
	if c.Mode == "polling" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := handler.Poll(ctx); e != nil {
				errCh <- e
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			if state.Sleep(ctx, 5*time.Second) != nil {
				return
			}
			var mine int
			check, done := context.WithTimeout(ctx, 3*time.Second)
			e := leaseConn.QueryRowContext(check, "SELECT IS_USED_LOCK('tg_guard_single_instance')=CONNECTION_ID()").Scan(&mine)
			done()
			if e != nil || mine != 1 {
				errCh <- errors.New("instance lock lost")
				return
			}
		}
	}()
	slog.Info("TG Guard started", "bot", tg.Username, "mode", c.Mode, "workers", c.Workers, "http_addr", c.HTTPAddr)
	select {
	case <-ctx.Done():
	case e = <-errCh:
	}
	stop()
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	server.Shutdown(shutdown)
	wg.Wait()
	return e
}
