package state

import (
	"context"
	"fmt"
	"strconv"
)

// BindBot prevents another bot from reusing callback tokens, rate counters and sessions.
func (s *State) BindBot(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("invalid bot identity")
	}
	owner, e := s.R.Eval(ctx, `local owner=redis.call("GET",KEYS[1]); if owner then return owner end; if redis.call("DBSIZE")>0 then return redis.error_reply("unbound Redis database contains previous data; select a fresh REDIS_DB") end; redis.call("SET",KEYS[1],ARGV[1]); return ARGV[1]`, []string{"tg_guard:bot_id"}, id).Text()
	if e != nil {
		return e
	}
	if owner != strconv.FormatInt(id, 10) {
		return fmt.Errorf("Redis database belongs to bot %s; select a separate REDIS_DB", owner)
	}
	return nil
}
