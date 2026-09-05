package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"tgguard/internal/domain"
	"tgguard/internal/state"
)

type Client struct {
	BaseURL  string
	HTTP     *http.Client
	State    *state.State
	ID       int64
	Username string
}
type APIError struct {
	Code        int
	Description string
}

func (e *APIError) Error() string { return fmt.Sprintf("telegram error %d: %s", e.Code, e.Description) }
func New(token string, s *state.State) *Client {
	return &Client{BaseURL: "https://api.telegram.org/bot" + token, HTTP: &http.Client{Timeout: 40 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}, State: s}
}
func (c *Client) Call(ctx context.Context, method string, in, out any) error {
	b, e := json.Marshal(in)
	if e != nil {
		return e
	}
	for attempt := 0; attempt < 3; attempt++ {
		if c.State != nil {
			if strings.HasPrefix(method, "send") {
				if args, ok := in.(map[string]any); ok {
					for {
						allowed, err := c.State.Limit(ctx, fmt.Sprintf("telegram:send:%v", args["chat_id"]), 1, time.Second)
						if err != nil {
							return err
						}
						if allowed {
							break
						}
						if err = state.Sleep(ctx, 100*time.Millisecond); err != nil {
							return err
						}
					}
				}
			}
			for {
				ok, e := c.State.Limit(ctx, "telegram:rate", 25, time.Second)
				if e != nil {
					return e
				}
				if ok {
					break
				}
				if e = state.Sleep(ctx, 100*time.Millisecond); e != nil {
					return e
				}
			}
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/"+method, bytes.NewReader(b))
		if e != nil {
			return errors.New("invalid Telegram endpoint")
		}
		req.Header.Set("Content-Type", "application/json")
		resp, e := c.HTTP.Do(req)
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("telegram %s transport failure", method)
		}
		var envelope struct {
			OK          bool            `json:"ok"`
			Result      json.RawMessage `json:"result"`
			Code        int             `json:"error_code"`
			Description string          `json:"description"`
			Parameters  struct {
				RetryAfter int `json:"retry_after"`
			} `json:"parameters"`
		}
		e = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&envelope)
		resp.Body.Close()
		if e != nil {
			return fmt.Errorf("telegram %s invalid response (HTTP %d)", method, resp.StatusCode)
		}
		if envelope.Code == 429 && envelope.Parameters.RetryAfter > 0 && attempt < 2 {
			if e = state.Sleep(ctx, time.Duration(envelope.Parameters.RetryAfter)*time.Second); e != nil {
				return e
			}
			continue
		}
		if !envelope.OK {
			return &APIError{Code: envelope.Code, Description: envelope.Description}
		}
		if out != nil {
			return json.Unmarshal(envelope.Result, out)
		}
		return nil
	}
	return errors.New("telegram retry exhausted")
}
func (c *Client) Identify(ctx context.Context) error {
	var u domain.User
	if e := c.Call(ctx, "getMe", map[string]any{}, &u); e != nil {
		return e
	}
	c.ID = u.ID
	c.Username = u.Username
	return nil
}
func (c *Client) Member(ctx context.Context, chat, user int64) (domain.Member, error) {
	var m domain.Member
	e := c.Call(ctx, "getChatMember", map[string]any{"chat_id": chat, "user_id": user}, &m)
	return m, e
}
func (c *Client) Send(ctx context.Context, chat int64, text string, markup any, reply int64) (int64, error) {
	in := map[string]any{"chat_id": chat, "text": text}
	if markup != nil {
		in["reply_markup"] = markup
	}
	if reply > 0 {
		in["reply_parameters"] = map[string]any{"message_id": reply, "allow_sending_without_reply": true}
	}
	var m domain.Message
	e := c.Call(ctx, "sendMessage", in, &m)
	return m.ID, e
}
func (c *Client) Delete(ctx context.Context, chat, message int64) error {
	e := c.Call(ctx, "deleteMessage", map[string]any{"chat_id": chat, "message_id": message}, nil)
	var te *APIError
	if errors.As(e, &te) && te.Code == 400 && strings.Contains(strings.ToLower(te.Description), "message to delete not found") {
		return nil
	}
	return e
}
func (c *Client) Restrict(ctx context.Context, chat, user int64, seconds int) error {
	in := map[string]any{"chat_id": chat, "user_id": user, "permissions": map[string]bool{"can_send_messages": false}, "use_independent_chat_permissions": true}
	if seconds > 0 {
		in["until_date"] = time.Now().Add(time.Duration(seconds) * time.Second).Unix()
	}
	return c.Call(ctx, "restrictChatMember", in, nil)
}
func (c *Client) Restore(ctx context.Context, chat, user int64) error {
	var ch domain.Chat
	if e := c.Call(ctx, "getChat", map[string]any{"chat_id": chat}, &ch); e != nil {
		return e
	}
	if ch.Permissions == nil {
		return errors.New("group default permissions unavailable")
	}
	return c.Call(ctx, "restrictChatMember", map[string]any{"chat_id": chat, "user_id": user, "permissions": ch.Permissions, "use_independent_chat_permissions": true}, nil)
}
func (c *Client) Ban(ctx context.Context, chat, user int64) error {
	return c.Call(ctx, "banChatMember", map[string]any{"chat_id": chat, "user_id": user, "revoke_messages": true}, nil)
}
func (c *Client) Unban(ctx context.Context, chat, user int64) error {
	return c.Call(ctx, "unbanChatMember", map[string]any{"chat_id": chat, "user_id": user, "only_if_banned": true}, nil)
}
func (c *Client) Kick(ctx context.Context, chat, user int64) error {
	if e := c.Ban(ctx, chat, user); e != nil {
		return e
	}
	return c.Unban(ctx, chat, user)
}
func (c *Client) AnswerCallback(ctx context.Context, id, text string) error {
	return c.Call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
}
