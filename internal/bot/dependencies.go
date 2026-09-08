package bot

import (
	"context"
	"tgguard/internal/domain"
	"tgguard/internal/service"
	"tgguard/internal/store"
)

// Interfaces belong to the ingestion consumer, allowing real failure and
// cancellation paths to be tested without Telegram or an active bot.
type Inbox interface {
	Offset(context.Context) (int64, error)
	SaveOffset(context.Context, int64) error
	Enqueue(context.Context, domain.Update) error
	Claim(context.Context) (store.Job, error)
	Finish(context.Context, store.Job, error) error
}
type Caller interface {
	Call(context.Context, string, any, any) error
}

func (h *Handler) inbox() Inbox {
	if h.Queue != nil {
		return h.Queue
	}
	return h.Service.Store
}
func (h *Handler) caller() Caller {
	if h.Telegram != nil {
		return h.Telegram
	}
	return h.Service.Bot
}
func (h *Handler) health() *service.IngestionHealth {
	if h.Health != nil {
		return h.Health
	}
	if h.Service != nil {
		return h.Service.Health
	}
	return nil
}
