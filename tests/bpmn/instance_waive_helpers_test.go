package bpmn_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/tests/testutils"
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
