package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// ProcessObserver defines the interface for observing process events.
type ProcessObserver interface {
	OnEvent(ctx context.Context, event entities.ProcessEvent)
}

// EventDispatcher defines the interface for dispatching process events.
type EventDispatcher interface {
	Register(observer ProcessObserver)
	Dispatch(ctx context.Context, event entities.ProcessEvent)
}
