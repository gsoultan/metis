package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/interceptors/security"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// Process variables are encrypted at rest on the instance, and the README says
// so. The engine keeps copies of them — the audit trail, a queued job's
// payload, an external task's variables, the variable history — and those were
// plain jsonb: every value the instance column protected could be read from the
// tables beside it.

const sealedMarker = "123-45-6789-sealed-marker"

func TestCopiesOfProcessVariablesAreEncryptedAtRest(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	// The audit observer as the application registers it, so the trail is
	// written the way it is in production.
	dispatcher := observersimpl.NewEventDispatcher()
	dispatcher.Register(observersimpl.NewAuditLogObserver(repo.Audit()))
	svc := services.NewServiceFacade(repo, dispatcher, observersimpl.NewSSEObserver(),
		"sealed-copies-secret", nil, nil, nil)
	ctx, _, projectID := testutils.ScopedProject(t, repo)

	deploy := func(key string, waiting *entities.Node) uuid.UUID {
		t.Helper()
		def := entities.ProcessDefinition{
			Project: &entities.Project{ID: projectID},
			Key:     key,
			Nodes: []*entities.Node{
				{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
				waiting,
				{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
			},
			Flows: []*entities.SequenceFlow{
				{ID: "f1", SourceRef: "start", TargetRef: waiting.ID},
				{ID: "f2", SourceRef: waiting.ID, TargetRef: "end"},
			},
		}
		if _, err := svc.CreateDefinition(ctx, &def); err != nil {
			t.Fatalf("deploy %s: %v", key, err)
		}
		id, err := svc.StartProcess(ctx, projectID, key, map[string]any{"applicantSSN": sealedMarker})
		if err != nil {
			t.Fatalf("start %s: %v", key, err)
		}
		return id
	}
	// Parked on a timer: a job whose payload is the instance's variables.
	timerInstance := deploy("sealed-timer", &entities.Node{ID: "wait", Type: entities.IntermediateCatchEvent,
		Incoming: []string{"f1"}, Outgoing: []string{"f2"}, Properties: map[string]any{"timer_duration": "PT1H"}})
	// Parked on an external worker: a task carrying the variables it is given.
	deploy("sealed-external", &entities.Node{ID: "work", Type: entities.ServiceTask, ExternalTopic: "sealed-topic",
		Incoming: []string{"f1"}, Outgoing: []string{"f2"}})

	for _, column := range []struct {
		table, column string
		mustHaveRows  bool
	}{
		{"jobs", "payload", true},
		{"external_tasks", "variables", true},
		{"audit_logs", "data", true},
		{"variable_snapshots", "variables", false},
	} {
		var rows, plaintext int64
		if err := db.WithContext(context.Background()).
			Raw("SELECT count(*), count(*) FILTER (WHERE "+column.column+"::text LIKE ?) FROM "+column.table,
				"%"+sealedMarker+"%").
			Row().Scan(&rows, &plaintext); err != nil {
			t.Fatalf("read %s: %v", column.table, err)
		}
		if column.mustHaveRows && rows == 0 {
			t.Errorf("%s has no rows, so this proves nothing about it", column.table)
		}
		if plaintext > 0 {
			t.Errorf("%s.%s holds the variable in plaintext in %d of %d rows", column.table, column.column, plaintext, rows)
		}
	}

	// And the engine still reads its own copies.
	tasks, err := svc.FetchAndLock(ctx, "sealed-topic", "worker", 1, 60_000)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("fetch the external task: tasks=%d err=%v", len(tasks), err)
	}
	if got, _ := tasks[0].Variables["applicantSSN"].(string); got != sealedMarker {
		t.Fatalf("the external task's variables did not read back: %v", tasks[0].Variables)
	}
	jobs, err := repo.Job().ListByInstance(ctx, timerInstance)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("read the timer's job: jobs=%d err=%v", len(jobs), err)
	}
	if got, _ := jobs[0].Payload["applicantSSN"].(string); got != sealedMarker {
		t.Fatalf("the job's payload did not read back: %v", jobs[0].Payload)
	}
	entries, err := repo.Audit().ListByInstance(ctx, timerInstance)
	if err != nil {
		t.Fatalf("read the audit trail: %v", err)
	}
	for _, entry := range entries {
		if got, _ := entry.Data["applicantSSN"].(string); got == sealedMarker {
			return
		}
	}
	t.Fatalf("no audit entry read back the variable: %d entries", len(entries))
}

// The answers the engine keeps are the same data by another route: a
// partner's response becomes variables through the step's output mapping, an
// event broadcast to another replica carries the variables of the process it
// announces, and the answer kept for a client's retry is often the instance it
// created.
func TestStoredAnswersAreEncryptedAtRest(t *testing.T) {
	db := testutils.SetupTestDB(t)
	conn := testutils.StormConn(db)
	repo := repositories.NewRepository(conn)
	ctx := entities.WithSystemContext(t.Context())

	instanceID := uuid.New()
	visit := models.UUID(uuid.New())
	call, err := repo.ServiceCall().Begin(ctx, models.ServiceCallModel{
		InstanceID: models.UUID(instanceID), NodeID: "charge", JobID: &visit,
	}, time.Now())
	if err != nil {
		t.Fatalf("record the call: %v", err)
	}
	if err := repo.ServiceCall().Complete(ctx, uuid.UUID(call.ID), map[string]any{"cardholder": sealedMarker}); err != nil {
		t.Fatalf("record the response: %v", err)
	}

	payload := `{"type":"ProcessStarted","variables":{"applicantSSN":"` + sealedMarker + `"}}`
	if err := repo.Broadcast().Publish(ctx, "replica-a", entities.SSEScope{Organization: uuid.New()}, payload); err != nil {
		t.Fatalf("publish: %v", err)
	}

	store := security.NewDBIdempotencyStore(conn, time.Hour)
	if _, err := store.Claim(ctx, "retry-key", "request-hash"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	answer := []byte(`{"instance":{"variables":{"applicantSSN":"` + sealedMarker + `"}}}`)
	if err := store.Complete(ctx, "retry-key", security.StoredResponse{StatusCode: 201, Body: answer}); err != nil {
		t.Fatalf("keep the answer: %v", err)
	}

	for _, probe := range []struct{ table, expr string }{
		{"service_calls", "response::text"},
		{"broadcast_events", "payload"},
		{"idempotency_records", "convert_from(body, 'UTF8')"},
	} {
		var rows, plaintext int64
		if err := db.WithContext(context.Background()).
			Raw("SELECT count(*), count(*) FILTER (WHERE "+probe.expr+" LIKE ?) FROM "+probe.table, "%"+sealedMarker+"%").
			Row().Scan(&rows, &plaintext); err != nil {
			t.Fatalf("read %s: %v", probe.table, err)
		}
		if rows == 0 {
			t.Errorf("%s has no rows, so this proves nothing about it", probe.table)
		}
		if plaintext > 0 {
			t.Errorf("%s holds the value in plaintext in %d of %d rows", probe.table, plaintext, rows)
		}
	}

	// Each still reads back as it was stored.
	stored, err := repo.ServiceCall().Get(ctx, instanceID, "charge", "")
	if err != nil || stored.Response["cardholder"] != sealedMarker {
		t.Fatalf("the response did not read back: %v %v", stored.Response, err)
	}
	events, err := repo.Broadcast().Since(ctx, "replica-b", 0, 10)
	if err != nil || len(events) != 1 || events[0].Payload != payload {
		t.Fatalf("the broadcast did not read back: %+v %v", events, err)
	}
	replay, err := store.Await(ctx, "retry-key")
	if err != nil || replay == nil || string(replay.Body) != string(answer) {
		t.Fatalf("the kept answer did not read back: %+v %v", replay, err)
	}
}
