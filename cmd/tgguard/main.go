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
	"tgguard/internal/buildinfo"
	"tgguard/internal/config"
	"tgguard/internal/service"
	"tgguard/internal/settings"
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
	return runApplication(ctx, c, *migrateOnly, telegram.New)
}

func runApplication(parent context.Context, c config.Config, migrateOnly bool, newTelegram func(string, *state.State) *telegram.Client) error {
	ctx, stop := context.WithCancel(parent)
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
	if migrateOnly {
		slog.Info("database migrations complete")
		return nil
	}
	cache := state.NewDB(c.RedisAddr, c.RedisPassword, c.RedisDB)
	defer cache.R.Close()
	if e = cache.R.Ping(startup).Err(); e != nil {
		return fmt.Errorf("Redis unavailable: %w", e)
	}
	tg := newTelegram(c.Token, nil)
	if e = tg.Identify(startup); e != nil {
		return e
	}
	if e = db.BindBot(startup, tg.ID); e != nil {
		return e
	}
	if e = cache.BindBot(startup, tg.ID); e != nil {
		return e
	}
	tg.State = cache
	ids := []int64{}
	for id := range c.SuperAdmins {
		ids = append(ids, id)
	}
	runtime, e := settings.New(startup, db, c.SettingsKey, settings.Config{SuperAdmins: ids, AI: settings.AI{AllowInsecureHTTP: c.AIAllowInsecureHTTP, Enabled: c.AIKey != "", BaseURL: c.AIBaseURL, Model: c.AIModel, APIKey: c.AIKey, TimeoutSeconds: int(c.AITimeout.Seconds()), MaxTokens: 500, TokenParameter: "max_completion_tokens"}})
	if e != nil {
		return e
	}
	provider := &ai.Live{Settings: runtime, State: cache, Store: db, Slots: make(chan struct{}, c.AIConcurrency)}
	svc, e := service.New(service.Service{Health: service.NewIngestionHealth(c.Mode), RetentionDays: c.RetentionDays, Store: db, State: cache, Bot: tg, AI: provider, SuperAdmins: c.SuperAdmins, Runtime: runtime})
	if e != nil {
		return e
	}
	handler := &bot.Handler{Service: svc}
	web := &api.Server{Service: svc, Config: c}
	server := newHTTPServer(ctx, c.HTTPAddr, web.Handler())

	// One process owns ingestion and recovery. Workers inside it process different chats concurrently.
	leaseConn, e := db.DB.Conn(ctx)
	if e != nil {
		return e
	}
	defer leaseConn.Close()
	lockName, e := db.LockName(ctx, "instance")
	if e != nil {
		return e
	}
	var owned int
	if e = leaseConn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lockName).Scan(&owned); e != nil {
		return e
	}
	if owned != 1 {
		return errors.New("another TG Guard instance is running; stop it before starting this instance")
	}
	defer store.ReleaseLock(leaseConn, lockName)
	if e = svc.RegisterMenus(startup); e != nil {
		return e
	}
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
		if err := watchInstance(ctx, leaseConn, lockName, 5*time.Second); err != nil {
			errCh <- err
		}
	}()
	slog.Info("TG Guard started", "bot", tg.Username, "mode", c.Mode, "workers", c.Workers, "http_addr", c.HTTPAddr, "build", buildinfo.Info())
	select {
	case <-ctx.Done():
	case e = <-errCh:
	}
	stop()
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	if shutdownErr := shutdownHTTP(shutdown, server); shutdownErr != nil && e == nil {
		e = shutdownErr
	}
	wg.Wait()
	return e
}
