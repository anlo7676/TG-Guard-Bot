package telegram

import (
	"context"
	"errors"
	"time"
)

type menuBudgetKey struct{}

var ErrMenuLimited = errors.New("private menu budget exhausted")

// MenuContext is only for discovery/display, never for permission-changing actions.
func MenuContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, menuBudgetKey{}, true)
}

func isMenu(ctx context.Context) bool { return ctx.Value(menuBudgetKey{}) == true }

func (c *Client) menuBudget(ctx context.Context, method string) error {
	if c.State == nil || !isMenu(ctx) {
		return nil
	}
	// Queries and display responses together cannot consume the 25/s global budget.
	// Reject immediately: background discovery must not wait in the worker queue.
	if method == "getChatMember" {
		ok, err := c.State.Limit(ctx, "telegram:menu:lookup", 5, time.Second)
		if err != nil {
			return err
		}
		if !ok {
			return ErrMenuLimited
		}
	}
	ok, err := c.State.Limit(ctx, "telegram:menu:all", 8, time.Second)
	if err != nil {
		return err
	}
	if !ok {
		return ErrMenuLimited
	}
	return nil
}
