package impl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

// handBackTimeout bounds handing a task back. It is not cut short by the
// bridge stopping: a task fetched just before the server stops would
// otherwise stay locked for the whole of the bridge's lock.
const handBackTimeout = 5 * time.Second

// What a bridge says about a problem, once per cause: see problemLog.
const (
	msgBridgeCouldNotConnect = "A RabbitMQ bridge could not connect to its broker"
	msgBridgeLostLink        = "The broker closed a RabbitMQ bridge's channel or connection; the bridge opens a new one at its next round"
	msgBridgeTaskRefused     = "A task was not accepted by the broker and was handed back"
)

// externalTaskBridge publishes the external tasks of one topic to a broker,
// each one confirmed before it counts as forwarded, and hands back every one
// the broker did not take.
type externalTaskBridge struct {
	tasks          contracts.ExternalTaskService
	link           *brokerLink
	publisher      *confirmingPublisher
	topic          string
	exchange       string
	routingKey     string
	lockDuration   time.Duration
	pollInterval   time.Duration
	confirmTimeout time.Duration
	// reconnect is how long to wait after each failed attempt to reach the
	// broker; failedAttempts counts them since it was last reached.
	reconnect      backoff
	failedAttempts int
	sleep          func(ctx context.Context, delay time.Duration) error
	logger         *zerolog.Logger
	problems       problemLog
}

// run polls until ctx ends.
func (b *externalTaskBridge) run(ctx context.Context) {
	defer b.link.close()
	wait := b.pollInterval
	for {
		if err := b.sleep(ctx, wait); err != nil {
			return
		}
		wait = b.poll(ctx)
	}
}

// poll is one round: reach the broker, then forward what is waiting. It
// returns how long to wait before the next round: the poll interval, or while
// the broker cannot be reached, a wait that grows with each failed attempt.
func (b *externalTaskBridge) poll(ctx context.Context) time.Duration {
	if err := b.connect(); err != nil {
		b.failedAttempts++
		wait := b.reconnect.delay(b.failedAttempts)
		b.problems.event(msgBridgeCouldNotConnect, err).Dur("retryIn", wait).
			Msg(msgBridgeCouldNotConnect)
		return wait
	}
	b.failedAttempts = 0
	// The repository reads the lock in milliseconds. It was once passed 30
	// meaning seconds, and every lock ran out after thirty milliseconds.
	tasks, err := b.tasks.FetchAndLock(ctx, b.topic, workerID, maxTasks, b.lockDuration.Milliseconds())
	if err != nil {
		b.logger.Error().Err(err).Msg("Bridge fetch error")
		return b.pollInterval
	}
	b.forward(ctx, tasks)
	return b.pollInterval
}

// connect makes sure the bridge has a channel in confirm mode to publish on,
// opening a new one in place of one the broker closed.
func (b *externalTaskBridge) connect() error {
	if lost := b.link.lost(); lost != nil {
		b.problems.event(msgBridgeLostLink, lost).Msg(msgBridgeLostLink)
	}
	ch, change, err := b.link.open()
	if err != nil {
		return err
	}
	if change == linkUnchanged && b.publisher != nil {
		return nil
	}
	// Once per channel: NotifyReturn appends a listener each call, and the
	// bridge publishes on a channel for as long as it stays open.
	publisher, err := newConfirmingPublisher(ch, b.confirmTimeout)
	if err != nil {
		b.link.dropChannel()
		return err
	}
	b.publisher = publisher
	if change == linkReconnected {
		// Said on every connection, the first and each one after a loss, so
		// the log shows when forwarding resumed and not only when it stopped.
		b.problems.working()
		b.logger.Info().Msg("A RabbitMQ bridge connected to its broker")
	}
	return nil
}

// forward publishes each task, and hands back the ones the broker did not
// take.
func (b *externalTaskBridge) forward(ctx context.Context, tasks []*entities.ExternalTask) {
	for i, task := range tasks {
		if ctx.Err() != nil {
			// Stopping: what was fetched and not published goes back now,
			// rather than when the bridge's lock runs out.
			b.releaseUnpublished(ctx, tasks[i:], fmt.Errorf("not published before the bridge stopped: %w", ctx.Err()))
			return
		}
		err := b.forwardOne(ctx, task)
		if err == nil {
			continue
		}
		cause := b.channelSpoilt(err)
		if cause == nil {
			continue
		}
		// The rest of this round goes back unpublished, and the next round
		// publishes on a new channel.
		b.releaseUnpublished(ctx, tasks[i+1:],
			fmt.Errorf("not published, because a task before it in the same round failed: %w", cause))
		b.link.dropChannel()
		b.publisher = nil
		return
	}
}

// channelSpoilt reports why the channel a publish failed on cannot be used
// for the next one, or nil when it can.
//
// The broker may have closed it — then nothing more can be published on it.
// Or it may have missed a confirm: then the broker's answer about the lost
// message may still come, and be read as its answer about another.
func (b *externalTaskBridge) channelSpoilt(publishErr error) error {
	if lost := b.link.lost(); lost != nil {
		b.problems.event(msgBridgeLostLink, lost).Msg(msgBridgeLostLink)
		return lost
	}
	if errors.Is(publishErr, errConfirmTimeout) {
		return publishErr
	}
	return nil
}

// forwardOne publishes one task, and hands it back when the broker did not
// take it.
func (b *externalTaskBridge) forwardOne(ctx context.Context, task *entities.ExternalTask) error {
	body, err := json.Marshal(task)
	if err != nil {
		// Publishing "null" onto a work queue hands a worker a message it
		// cannot act on and loses the task.
		b.logger.Error().Err(err).Str("taskID", task.ID.String()).
			Msg("A task could not be encoded and was not published")
		b.releaseUnforwardedTask(ctx, task, err)
		return err
	}
	err = b.publisher.publish(ctx, b.exchange, b.routingKey, amqp.Publishing{
		ContentType: "application/json",
		Body:        body,
		Headers: amqp.Table{
			"task_id": task.ID.String(),
		},
	})
	if err != nil {
		// The task is locked to this bridge. Leaving it that way meant the
		// work stalled until the lock expired while the line below claimed it
		// had been forwarded — so hand it back now, and let the retry the
		// engine already has do its job.
		if ctx.Err() != nil {
			b.logger.Info().Str("taskID", task.ID.String()).
				Msg("A task being published when the bridge stopped was handed back")
		} else {
			b.problems.event(msgBridgeTaskRefused, err).Str("taskID", task.ID.String()).
				Msg(msgBridgeTaskRefused)
		}
		b.releaseUnforwardedTask(ctx, task, err)
		return err
	}
	b.problems.working()
	b.logger.Info().Str("taskID", task.ID.String()).Msg("Forwarded external task to RabbitMQ")
	return nil
}

// releaseUnpublished hands back tasks the bridge fetched and did not try to
// publish, saying why.
func (b *externalTaskBridge) releaseUnpublished(ctx context.Context, tasks []*entities.ExternalTask, why error) {
	if len(tasks) == 0 {
		return
	}
	for _, task := range tasks {
		b.releaseUnforwardedTask(ctx, task, why)
	}
	b.logger.Warn().Err(why).Int("tasks", len(tasks)).
		Msg("Tasks the bridge fetched and did not publish were handed back")
}

// releaseUnforwardedTask hands a locked task back when the bridge could not
// put it on the broker.
//
// FetchAndLock makes a task invisible to other workers for as long as the
// bridge's lock. A bridge that fetched a task and then failed to publish it
// used to simply move on, so the work sat idle for the whole lock while the
// only record was a log line saying it had been forwarded. Failing it here
// makes the engine's own retry the thing that decides what happens next,
// which is what it is for.
//
// It is done on a context the bridge's stopping does not end: on the stopping
// one the database refused it, and the task stayed locked for the whole lock.
func (b *externalTaskBridge) releaseUnforwardedTask(ctx context.Context, task *entities.ExternalTask, cause error) {
	if b.tasks == nil || task == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), handBackTimeout)
	defer cancel()
	// Retries are left where the task already had them: this is a transport
	// failure, not the worker rejecting the work, so it should not consume an
	// attempt the business logic is entitled to.
	err := b.tasks.HandleFailure(ctx, task.ID, workerID,
		"the bridge could not publish this task to the broker",
		cause.Error(), task.Retries, 0)
	if err != nil {
		b.logger.Error().Err(err).Str("taskID", task.ID.String()).
			Msg("A task that was not forwarded could not be handed back either; it stays locked until its lock expires")
	}
}
