package app

import (
	"encoding/json"

	"github.com/rs/zerolog"
)

// rabbitMQConsumer is one inbound consumer an operator asked for: a queue on
// the broker a project's connection names, each message on it correlated as a
// BPMN message of that project.
type rabbitMQConsumer struct {
	target  rabbitMQTarget
	queue   string
	message string
}

// parseRabbitMQConsumer reads one entry of METIS_RABBITMQ_CONSUMERS.
func parseRabbitMQConsumer(item json.RawMessage) (rabbitMQConsumer, error) {
	var written struct {
		Project    string `json:"project"`
		Connection string `json:"connection"`
		Queue      string `json:"queue"`
		Message    string `json:"message"`
	}
	if err := decodeStrictly(item, &written); err != nil {
		return rabbitMQConsumer{}, err
	}
	target, err := parseRabbitMQTarget(written.Project, written.Connection)
	if err != nil {
		return rabbitMQConsumer{}, err
	}
	if err := requireSetting("queue", written.Queue); err != nil {
		return rabbitMQConsumer{}, err
	}
	if err := requireSetting("message", written.Message); err != nil {
		return rabbitMQConsumer{}, err
	}
	return rabbitMQConsumer{target: target, queue: written.Queue, message: written.Message}, nil
}

// key is what the messaging service allows once: one consumer per project and
// queue.
func (c rabbitMQConsumer) key() string {
	return "project " + c.target.project.String() + ", queue " + c.queue
}

// logger returns base with this consumer's settings on it, which are what
// somebody reading the log needs to find it in METIS_RABBITMQ_CONSUMERS.
func (c rabbitMQConsumer) logger(base *zerolog.Logger) zerolog.Logger {
	return base.With().
		Str("project", c.target.project.String()).
		Str("connection", c.target.connection.String()).
		Str("queue", c.queue).
		Str("messageName", c.message).
		Logger()
}
