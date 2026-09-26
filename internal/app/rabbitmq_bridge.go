package app

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/rs/zerolog"
)

// rabbitMQBridge is one external-task bridge an operator asked for: the tasks
// of a topic, published to an exchange on the broker a project's connection
// names.
type rabbitMQBridge struct {
	target     rabbitMQTarget
	topic      string
	exchange   string
	routingKey string
}

// parseRabbitMQBridge reads one entry of METIS_RABBITMQ_BRIDGES.
func parseRabbitMQBridge(item json.RawMessage) (rabbitMQBridge, error) {
	var written struct {
		Project    string `json:"project"`
		Connection string `json:"connection"`
		Topic      string `json:"topic"`
		Exchange   string `json:"exchange"`
		RoutingKey string `json:"routing_key"`
	}
	if err := decodeStrictly(item, &written); err != nil {
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
	}, nil
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
