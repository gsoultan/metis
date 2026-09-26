package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func bridgeJSON(project, connection uuid.UUID, topic, exchange, routingKey string) string {
	return fmt.Sprintf(`{"project":%q,"connection":%q,"topic":%q,"exchange":%q,"routing_key":%q}`,
		project, connection, topic, exchange, routingKey)
}

// bridgeJSONWithLock is bridgeJSON with "lock_seconds" set to lock, written
// as raw JSON.
func bridgeJSONWithLock(project, connection uuid.UUID, lock string) string {
	return fmt.Sprintf(`{"project":%q,"connection":%q,"topic":"reverse-charge","exchange":"billing","routing_key":"charges.reverse","lock_seconds":%s}`,
		project, connection, lock)
}

func consumerJSON(project, connection uuid.UUID, queue, message string) string {
	return fmt.Sprintf(`{"project":%q,"connection":%q,"queue":%q,"message":%q}`, project, connection, queue, message)
}

// assertProblems checks that err names every problem it should, and nothing
// when there are none.
func assertProblems(t *testing.T, err error, want []string) {
	t.Helper()
	if len(want) == 0 {
		if err != nil {
			t.Fatalf("a configuration with nothing wrong was refused: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("a configuration with something wrong was accepted; want an error saying %q", want)
	}
	for _, words := range want {
		if !strings.Contains(err.Error(), words) {
			t.Errorf("the error does not say %q:\n%v", words, err)
		}
	}
}

func TestRabbitMQBridgesAreReadFromTheirVariable(t *testing.T) {
	project, connection := uuid.New(), uuid.New()
	reverse := bridgeJSON(project, connection, "reverse-charge", "billing", "charges.reverse")
	// The default exchange, which routes by routing key alone.
	ship := bridgeJSON(project, connection, "ship", "", "shipments")

	tests := []struct {
		name   string
		raw    string
		topics []string
		errors []string
	}{
		{name: "unset is off", raw: ""},
		{name: "blank is off", raw: " \n\t "},
		{name: "an empty list is off", raw: "[]"},
		{name: "two bridges", raw: "[" + reverse + "," + ship + "]", topics: []string{"reverse-charge", "ship"}},
		{
			name:   "not JSON at all",
			raw:    "project=" + project.String(),
			errors: []string{"METIS_RABBITMQ_BRIDGES is not a JSON list of bridges, so none will run"},
		},
		{
			name:   "one bridge, not a list of them",
			raw:    reverse,
			errors: []string{"METIS_RABBITMQ_BRIDGES is not a JSON list of bridges"},
		},
		{
			name:   "a misspelt setting leaves that bridge out and runs the other",
			raw:    "[" + reverse + `,{"project":"` + project.String() + `","connection":"` + connection.String() + `","topic":"ship","routing_ky":"shipments"}]`,
			topics: []string{"reverse-charge"},
			errors: []string{"METIS_RABBITMQ_BRIDGES, bridge 2", `unknown field "routing_ky"`},
		},
		{
			name:   "no topic",
			raw:    "[" + bridgeJSON(project, connection, " ", "billing", "charges.reverse") + "]",
			errors: []string{`METIS_RABBITMQ_BRIDGES, bridge 1: "topic" is required`},
		},
		{
			name:   "a project that is not an id",
			raw:    `[{"project":"orders","connection":"` + connection.String() + `","topic":"t","exchange":"x"}]`,
			errors: []string{`bridge 1: "project" is not an id`},
		},
		{
			name:   "no connection",
			raw:    `[{"project":"` + project.String() + `","topic":"t","exchange":"x"}]`,
			errors: []string{`bridge 1: "connection" is required`},
		},
		{
			name:   "nowhere for a task to go",
			raw:    "[" + bridgeJSON(project, connection, "t", "", "") + "]",
			errors: []string{`bridge 1: "exchange" and "routing_key" are both empty`},
		},
		{
			name:   "the same project and topic twice",
			raw:    "[" + reverse + "," + bridgeJSON(project, connection, "reverse-charge", "other", "other") + "]",
			topics: []string{"reverse-charge"},
			errors: []string{"bridge 2: it repeats bridge 1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bridges, err := parseRabbitMQEntries(envRabbitMQBridges, "bridge", tc.raw, parseRabbitMQBridge)
			assertProblems(t, err, tc.errors)
			var topics []string
			for _, bridge := range bridges {
				topics = append(topics, bridge.topic)
			}
			if fmt.Sprint(topics) != fmt.Sprint(tc.topics) {
				t.Errorf("bridges %v would run, want %v", topics, tc.topics)
			}
		})
	}
}

func TestARabbitMQBridgeKeepsEverySettingAsWritten(t *testing.T) {
	project, connection := uuid.New(), uuid.New()
	bridges, err := parseRabbitMQEntries(envRabbitMQBridges, "bridge",
		"["+bridgeJSONWithLock(project, connection, "600")+"]", parseRabbitMQBridge)
	if err != nil || len(bridges) != 1 {
		t.Fatalf("read %d bridges, %v", len(bridges), err)
	}
	want := rabbitMQBridge{
		target:     rabbitMQTarget{project: project, connection: connection},
		topic:      "reverse-charge",
		exchange:   "billing",
		routingKey: "charges.reverse",
		lock:       10 * time.Minute,
	}
	if bridges[0] != want {
		t.Fatalf("read %+v, want %+v", bridges[0], want)
	}
}

// How long a bridge locks each task is the downstream worker's whole budget,
// its time on the queue included. It was a fixed 30 seconds, so a queue that
// backed up had the same tasks published again. It is set per bridge now,
// and read as strictly as the rest of the entry: a lock the bridge could not
// keep to is refused rather than rounded.
func TestARabbitMQBridgesLockIsReadFromLockSeconds(t *testing.T) {
	project, connection := uuid.New(), uuid.New()
	tests := []struct {
		name  string
		entry string
		lock  time.Duration
		error string
	}{
		{name: "not set is five minutes", entry: bridgeJSON(project, connection, "reverse-charge", "billing", "charges.reverse"), lock: 5 * time.Minute},
		{name: "ten minutes", entry: bridgeJSONWithLock(project, connection, "600"), lock: 10 * time.Minute},
		{name: "the shortest", entry: bridgeJSONWithLock(project, connection, "30"), lock: 30 * time.Second},
		{name: "the longest", entry: bridgeJSONWithLock(project, connection, "86400"), lock: 24 * time.Hour},
		{name: "too short", entry: bridgeJSONWithLock(project, connection, "29"), error: `"lock_seconds" is 29; it must be from 30 to 86400`},
		{name: "zero", entry: bridgeJSONWithLock(project, connection, "0"), error: `"lock_seconds" is 0; it must be from 30 to 86400`},
		{name: "negative", entry: bridgeJSONWithLock(project, connection, "-300"), error: `"lock_seconds" is -300; it must be from 30 to 86400`},
		{name: "longer than a day", entry: bridgeJSONWithLock(project, connection, "86401"), error: `"lock_seconds" is 86401; it must be from 30 to 86400`},
		{name: "not whole seconds", entry: bridgeJSONWithLock(project, connection, "300.5"), error: "cannot be read"},
		{name: "a string", entry: bridgeJSONWithLock(project, connection, `"300"`), error: "cannot be read"},
		{
			name:  "misspelt",
			entry: strings.Replace(bridgeJSONWithLock(project, connection, "600"), "lock_seconds", "lock_second", 1),
			error: `cannot be read: json: unknown field "lock_second"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bridges, err := parseRabbitMQEntries(envRabbitMQBridges, "bridge", "["+tc.entry+"]", parseRabbitMQBridge)
			if tc.error != "" {
				assertProblems(t, err, []string{"bridge 1: " + tc.error})
				if len(bridges) != 0 {
					t.Errorf("a bridge with that lock would run: %+v", bridges)
				}
				return
			}
			if err != nil || len(bridges) != 1 {
				t.Fatalf("read %d bridges, %v", len(bridges), err)
			}
			if bridges[0].lock != tc.lock {
				t.Errorf("the bridge locks each task for %v, want %v", bridges[0].lock, tc.lock)
			}
		})
	}
}

func TestRabbitMQConsumersAreReadFromTheirVariable(t *testing.T) {
	project, connection := uuid.New(), uuid.New()
	payments := consumerJSON(project, connection, "payments", "PaymentReceived")

	tests := []struct {
		name   string
		raw    string
		queues []string
		errors []string
	}{
		{name: "unset is off", raw: ""},
		{name: "one consumer", raw: "[" + payments + "]", queues: []string{"payments"}},
		{
			name:   "no queue",
			raw:    "[" + consumerJSON(project, connection, "", "PaymentReceived") + "]",
			errors: []string{`METIS_RABBITMQ_CONSUMERS, consumer 1: "queue" is required`},
		},
		{
			name:   "no message",
			raw:    "[" + payments + "," + consumerJSON(project, connection, "refunds", "") + "]",
			queues: []string{"payments"},
			errors: []string{`consumer 2: "message" is required`},
		},
		{
			name:   "the same project and queue twice",
			raw:    "[" + payments + "," + consumerJSON(project, connection, "payments", "Other") + "]",
			queues: []string{"payments"},
			errors: []string{"consumer 2: it repeats consumer 1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			consumers, err := parseRabbitMQEntries(envRabbitMQConsumers, "consumer", tc.raw, parseRabbitMQConsumer)
			assertProblems(t, err, tc.errors)
			var queues []string
			for _, consumer := range consumers {
				queues = append(queues, consumer.queue)
			}
			if fmt.Sprint(queues) != fmt.Sprint(tc.queues) {
				t.Errorf("consumers %v would run, want %v", queues, tc.queues)
			}
		})
	}
}

// The likeliest thing to find in a field meant for an id is the URL of the
// connection it should have named, and the error is logged.
func TestAMisplacedBrokerURLIsNotRepeatedInTheError(t *testing.T) {
	const password = "s3cret-broker-password"
	raw := `[{"project":"` + uuid.NewString() + `","connection":"amqp://metis:` + password +
		`@broker.internal:5672/","queue":"payments","message":"PaymentReceived"}]`

	_, err := parseRabbitMQEntries(envRabbitMQConsumers, "consumer", raw, parseRabbitMQConsumer)
	assertProblems(t, err, []string{`consumer 1: "connection" is not an id`})
	if strings.Contains(err.Error(), password) {
		t.Fatalf("the error repeats the broker password: %v", err)
	}
}

// A mistake in one variable does not stop what the other names.
func TestAMalformedBridgeListDoesNotStopTheConsumers(t *testing.T) {
	project, connection := uuid.New(), uuid.New()
	t.Setenv(envRabbitMQBridges, "{not json")
	t.Setenv(envRabbitMQConsumers, "["+consumerJSON(project, connection, "payments", "PaymentReceived")+"]")

	bridges, consumers := readRabbitMQConfiguration()
	if len(bridges) != 0 {
		t.Errorf("%d bridges would run from a list that cannot be read", len(bridges))
	}
	if len(consumers) != 1 {
		t.Fatalf("%d consumers would run, want the one configured", len(consumers))
	}
}
