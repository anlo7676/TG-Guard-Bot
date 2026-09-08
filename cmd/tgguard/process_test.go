package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-sql-driver/mysql"
	"tgguard/internal/config"
	"tgguard/internal/state"
	"tgguard/internal/telegram"
)

// The subprocess uses its own database and a simulated Telegram endpoint.
func TestApplicationProcess(t *testing.T) {
	if os.Getenv("TG_PROCESS_TEST_CHILD") == "1" {
		c, e := config.Load()
		if e != nil {
			t.Fatal(e)
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
		defer stop()
		e = runApplication(ctx, c, false, func(token string, s *state.State) *telegram.Client {
			client := telegram.New(token, s)
			client.BaseURL = os.Getenv("TG_PROCESS_TEST_TELEGRAM")
			return client
		})
		if e != nil {
			t.Fatal(e)
		}
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM process regression runs in Linux CI")
	}
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requires isolated MySQL test database")
	}
	cfg, e := mysql.ParseDSN(dsn)
	if e != nil || !strings.HasSuffix(cfg.DBName, "_test") || !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(cfg.DBName) {
		t.Fatal("unsafe test DSN")
	}
	cfg.DBName = fmt.Sprintf("tg_process_%d_test", time.Now().UnixNano())
	dbName := cfg.DBName
	adminCfg := *cfg
	adminCfg.DBName = ""
	db, e := sql.Open("mysql", adminCfg.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); e != nil {
		t.Fatal(e)
	}
	defer db.Exec("DROP DATABASE `" + dbName + "`")
	cache := miniredis.RunT(t)
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/getMe" {
			fmt.Fprint(w, `{"ok":true,"result":{"id":987654,"username":"ProcessTestBot","is_bot":true}}`)
			return
		}
		if r.URL.Path == "/getUpdates" {
			select {
			case <-time.After(100 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
			fmt.Fprint(w, `{"ok":true,"result":[]}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	}))
	defer telegramServer.Close()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := listener.Addr().String()
	listener.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestApplicationProcess$", "-test.timeout=40s")
	values := map[string]string{"TG_PROCESS_TEST_CHILD": "1", "TG_PROCESS_TEST_TELEGRAM": telegramServer.URL, "BOT_TOKEN": "987654:fake-process-token", "MYSQL_DSN": cfg.FormatDSN(), "REDIS_ADDR": cache.Addr(), "REDIS_PASSWORD": "", "REDIS_DB": "0", "HTTP_ADDR": addr, "ADMIN_API_TOKEN": strings.Repeat("t", 64), "SETTINGS_ENCRYPTION_KEY": strings.Repeat("k", 64), "BOT_MODE": "polling", "AI_API_KEY": "", "AI_BASE_URL": "https://example.com/v1", "AI_ALLOW_INSECURE_HTTP": "false", "BOT_SUPER_ADMINS": "", "TRUSTED_PROXY_CIDRS": ""}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if _, replace := values[key]; !replace {
			cmd.Env = append(cmd.Env, value)
		}
	}
	for key, value := range values {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	client := &http.Client{Timeout: time.Second}
	ready := false
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		response, err := client.Get("http://" + addr + "/health/ready")
		if err == nil {
			ready = response.StatusCode == 200
			response.Body.Close()
			if ready {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("application did not become ready")
	}
	if e = cmd.Process.Signal(syscall.SIGTERM); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		waited = true
		if e != nil {
			t.Fatalf("process stop: %v\n%s", e, output.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("process did not stop")
	}
}
