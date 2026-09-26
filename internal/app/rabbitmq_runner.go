package app

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// How long a bridge or consumer that cannot start waits before trying again.
//
// What stops one starting — a project or a connection that does not exist, a
// connection that is not RabbitMQ — waits for somebody to fix it, so the wait
// doubles each time, up to five minutes: a mistake nobody notices for a week
// costs a line every five minutes rather than one every five seconds. Once it
// has started, a broker that goes away is the messaging service's to reconnect
// to, which it does on a schedule of the same shape: from five seconds,
// doubling to five minutes, with jitter.
const (
	rabbitMQRetryFirst = 5 * time.Second
	rabbitMQRetryMost  = 5 * time.Minute
)

// rabbitMQRunner starts the RabbitMQ bridges and consumers an operator has
// named, keeps trying the ones that cannot start yet, and stops them all on
// shutdown.
//
// It decides which project, which connection and whose work; the messaging
// service does the rest — the broker connection, reconnecting, publishing and
// consuming. Every replica runs the same list, which is safe: see
// docs/recovery.md §2.1.
type rabbitMQRunner struct {
	messaging      servicecontracts.MessagingService
	connections    rabbitMQConnections
	organizationOf func(ctx context.Context, project uuid.UUID) (uuid.UUID, error)
	logger         zerolog.Logger
	retryFirst     time.Duration
	retryMost      time.Duration

	cancel context.CancelFunc
	loops  sync.WaitGroup
}

func newRabbitMQRunner(
	messaging servicecontracts.MessagingService,
	connections rabbitMQConnections,
	organizationOf func(ctx context.Context, project uuid.UUID) (uuid.UUID, error),
) *rabbitMQRunner {
	return &rabbitMQRunner{
		messaging:      messaging,
		connections:    connections,
		organizationOf: organizationOf,
		logger:         log.Logger,
		retryFirst:     rabbitMQRetryFirst,
		retryMost:      rabbitMQRetryMost,
	}
}

// start runs every bridge and consumer in the background, and returns at once:
// the server serves whether or not any of them can start.
func (r *rabbitMQRunner) start(ctx context.Context, bridges []rabbitMQBridge, consumers []rabbitMQConsumer) {
	ctx, r.cancel = context.WithCancel(ctx)
	for _, bridge := range bridges {
		logger := bridge.logger(&r.logger)
		r.loops.Go(func() {
			r.keepTrying(ctx, &logger, "bridge", func(ctx context.Context) error {
				return r.startBridge(ctx, bridge, &logger)
			})
		})
	}
	for _, consumer := range consumers {
		logger := consumer.logger(&r.logger)
		r.loops.Go(func() {
			r.keepTrying(ctx, &logger, "consumer", func(ctx context.Context) error {
				return r.startConsumer(ctx, consumer, &logger)
			})
		})
	}
}

// keepTrying starts one bridge or consumer, and when it cannot, says why and
// tries again later, waiting twice as long each time up to retryMost.
func (r *rabbitMQRunner) keepTrying(ctx context.Context, logger *zerolog.Logger, kind string, start func(context.Context) error) {
	delay := r.retryFirst
	for {
		err := start(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		// Redacted: the reason can carry a database error, and nothing in a
		// log line should be a credential.
		logger.Error().Str("error", redaction.RedactError(err)).Dur("retryIn", delay).
			Msgf("A RabbitMQ %s could not start; it will try again", kind)
		if !waitOrStop(ctx, delay) {
			return
		}
		delay = min(delay*2, r.retryMost)
	}
}

func (r *rabbitMQRunner) startBridge(ctx context.Context, bridge rabbitMQBridge, logger *zerolog.Logger) error {
	scoped, broker, err := r.resolve(ctx, bridge.target)
	if err != nil {
		return err
	}
	err = r.messaging.StartBridge(scoped, bridge.target.project, bridge.topic, broker.url, bridge.exchange, bridge.routingKey, bridge.lock)
	if err != nil {
		return err
	}
	logger.Info().Str("organization", broker.organization.String()).
		Str("broker", broker.address).Str("vhost", broker.vhost).Str("lock", bridge.lock.String()).
		Msg("Started a RabbitMQ bridge; it connects to its broker in the background")
	return nil
}

func (r *rabbitMQRunner) startConsumer(ctx context.Context, consumer rabbitMQConsumer, logger *zerolog.Logger) error {
	scoped, broker, err := r.resolve(ctx, consumer.target)
	if err != nil {
		return err
	}
	err = r.messaging.StartInboundConsumer(scoped, consumer.target.project, broker.url, consumer.queue, consumer.message)
	if err != nil {
		return err
	}
	logger.Info().Str("organization", broker.organization.String()).
		Str("broker", broker.address).Str("vhost", broker.vhost).
		Msg("Started a RabbitMQ consumer; it connects to its broker in the background")
	return nil
}

// resolve finds the broker a target's connection names, and the context a
// bridge or consumer for it runs under.
//
// That context carries the project's organization as its tenant, so what the
// bridge or consumer reads through the repositories is that organization's:
// the external tasks a bridge forwards, the subscriptions a message correlates
// to. It is deliberately not system work. A bridge running as system work
// would forward every organization's tasks of its topic to one project's
// broker.
func (r *rabbitMQRunner) resolve(ctx context.Context, target rabbitMQTarget) (context.Context, rabbitMQBroker, error) {
	organization, err := r.organizationOf(ctx, target.project)
	if err != nil {
		return nil, rabbitMQBroker{}, err
	}
	scoped := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: organization.String()})

	connection, err := r.connections.GetConnectorInstance(scoped, target.connection)
	if err != nil {
		return nil, rabbitMQBroker{}, target.connectionError(err)
	}
	broker, err := target.brokerOf(connection)
	if err != nil {
		return nil, rabbitMQBroker{}, err
	}
	broker.organization = organization

	// What the messaging service cannot know about the bridge or consumer —
	// the connection, whose work, which broker — on every line it writes. It
	// adds the project, and the topic or the queue, itself.
	messagingLogger := r.logger.With().
		Str("connection", target.connection.String()).
		Str("organization", organization.String()).
		Str("broker", broker.address).
		Logger()
	return messagingLogger.WithContext(scoped), broker, nil
}

// stop ends the retries, then stops what they started, waiting no longer than
// ctx allows: a bridge blocked dialling a broker that has gone away must not
// hold the process open.
func (r *rabbitMQRunner) stop(ctx context.Context) {
	if r.cancel == nil {
		return // never started, so nothing to stop
	}
	r.cancel()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		// The retries first, so nothing is started after StopAll has run.
		r.loops.Wait()
		r.messaging.StopAll()
	}()
	select {
	case <-stopped:
		r.logger.Info().Msg("The RabbitMQ bridges and consumers have stopped")
	case <-ctx.Done():
		r.logger.Warn().Msg("The RabbitMQ bridges and consumers did not stop within the shutdown budget; exiting without them")
	}
}

// waitOrStop waits for d, and reports false if ctx ends first.
func waitOrStop(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
