package app

import (
	"context"
)

// startRabbitMQ runs the RabbitMQ bridges and consumers the environment names.
//
// Nothing started them before: StartBridge, StartInboundConsumer and StopAll
// had no callers, so a running server held no broker connection at all while
// the README said it correlated RabbitMQ messages and bridged external tasks.
//
// An installation that names none starts nothing and connects to no broker,
// exactly as before.
func (a *App) startRabbitMQ(ctx context.Context) {
	bridges, consumers := readRabbitMQConfiguration()
	if len(bridges) == 0 && len(consumers) == 0 {
		return
	}
	a.rabbitMQ = newRabbitMQRunner(a.svc, a.svc, a.projectOrganization)
	a.rabbitMQ.start(ctx, bridges, consumers)
}

// stopRabbitMQ stops them again, waiting no longer than ctx allows.
func (a *App) stopRabbitMQ(ctx context.Context) {
	if a.rabbitMQ == nil {
		return
	}
	a.rabbitMQ.stop(ctx)
}
