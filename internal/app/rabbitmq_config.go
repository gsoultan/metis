package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/rs/zerolog/log"
)

// The RabbitMQ bridges and consumers this replica runs, as whoever runs it has
// named them.
//
// Both are off unless one of these is set. They are read from the environment
// rather than offered through the API on purpose: a bridge publishes a
// project's work to a broker outside Metis, and whether an organization's tasks
// leave the installation, and for where, is for whoever runs the servers to
// decide, not something any organization's administrator can switch on.
//
// Each is a JSON list; docs/integration.md has the shape and an example.
const (
	envRabbitMQBridges   = "METIS_RABBITMQ_BRIDGES"
	envRabbitMQConsumers = "METIS_RABBITMQ_CONSUMERS"
)

// readRabbitMQConfiguration reads both lists and reports what cannot be run.
//
// It never stops the server. An entry that cannot be read is named, by its
// position, and left out; the others still run, as one environment whose port
// is taken does not stop the rest. Nothing about a bridge is worth refusing to
// serve every process over.
func readRabbitMQConfiguration() ([]rabbitMQBridge, []rabbitMQConsumer) {
	rawBridges, rawConsumers := envvar.Get(envRabbitMQBridges), envvar.Get(envRabbitMQConsumers)
	if strings.TrimSpace(rawBridges) == "" && strings.TrimSpace(rawConsumers) == "" {
		log.Info().Msg("RabbitMQ bridges and consumers are off; set " + envRabbitMQBridges +
			" or " + envRabbitMQConsumers + " to run them")
		return nil, nil
	}

	bridges, bridgeProblems := parseRabbitMQEntries(envRabbitMQBridges, "bridge", rawBridges, parseRabbitMQBridge)
	consumers, consumerProblems := parseRabbitMQEntries(envRabbitMQConsumers, "consumer", rawConsumers, parseRabbitMQConsumer)
	if problems := errors.Join(bridgeProblems, consumerProblems); problems != nil {
		log.Error().Err(problems).
			Msg("Part of the RabbitMQ configuration cannot be used. What it names will not run; the rest will.")
	}
	return bridges, consumers
}

// parseRabbitMQEntries reads one variable's JSON list.
//
// Each entry is read on its own. One that cannot be — malformed, missing a
// setting, or naming what an earlier entry already runs — is named in the
// error by its position and left out, and the rest are returned.
func parseRabbitMQEntries[T interface{ key() string }](variable, noun, raw string, parse func(json.RawMessage) (T, error)) ([]T, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("%s is not a JSON list of %ss, so none will run: %w", variable, noun, err)
	}

	var (
		entries  []T
		problems []error
		// key → the position of the entry that runs it, so a repeat can say
		// which one it repeats.
		first = make(map[string]int, len(items))
	)
	for i, item := range items {
		position := i + 1
		entry, err := parse(item)
		if err == nil {
			if earlier, repeated := first[entry.key()]; repeated {
				err = fmt.Errorf("it repeats %s %d (%s)", noun, earlier, entry.key())
			}
		}
		if err != nil {
			problems = append(problems, fmt.Errorf("%s, %s %d: %w", variable, noun, position, err))
			continue
		}
		first[entry.key()] = position
		entries = append(entries, entry)
	}
	return entries, errors.Join(problems...)
}

// decodeStrictly decodes one entry, refusing a setting it does not know. A
// misspelt "routing_key" would otherwise be dropped without a word, and the
// bridge would publish with none.
func decodeStrictly(item json.RawMessage, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(item))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("cannot be read: %w", err)
	}
	return nil
}

// requireSetting refuses a setting that is missing or blank.
func requireSetting(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%q is required", field)
	}
	return nil
}

// parseConfiguredID reads a project or connection id.
//
// The value is not repeated in the error. It is somebody's configuration, and
// the likeliest wrong thing to find in a field meant for an id is a URL copied
// from the connection it names — password and all.
func parseConfiguredID(field, value string) (uuid.UUID, error) {
	if err := requireSetting(field, value); err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%q is not an id", field)
	}
	return id, nil
}
