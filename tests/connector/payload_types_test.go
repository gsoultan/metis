package connector_test

import (
	"testing"
	"time"

	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// A connector's payload is built from process variables, and process variables
// come from a definition somebody uploaded. A number where a connector expected
// a string is an ordinary mapping mistake — the Discord executor turned it into
// a panic that took the worker down with it:
//
//	content = payload["text"].(string)   // panics on payload{"text": 42}
//
// Every built-in executor is exercised here with the wrong type in each field it
// reads, because "it must be a string" is an assumption none of them can make
// about input the engine did not write.
func TestAnExecutorSurvivesAPayloadOfTheWrongType(t *testing.T) {
	t.Setenv("GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	// Values a BPMN mapping produces without anybody intending anything unusual:
	// a number from an arithmetic expression, a boolean from a gateway, a list
	// from a multi-instance collection, an object from a service task's reply.
	wrongTypes := []any{42, 3.5, true, []any{"a"}, map[string]any{"k": "v"}}
	fields := []string{"content", "text", "to", "subject", "body", "message", "url", "title"}

	repo := repositories.NewRepository(testutils.SetupTestDB(t))

	for _, executor := range serviceimpl.BuiltInConnectorKeys() {
		t.Run(executor, func(t *testing.T) {
			svc := serviceimpl.NewConnectorService(repo)

			for _, field := range fields {
				for _, value := range wrongTypes {
					assertNoPanic(t, executor, field, value, func() {
						// Failing is expected — there is no server to talk to
						// and the config is empty. It must fail by returning an
						// error, not by taking the process down.
						_, _ = svc.ExecuteConnector(t.Context(), executor, nil, map[string]any{field: value})
					})
				}
			}
		})
	}
}

// assertNoPanic runs call and fails the test if it panics.
//
// A call that is merely slow is not a failure: several executors try to reach a
// broker or an SMTP server that is not there, and how long that takes is not
// what is being tested. The panic, if there is one, happens while reading the
// payload — long before anything is dialled — so a call still running after the
// grace period has already proved the point.
func assertNoPanic(t *testing.T, executor, field string, value any, call func()) {
	t.Helper()

	panicked := make(chan any, 1)
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicked <- r
			}
			close(done)
		}()
		call()
	}()

	select {
	case <-done:
		select {
		case r := <-panicked:
			t.Errorf("%s panicked on %s=%v (%T): %v", executor, field, value, value, r)
		default:
		}
	case <-time.After(2 * time.Second):
		// Still dialling something. It read the payload without panicking.
	}
}

// A port is a number. Both readers of the SMTP port asked for a string, so
// `port: 587` — the obvious way to write it in YAML or JSON — read as absent,
// and the connector answered "configuration is incomplete" to somebody who had
// configured it correctly.
func TestAPortMayBeWrittenAsANumber(t *testing.T) {
	for _, written := range []any{"587", 587, int64(587), float64(587)} {
		got := connectors.PortSetting(map[string]any{"port": written}, "port")
		if got != "587" {
			t.Errorf("port written as %v (%T) read as %q, want \"587\"", written, written, got)
		}
	}

	// Things that are not a port stay absent rather than becoming nonsense.
	for _, notAPort := range []any{nil, true, 3.5, 70000, -1, map[string]any{}} {
		if got := connectors.PortSetting(map[string]any{"port": notAPort}, "port"); got != "" {
			t.Errorf("%v (%T) read as port %q, want it treated as absent", notAPort, notAPort, got)
		}
	}
}
