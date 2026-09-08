package service

import "errors"

// New validates the production graph once; tests may inject narrow adapters directly.
func New(deps Service) (*Service, error) {
	if deps.Store == nil || deps.Store.DB == nil {
		return nil, errors.New("service requires a repository")
	}
	if deps.State == nil || deps.State.R == nil {
		return nil, errors.New("service requires Redis state")
	}
	if deps.Bot == nil || deps.Bot.HTTP == nil {
		return nil, errors.New("service requires a Telegram client")
	}
	if deps.Runtime == nil || deps.Health == nil {
		return nil, errors.New("service requires runtime settings and ingestion health")
	}
	if deps.Executor == nil {
		deps.Executor = deps.Bot
	}
	return &deps, nil
}
