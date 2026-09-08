package state

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

type State struct{ R *redis.Client }

func New(addr, password string) *State {
	return NewDB(addr, password, 0)
}
func NewDB(addr, password string, database int) *State {
	return &State{R: redis.NewClient(&redis.Options{DB: database, Addr: addr, Password: password, PoolSize: 32, DialTimeout: 3 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second})}
}
func Token() (string, error) {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func Hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func (s *State) Put(ctx context.Context, key string, v any, ttl time.Duration) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return s.R.Set(ctx, key, b, ttl).Err()
}
func (s *State) Get(ctx context.Context, key string, v any) error {
	b, e := s.R.Get(ctx, key).Bytes()
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func (s *State) Lock(ctx context.Context, key string, ttl time.Duration) (func(), error) {
	token, e := Token()
	if e != nil {
		return nil, e
	}
	ok, e := s.R.SetNX(ctx, key, token, ttl).Result()
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, errors.New("resource is busy; retry later")
	}
	return func() {
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.R.Eval(c, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, []string{key}, token).Err(); err != nil {
			slog.Warn("Redis unlock failed", "lock_key", key, "error", err)
		}
	}, nil
}

// Each update contributes once even if a durable inbox job is retried.
func (s *State) Spam(ctx context.Context, chat, user, update int64, text string, window int) (int, int, error) {
	base := fmt.Sprintf("spam:{%d:%d}", chat, user)
	r, e := s.R.Eval(ctx, `local memo=redis.call('GET',KEYS[3]); if memo then return cjson.decode(memo) end
 local a=redis.call('INCR',KEYS[1]); if a==1 then redis.call('EXPIRE',KEYS[1],ARGV[1]) end
 local b=redis.call('INCR',KEYS[2]); if b==1 then redis.call('EXPIRE',KEYS[2],60) end
 redis.call('SET',KEYS[3],cjson.encode({a,b}),'EX',86400); return {a,b}`, []string{base + ":rate", base + ":dup:" + Hash(text), base + fmt.Sprintf(":event:%d", update)}, window).Int64Slice()
	if e != nil {
		return 0, 0, e
	}
	return int(r[0]), int(r[1]), nil
}
func (s *State) Limit(ctx context.Context, key string, max int, ttl time.Duration) (bool, error) {
	n, e := s.R.Eval(ctx, `local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end return n`, []string{key}, ttl.Milliseconds()).Int64()
	return n <= int64(max), e
}
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
