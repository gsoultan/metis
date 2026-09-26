package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// How long a bridge locks each task it publishes: "lock_seconds".
//
// The lock is the downstream worker's whole budget — the time the message
// waits on the queue as well as the time the work takes — because the
// external-task API has no way for a worker to extend it. A task still open
// when it runs out is published again. It was a fixed 30 seconds, so a queue
// that backed up for longer than that had its tasks published a second time,
// and a third.
//
// The default is long enough for a queue; the bounds refuse a lock that has
// run out before the bridge's next round, or one that hides a task whose
// message was lost for longer than a day.
const (
	defaultRabbitMQBridgeLock    = 5 * time.Minute
	minRabbitMQBridgeLockSeconds = 30
	maxRabbitMQBridgeLockSeconds = 24 * 60 * 60
)

// rabbitMQBridge is one external-task bridge an operator asked for: the tasks
// of a topic, published to an exchange on the broker a project's connection
// names.
type rabbitMQBridge struct {
	target     rabbitMQTarget
	topic      string
	exchange   string
	routingKey string
	// lock is how long each task the bridge publishes stays locked to it.
	lock time.Duration
}

// parseRabbitMQBridge reads one entry of METIS_RABBITMQ_BRIDGES.
func parseRabbitMQBridge(item json.RawMessage) (rabbitMQBridge, error) {
	var written struct {
		Project     string `json:"project"`
		Connection  string `json:"connection"`
		Topic       string `json:"topic"`
		Exchange    string `json:"exchange"`
		RoutingKey  string `json:"routing_key"`
		LockSeconds *int64 `json:"lock_seconds"`
	}
	if err := decodeStrictly(item, &written); err != nil {
		return rabbitMQBridge{}, err
	}
	lock, err := parseRabbitMQBridgeLock(written.LockSeconds)
	if err != nil {
		return rabbitMQBridge{}, err
	}
	target, err := parseRabbitMQTarget(written.Project, written.Connection)
	if err != nil {
		return rabbitMQBridge{}, err
	}
	if err := requireSetting("topic", written.Topic); err != nil {
		return rabbitMQBridge{}, err
	}
	// The default exchange routes by routing key alone, so with neither there
	// is nowhere for a task to go. The broker would return every one, and the
	// bridge would hand every one back, forever.
	if strings.TrimSpace(written.Exchange) == "" && strings.TrimSpace(written.RoutingKey) == "" {
		return rabbitMQBridge{}, errors.New(`"exchange" and "routing_key" are both empty, so the tasks would reach no queue`)
	}
	return rabbitMQBridge{
		target:     target,
		topic:      written.Topic,
		exchange:   written.Exchange,
		routingKey: written.RoutingKey,
		lock:       lock,
	}, nil
}

// parseRabbitMQBridgeLock reads "lock_seconds": a whole number of seconds from
// 30 to a day, or nothing for the default.
func parseRabbitMQBridgeLock(seconds *int64) (time.Duration, error) {
	if seconds == nil {
		return defaultRabbitMQBridgeLock, nil
	}
	if *seconds < minRabbitMQBridgeLockSeconds || *seconds > maxRabbitMQBridgeLockSeconds {
		return 0, fmt.Errorf(`"lock_seconds" is %d; it must be from %d to %d, and cover a task's time on the queue as well as the worker's`,
			*seconds, minRabbitMQBridgeLockSeconds, maxRabbitMQBridgeLockSeconds)
	}
	return time.Duration(*seconds) * time.Second, nil
}

// key is what the messaging service allows once: one bridge per project and
// topic.
func (b rabbitMQBridge) key() string {
	return "project " + b.target.project.String() + ", topic " + b.topic
}

// logger returns base with this bridge's settings on it, which are what
// somebody reading the log needs to find it in METIS_RABBITMQ_BRIDGES.
func (b rabbitMQBridge) logger(base *zerolog.Logger) zerolog.Logger {
	return base.With().
		Str("project", b.target.project.String()).
		Str("connection", b.target.connection.String()).
		Str("topic", b.topic).
		Str("exchange", b.exchange).
		Str("routingKey", b.routingKey).
		Logger()
}
