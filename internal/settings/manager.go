package settings

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

type AI struct {
	Enabled        bool   `json:"enabled"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	APIKey         string `json:"api_key,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MaxTokens      int    `json:"max_tokens"`
	TokenParameter string `json:"token_parameter"`
}
type Config struct {
	SuperAdmins []int64 `json:"super_admins"`
	PanelURL    string  `json:"panel_url"`
	AI          AI      `json:"ai"`
}
type PublicAI struct {
	Enabled        bool   `json:"enabled"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	KeyConfigured  bool   `json:"key_configured"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MaxTokens      int    `json:"max_tokens"`
	TokenParameter string `json:"token_parameter"`
}
type Public struct {
	SuperAdmins []int64  `json:"super_admins"`
	PanelURL    string   `json:"panel_url"`
	AI          PublicAI `json:"ai"`
}

func (c Config) Public() Public {
	ids := append([]int64{}, c.SuperAdmins...)
	a := c.AI
	return Public{ids, c.PanelURL, PublicAI{a.Enabled, a.BaseURL, a.Model, a.APIKey != "", a.TimeoutSeconds, a.MaxTokens, a.TokenParameter}}
}
func (c Config) Validate() error {
	if len(c.SuperAdmins) > 100 {
		return errors.New("最多可设置 100 位机器人管理员")
	}
	seen := map[int64]bool{}
	for _, id := range c.SuperAdmins {
		if id <= 0 || id > 9007199254740991 || seen[id] {
			return errors.New("管理员 ID 必须为不重复的 Telegram 数字 ID")
		}
		seen[id] = true
	}
	if c.PanelURL != "" {
		if !validURL(c.PanelURL) {
			return errors.New("后台地址必须是有效的 HTTP/HTTPS 地址，不能包含密码、参数或片段")
		}
	}
	a := c.AI
	if !validURL(a.BaseURL) || len(a.BaseURL) > 2048 {
		return errors.New("AI Base URL 不正确")
	}
	if len(a.APIKey) > 4096 || strings.ContainsAny(a.APIKey, "\r\n") || len(a.Model) > 255 {
		return errors.New("AI 密钥或模型格式不正确")
	}
	if a.Enabled && (strings.TrimSpace(a.Model) == "" || a.APIKey == "") {
		return errors.New("启用 AI 前请填写模型名称和 API Key")
	}
	if a.TimeoutSeconds < 1 || a.TimeoutSeconds > 30 || a.MaxTokens < 100 || a.MaxTokens > 8192 {
		return errors.New("AI 超时需为 1–30 秒，输出 Token 上限为 100–8192")
	}
	if a.TokenParameter != "max_completion_tokens" && a.TokenParameter != "max_tokens" {
		return errors.New("Token 参数只能是 max_completion_tokens 或 max_tokens")
	}
	return nil
}
func validURL(s string) bool {
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

type Repository interface {
	SystemSettings(context.Context) (string, error)
	SaveSystemSettings(context.Context, string, any, any) error
}
type Manager struct {
	repo    Repository
	aead    cipher.AEAD
	current atomic.Pointer[Config]
	mu      sync.Mutex
}

func New(ctx context.Context, repo Repository, master string, defaults Config) (*Manager, error) {
	if len(master) < 32 {
		return nil, errors.New("settings encryption key is too short")
	}
	key := sha256.Sum256([]byte("tg-guard-settings-v1:" + master))
	block, e := aes.NewCipher(key[:])
	if e != nil {
		return nil, e
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	m := &Manager{repo: repo, aead: aead}
	b, e := repo.SystemSettings(ctx)
	if e == nil {
		if defaults, e = m.decrypt(b); e != nil {
			return nil, errors.New("系统配置解密失败，请检查 SETTINGS_ENCRYPTION_KEY")
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if e = defaults.Validate(); e != nil {
		return nil, e
	}
	m.current.Store(&defaults)
	return m, nil
}
func (m *Manager) Snapshot() Config {
	c := *m.current.Load()
	c.SuperAdmins = append([]int64{}, c.SuperAdmins...)
	return c
}
func (m *Manager) IsAdmin(id int64) bool {
	for _, v := range m.current.Load().SuperAdmins {
		if id == v {
			return true
		}
	}
	return false
}

// Empty APIKey retains the stored key. Clearing a key is explicit and requires disabling AI.
func (m *Manager) Save(ctx context.Context, next Config, clearKey bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old := m.Snapshot()
	if clearKey {
		next.AI.APIKey = ""
	} else if next.AI.APIKey == "" {
		next.AI.APIKey = old.AI.APIKey
	}
	next.AI.BaseURL = strings.TrimRight(strings.TrimSpace(next.AI.BaseURL), "/")
	next.AI.Model = strings.TrimSpace(next.AI.Model)
	if e := next.Validate(); e != nil {
		return e
	}
	sealed, e := m.encrypt(next)
	if e != nil {
		return e
	}
	if e = m.repo.SaveSystemSettings(ctx, sealed, old.Public(), next.Public()); e != nil {
		return e
	}
	next.SuperAdmins = append([]int64{}, next.SuperAdmins...)
	m.current.Store(&next)
	return nil
}
func (m *Manager) encrypt(c Config) (string, error) {
	b, e := json.Marshal(c)
	if e != nil {
		return "", e
	}
	nonce := make([]byte, m.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	out := m.aead.Seal(nonce, nonce, b, []byte("system_settings:v1"))
	return base64.RawStdEncoding.EncodeToString(out), nil
}
func (m *Manager) decrypt(s string) (Config, error) {
	var c Config
	b, e := base64.RawStdEncoding.DecodeString(s)
	if e != nil || len(b) < m.aead.NonceSize() {
		return c, errors.New("invalid ciphertext")
	}
	n := m.aead.NonceSize()
	plain, e := m.aead.Open(nil, b[:n], b[n:], []byte("system_settings:v1"))
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(plain, &c)
	return c, e
}
