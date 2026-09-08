package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	UpdateDir                                            string
	TrustedProxies                                       []netip.Prefix
	AIAllowInsecureHTTP                                  bool
	RetentionDays                                        int
	Token, Mode, HTTPAddr, DSN, RedisAddr, RedisPassword string
	AdminToken, WebhookURL, WebhookSecret                string
	AIBaseURL, AIKey, AIModel                            string
	AITimeout                                            time.Duration
	Workers, AIConcurrency                               int
	RedisDB                                              int
	SuperAdmins                                          map[int64]bool
	SettingsKey                                          string
}

func Load() (Config, error) {
	c := Config{Token: os.Getenv("BOT_TOKEN"), Mode: env("BOT_MODE", "polling"), HTTPAddr: env("HTTP_ADDR", ":8080"),
		DSN: os.Getenv("MYSQL_DSN"), RedisAddr: env("REDIS_ADDR", "127.0.0.1:6379"), RedisPassword: os.Getenv("REDIS_PASSWORD"),
		AdminToken: os.Getenv("ADMIN_API_TOKEN"), WebhookURL: os.Getenv("WEBHOOK_URL"), WebhookSecret: os.Getenv("WEBHOOK_SECRET"),
		AIBaseURL: env("AI_BASE_URL", "https://api.openai.com/v1"), AIKey: os.Getenv("AI_API_KEY"), AIModel: os.Getenv("AI_MODEL"), SuperAdmins: map[int64]bool{}}
	c.UpdateDir = os.Getenv("UPDATE_STATE_DIR")
	if c.Token == "" || c.DSN == "" {
		return c, errors.New("BOT_TOKEN and MYSQL_DSN are required")
	}
	if len(c.AdminToken) < 32 {
		return c, errors.New("ADMIN_API_TOKEN must have at least 32 characters")
	}
	c.SettingsKey = env("SETTINGS_ENCRYPTION_KEY", c.AdminToken)
	if len(c.SettingsKey) < 32 {
		return c, errors.New("SETTINGS_ENCRYPTION_KEY must have at least 32 characters")
	}
	if c.Mode != "polling" && c.Mode != "webhook" {
		return c, errors.New("BOT_MODE must be polling or webhook")
	}
	if c.Mode == "webhook" {
		u, err := url.Parse(c.WebhookURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "/telegram/webhook" || len(c.WebhookSecret) < 32 || len(c.WebhookSecret) > 256 || strings.IndexFunc(c.WebhookSecret, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
		}) >= 0 {
			return c, errors.New("webhook requires HTTPS /telegram/webhook and a 32-256 character secret using A-Z, a-z, 0-9, _ or -")
		}
	}
	var err error
	for _, value := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",") {
		if strings.TrimSpace(value) == "" {
			continue
		}
		prefix, e := netip.ParsePrefix(strings.TrimSpace(value))
		if e != nil {
			return c, errors.New("TRUSTED_PROXY_CIDRS must contain valid CIDR ranges")
		}
		c.TrustedProxies = append(c.TrustedProxies, prefix)
	}
	if c.AIAllowInsecureHTTP, err = strconv.ParseBool(env("AI_ALLOW_INSECURE_HTTP", "false")); err != nil {
		return c, errors.New("AI_ALLOW_INSECURE_HTTP must be true or false")
	}
	if c.RetentionDays, err = number("RETENTION_DAYS", 90, 0, 3650); err != nil {
		return c, err
	}
	if c.RedisDB, err = number("REDIS_DB", 0, 0, 15); err != nil {
		return c, err
	}
	if c.Workers, err = number("WORKERS", 8, 1, 64); err != nil {
		return c, err
	}
	if c.AIConcurrency, err = number("AI_CONCURRENCY", 4, 1, 64); err != nil {
		return c, err
	}
	if c.AITimeout, err = time.ParseDuration(env("AI_TIMEOUT", "12s")); err != nil || c.AITimeout < time.Second || c.AITimeout > 30*time.Second {
		return c, errors.New("AI_TIMEOUT must be 1s-30s")
	}
	if c.AIKey != "" {
		if strings.HasPrefix(strings.ToLower(c.AIBaseURL), "http://") && !c.AIAllowInsecureHTTP {
			return c, errors.New("AI HTTP requires explicit AI_ALLOW_INSECURE_HTTP=true")
		}
		u, e := url.Parse(c.AIBaseURL)
		if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || c.AIModel == "" {
			return c, errors.New("AI_BASE_URL and AI_MODEL are invalid")
		}
	}
	for _, s := range strings.Split(os.Getenv("BOT_SUPER_ADMINS"), ",") {
		if strings.TrimSpace(s) == "" {
			continue
		}
		id, e := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if e != nil || id <= 0 {
			return c, errors.New("invalid BOT_SUPER_ADMINS")
		}
		c.SuperAdmins[id] = true
	}
	return c, nil
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func number(k string, d, min, max int) (int, error) {
	v, e := strconv.Atoi(env(k, strconv.Itoa(d)))
	if e != nil || v < min || v > max {
		return 0, fmt.Errorf("%s must be %d-%d", k, min, max)
	}
	return v, nil
}
