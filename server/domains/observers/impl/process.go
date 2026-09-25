package impl

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/contracts"
)

type eventDispatcher struct {
	observers []contracts.ProcessObserver
}

func NewEventDispatcher() contracts.EventDispatcher {
	return &eventDispatcher{
		observers: make([]contracts.ProcessObserver, 0),
	}
}

func (d *eventDispatcher) Register(observer contracts.ProcessObserver) {
	d.observers = append(d.observers, observer)
}

func (d *eventDispatcher) Dispatch(ctx context.Context, event entities.ProcessEvent) {
	for _, observer := range d.observers {
		observer.OnEvent(ctx, event)
	}
}
