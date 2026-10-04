package bpmn_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

const waiveReason = "The operations manager is on leave; the CFO agreed by email, ticket FIN-2231."

// waiver drives the in-place command as a signed-in administrator, ana.
type waiver struct {
	h   engineHarness
	svc servicecontracts.InstanceDeviator
	ctx context.Context
}

func newWaiver(h engineHarness) waiver {
	return waiver{
		h:   h,
		svc: serviceimpl.NewInstanceDeviationService(h.repo, h.engine),
		ctx: context.WithValue(h.Ctx(), pkgauth.UserContextKey,
			entities.User{Username: "ana", Roles: []string{entities.RoleAdmin}}),
	}
}

func deviationCommand(kind entities.DeviationKind, instanceID uuid.UUID, nodeID string, outputs map[string]any) entities.DeviationCommand {
	return entities.DeviationCommand{InstanceID: instanceID, Kind: kind, NodeID: nodeID, Reason: waiveReason, Outputs: outputs}
}

// preview asks for the plan; refusals are part of a plan, not an error.
func (w waiver) preview(t *testing.T, cmd entities.DeviationCommand) entities.DeviationPlan {
	t.Helper()
	cmd.DryRun = true
	out, err := w.svc.DeviateInstance(w.ctx, cmd)
	if err != nil {
		t.Fatalf("preview %s of %q: %v", cmd.Kind, cmd.NodeID, err)
	}
	if out.Applied || out.Deviation != nil {
		t.Fatalf("a preview applied: %+v", out)
	}
	return out.Plan
}

// apply previews, then applies with the visit key the preview returned.
func (w waiver) apply(t *testing.T, cmd entities.DeviationCommand) (entities.DeviationOutcome, error) {
	t.Helper()
	plan := w.preview(t, cmd)
	cmd.DryRun = false
	cmd.VisitKey = plan.VisitKey
	return w.svc.DeviateInstance(w.ctx, cmd)
}

func (w waiver) mustApply(t *testing.T, cmd entities.DeviationCommand) entities.DeviationOutcome {
	t.Helper()
	out, err := w.apply(t, cmd)
	if err != nil {
		t.Fatalf("apply %s of %q: %v", cmd.Kind, cmd.NodeID, err)
	}
	if !out.Applied || out.Deviation == nil {
		t.Fatalf("apply %s of %q answered %+v", cmd.Kind, cmd.NodeID, out)
	}
	return out
}

func (w waiver) ledger(t *testing.T, instanceID uuid.UUID) []entities.Deviation {
	t.Helper()
	rows, err := w.h.repo.Deviation().ListByInstance(w.h.Ctx(), instanceID)
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	return rows
}

// theWaive is the one waive an instance's ledger records, and fails the test
// when it records any other number of them.
func (w waiver) theWaive(t *testing.T, instanceID uuid.UUID) entities.Deviation {
	t.Helper()
	var waives []entities.Deviation
	for _, row := range w.ledger(t, instanceID) {
		if row.Kind == entities.DeviationWaive {
			waives = append(waives, row)
		}
	}
	if len(waives) != 1 {
		t.Fatalf("the ledger records %d waive(s), want exactly one", len(waives))
	}
	return waives[0]
}

// theOpenTask is the one task an instance has open on a step, and fails the
// test when it has any other number there.
func theOpenTask(t *testing.T, h engineHarness, instanceID uuid.UUID, nodeID string) entities.Task {
	t.Helper()
	open := openIterationTasks(h.Ctx(), t, h, instanceID, nodeID)
	if len(open) != 1 {
		t.Fatalf("%d task(s) are open on %s, want exactly one", len(open), nodeID)
	}
	return open[0]
}

// recordsAsProductionDoes registers the two observers that write when the
// engine raises an event on a running server: the one that keeps the audit
// trail and the one that leaves a notification. A test that asks what a
// change left behind has to have them writing, or it is asking of fewer
// tables than production writes to.
func (h engineHarness) recordsAsProductionDoes() {
	h.dispatcher.Register(observersimpl.NewAuditLogObserver(h.repo.Audit()))
	h.dispatcher.Register(observersimpl.NewNotificationObserver(serviceimpl.NewNotificationService(h.repo.Notification())))
}

// eventLog records what the engine raises.
type eventLog struct {
	mu     sync.Mutex
	events []entities.ProcessEvent
}

func (l *eventLog) OnEvent(_ context.Context, event entities.ProcessEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *eventLog) ofType(eventType string) []entities.ProcessEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []entities.ProcessEvent
	for _, event := range l.events {
		if event.Type == eventType {
			out = append(out, event)
		}
	}
	return out
}

// count is how many events the engine has raised, of any type.
func (l *eventLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.events)
}

// opsApproval is start → Operations approve (held by ollie) → Sales approve
// (an operator's to take) → end.
func opsApproval(projectID uuid.UUID, key string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     key,
		Name:    "Quotation approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "opsApprove", Type: entities.UserTask, Name: "Operations approve", Assignee: "ollie",
				Properties: testutils.FormDeclaring("approved")},
			{ID: "salesApprove", Type: entities.UserTask, Name: "Sales approve"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "q1", SourceRef: "start", TargetRef: "opsApprove"},
			{ID: "q2", SourceRef: "opsApprove", TargetRef: "salesApprove"},
			{ID: "q3", SourceRef: "salesApprove", TargetRef: "end"},
		},
	}
}

// start deploys def and starts one instance of it.
func (w waiver) start(t *testing.T, def *entities.ProcessDefinition, variables map[string]any) uuid.UUID {
	t.Helper()
	w.h.deploy(t, def)
	id, err := w.h.svc.StartProcess(w.h.Ctx(), w.h.projID, def.Key, variables)
	if err != nil {
		t.Fatalf("start %s: %v", def.Key, err)
	}
	return id
}

// everyRow is what the test's database holds: for each table of its schema,
// how many rows and a hash of all of them. A test's schema is its own, so two
// readings that are equal mean nothing was written between them — to any
// table, by any path.
func everyRow(t *testing.T, h engineHarness) map[string]string {
	t.Helper()
	var tables []string
	if err := h.db.Raw(`SELECT table_name FROM information_schema.tables
		 WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'`).Scan(&tables).Error; err != nil {
		t.Fatalf("list the tables: %v", err)
	}
	if len(tables) == 0 {
		t.Fatal("the schema lists no tables, so comparing them would prove nothing")
	}
	held := make(map[string]string, len(tables))
	for _, table := range tables {
		var rows string
		query := fmt.Sprintf(`SELECT count(*)::text || ' rows ' || coalesce(md5(string_agg(r::text, '|' ORDER BY r::text)), '')
			 FROM %q r`, table)
		if err := h.db.Raw(query).Scan(&rows).Error; err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		held[table] = rows
	}
	return held
}

// tablesThatDiffer names the tables two readings of everyRow disagree on.
func tablesThatDiffer(before, after map[string]string) []string {
	var changed []string
	for table, rows := range after {
		if before[table] != rows {
			changed = append(changed, fmt.Sprintf("%s (%s, was %s)", table, rows, before[table]))
		}
	}
	return changed
}

// said reports whether one of the sentences is exactly want.
func said(sentences []string, want string) bool {
	for _, sentence := range sentences {
		if sentence == want {
			return true
		}
	}
	return false
}

// pointOfKind is the plan's decision point of a kind at a step.
func pointOfKind(plan entities.DeviationPlan, nodeID string, kind entities.DecisionPointKind) (entities.DecisionPoint, bool) {
	for _, point := range plan.DecisionPoints {
		if point.NodeID == nodeID && point.Kind == kind {
			return point, true
		}
	}
	return entities.DecisionPoint{}, false
}

// lines sets sentences out one to a line, for a failure somebody has to read.
func lines(sentences []string) string {
	if len(sentences) == 0 {
		return "(none)"
	}
	return "\n  " + strings.Join(sentences, "\n  ")
}

// lockWait is how long a request sent to wait for a held row is given to get
// there, and to finish once the row is let go. Far longer than either takes;
// it only bounds a test that has gone wrong.
const lockWait = 20 * time.Second

// heldRows is rows held by somebody who is part-way through something: a
// transaction of the test's own, standing in for a request that has not
// finished. What is sent while it is open waits for it, which is how a test
// puts two requests in the order it is about, every time.
type heldRows struct {
	tx      *gorm.DB
	session int
	ended   bool
}

// holding opens a transaction, runs one statement in it and leaves it open.
// The statement has to touch at least one row: a test that held nothing would
// make nobody wait and prove nothing.
func (h engineHarness) holding(t *testing.T, statement string, args ...any) *heldRows {
	t.Helper()
	tx := h.db.Begin()
	if tx.Error != nil {
		t.Fatalf("open the transaction that holds the rows: %v", tx.Error)
	}
	held := &heldRows{tx: tx}
	t.Cleanup(func() {
		if !held.ended {
			tx.Rollback()
		}
	})
	if err := tx.Raw("SELECT pg_backend_pid()").Scan(&held.session).Error; err != nil {
		t.Fatalf("read the holder's session: %v", err)
	}
	result := tx.Exec(statement, args...)
	if result.Error != nil || result.RowsAffected == 0 {
		t.Fatalf("hold the rows: %d row(s), %v", result.RowsAffected, result.Error)
	}
	return held
}

// letGo ends the holder's transaction: kept, or undone as though it had never
// been.
func (r *heldRows) letGo(t *testing.T, keep bool) {
	t.Helper()
	r.ended = true
	end := r.tx.Rollback
	if keep {
		end = r.tx.Commit
	}
	if err := end().Error; err != nil {
		t.Fatalf("let the rows go: %v", err)
	}
}

// sent is a request made on a goroutine of its own, and what it answered.
type sent[T any] struct {
	done  chan struct{}
	value T
	err   error
}

// send makes a request without waiting for its answer.
func send[T any](call func() (T, error)) *sent[T] {
	s := &sent[T]{done: make(chan struct{})}
	go func() {
		defer close(s.done)
		s.value, s.err = call()
	}()
	return s
}

// answered reports whether the request has finished.
func (s *sent[T]) answered() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// answer waits for the request to finish, for no longer than lockWait.
func (s *sent[T]) answer(t *testing.T, what string) (T, error) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(lockWait):
		t.Fatalf("%s did not finish", what)
	}
	return s.value, s.err
}

// waitForWaiters waits until count requests are waiting for what held holds:
// for it, or in a line behind one that is. A request that finishes instead
// never waited, and the test has not made the order it is about.
//
// It looks at what the database says is waiting, not at a clock, so what
// follows it does not depend on how long anything took.
func (h engineHarness) waitForWaiters(t *testing.T, held *heldRows, count int, finished ...func() bool) {
	t.Helper()
	const waiting = `WITH RECURSIVE behind(pid) AS (
		    SELECT pid FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid))
		  UNION
		    SELECT a.pid FROM pg_stat_activity a JOIN behind b ON b.pid = ANY(pg_blocking_pids(a.pid)))
		SELECT count(*) FROM behind`
	seen := 0
	for deadline := time.Now().Add(lockWait); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		for i, done := range finished {
			if done() {
				t.Fatalf("request %d finished without waiting for the rows it was sent to wait for", i+1)
			}
		}
		if err := h.db.Raw(waiting, held.session).Scan(&seen).Error; err != nil {
			t.Fatalf("look for what is waiting for the held rows: %v", err)
		}
		if seen == count {
			return
		}
	}
	t.Fatalf("%d request(s) are waiting for the held rows, want %d", seen, count)
}
