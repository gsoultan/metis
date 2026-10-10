package bpmn_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// A hold and then a waive of one visit are two acts: the key covers the kind,
// so each has its row, and neither is a replay of the other. A waive previewed
// before the hold is still the plan of that work afterwards — a waive's key
// leaves the step's incidents out — and applies.
//
// What becomes of the hold's incident: it stays open, on a step the instance
// has left. A waive does nothing with a step's incidents, and nothing closes
// one when the instance moves on. Found, not changed; it is on the roadmap.
func TestAHoldAndThenAWaiveOfOneVisitAreTwoActs(t *testing.T) {
	h := newEngineHarness(t, "Hold Then Waive Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, opsApproval(h.projID, "ops-hold-then-waive"), nil)
	waive := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", map[string]any{"approved": true}))

	held := w.mustApply(t, deviationCommand(entities.DeviationHold, id, "opsApprove", nil))
	if held.Deviation.VisitKey == waive.VisitKey {
		t.Fatal("a hold and a waive of one visit have one key; the second would be answered as the first")
	}
	out, err := w.svc.DeviateInstance(w.ctx, waive)
	if err != nil || !out.Applied || out.Replayed || out.Deviation == nil || out.Deviation.Kind != entities.DeviationWaive {
		t.Fatalf("the waive previewed before the hold, applied after it: %+v, %v; want it applied as a waive", out, err)
	}
	if !h.waitingAt(ctx, t, id, "salesApprove") || tokensOn(t, h, id, "opsApprove") != 0 {
		t.Fatal("the instance did not move on from the waived step")
	}
	rows := w.ledger(t, id)
	if len(rows) != 2 || rows[0].Kind != entities.DeviationHold || rows[1].Kind != entities.DeviationWaive || rows[0].VisitKey == rows[1].VisitKey {
		t.Fatalf("the ledger: %+v; want the hold and then the waive, each with its own key", rows)
	}
	incidents, err := h.svc.ListIncidents(ctx, id)
	if err != nil || len(incidents) != 1 || incidents[0].Status != entities.IncidentOpen || incidents[0].Node == nil || incidents[0].Node.ID != "opsApprove" {
		t.Fatalf("the hold's incident after the waive: %+v (err %v); it is expected still open on the step the instance left", incidents, err)
	}
}

// An apply in place of a plan previewed before a migration dealt with the
// instance. The migration takes the instance's row as an apply does, so the
// apply comes after it and asks the locked row: the instance is on another
// version, and its key is another; or it was cancelled, and is refused as
// ended; or it was moved past the step, and waits somewhere else. In each
// case the apply is told, and nothing is written.
func TestAnApplyOfAPlanPreviewedBeforeAMigrationDealtWithTheInstanceDoesNothing(t *testing.T) {
	const previewAgain = "this instance has moved since you previewed it; preview again"
	for _, migration := range []struct {
		name string
		// keepsTheStep says whether the version migrated to has the step.
		keepsTheStep bool
		// waits says the migration has to be approved by a second
		// administrator: only the one that skips the step does.
		waits   bool
		options []servicecontracts.MigrationOption
		status  entities.ProcessStatus
		says    map[entities.DeviationKind]string
	}{
		{"moved to another version", true, false, nil, entities.ProcessActive,
			map[entities.DeviationKind]string{entities.DeviationWaive: previewAgain, entities.DeviationCancel: previewAgain, entities.DeviationHold: previewAgain}},
		{"cancelled by its decision", false, false, decidingAtOpsApprove(servicecontracts.NodeActionCancel), entities.ProcessCancelled,
			map[entities.DeviationKind]string{
				entities.DeviationWaive:  "this instance is cancelled, so it can no longer be waived; preview again",
				entities.DeviationCancel: "this instance is cancelled, so it can no longer be cancelled; preview again",
				entities.DeviationHold:   "this instance is cancelled, so it can no longer be held; preview again",
			}},
		{"moved past the step by its decision", false, true, decidingAtOpsApprove(servicecontracts.NodeActionSkip), entities.ProcessActive,
			map[entities.DeviationKind]string{entities.DeviationWaive: previewAgain, entities.DeviationCancel: previewAgain, entities.DeviationHold: previewAgain}},
	} {
		t.Run(migration.name, func(t *testing.T) {
			h := newEngineHarness(t, "Apply After Migration Project")
			h.recordsAsProductionDoes()
			w := newWaiver(h)
			ctx := h.Ctx()
			v1def := withFlowsListed(opsApproval(h.projID, "ops-then-migrated"))
			id := w.start(t, v1def, nil)
			row, err := h.repo.Process().Get(ctx, id)
			if err != nil {
				t.Fatalf("read the instance: %v", err)
			}
			v1 := uuid.UUID(row.DefinitionID)
			stale := map[entities.DeviationKind]entities.DeviationCommand{}
			for _, kind := range []entities.DeviationKind{entities.DeviationWaive, entities.DeviationCancel, entities.DeviationHold} {
				var outputs map[string]any
				if kind == entities.DeviationWaive {
					outputs = map[string]any{"approved": true}
				}
				stale[kind] = w.previewed(t, deviationCommand(kind, id, "opsApprove", outputs))
			}

			v2def := withFlowsListed(opsApproval(h.projID, "ops-then-migrated"))
			if !migration.keepsTheStep {
				v2def = withFlowsListed(withoutOpsApprove(h.projID, "ops-then-migrated"))
			}
			v2, err := h.svc.CreateDefinition(ctx, v2def)
			if err != nil {
				t.Fatalf("deploy the next version: %v", err)
			}
			migrate := func() error { return h.svc.MigrateInstances(ctx, v1, v2, nil, migration.options...) }
			if migration.waits {
				migrate = func() error { return migrateSeconded(t, h, v1, v2, nil, migration.options...) }
			}
			if err := migrate(); err != nil {
				t.Fatalf("migrate: %v", err)
			}
			now := requireInstanceStatus(ctx, t, h, id, migration.status)
			if after, err := h.repo.Process().Get(ctx, id); err != nil || (migration.status == entities.ProcessActive && models.UUID(v2) != after.DefinitionID) {
				t.Fatalf("the instance is on version %s after the migration (err %v), want %s", uuid.UUID(after.DefinitionID), err, v2)
			}
			if migration.status == entities.ProcessActive && len(now.Tokens) != 1 {
				t.Fatalf("the instance holds %d token(s) after the migration; this test needs it waiting at one step", len(now.Tokens))
			}
			migrated := everyRow(t, h)

			for kind, command := range stale {
				out, err := w.svc.DeviateInstance(w.ctx, command)
				if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.HasSuffix(err.Error(), migration.says[kind]) {
					t.Errorf("a %s previewed before the migration, applied after it: %v\nwant %q", kind, err, migration.says[kind])
				}
				if out.Applied || out.Replayed || out.Deviation != nil {
					t.Errorf("a %s previewed before the migration answered %+v", kind, out)
				}
			}
			if changed := tablesThatDiffer(migrated, everyRow(t, h)); len(changed) != 0 {
				t.Fatalf("applies of plans the migration had overtaken changed %v", changed)
			}
		})
	}
}

// decidingAtOpsApprove is a migration's decision for the instances waiting at
// the operations approval, as dita.
func decidingAtOpsApprove(kind servicecontracts.NodeActionKind) []servicecontracts.MigrationOption {
	return []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: kind, Reason: "the operations approval was dropped from the process"},
		}),
		servicecontracts.WithActor("dita"),
	}
}

// withoutOpsApprove is opsApproval after its first step was dropped: start →
// Sales approve → end.
func withoutOpsApprove(projectID uuid.UUID, key string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID}, Key: key, Name: "Quotation approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "salesApprove", Type: entities.UserTask, Name: "Sales approve"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "q1", SourceRef: "start", TargetRef: "salesApprove"}, {ID: "q3", SourceRef: "salesApprove", TargetRef: "end"}},
	}
}

// withFlowsListed gives each step its own list of the flows that enter and
// leave it, as a definition drawn in the designer has them. A migration's
// planner reads a step's own list where the in-place planner reads the
// definition's flows.
func withFlowsListed(def *entities.ProcessDefinition) *entities.ProcessDefinition {
	for _, node := range def.Nodes {
		node.Incoming, node.Outgoing = nil, nil
		for _, flow := range def.Flows {
			if flow.TargetRef == node.ID {
				node.Incoming = append(node.Incoming, flow.ID)
			}
			if flow.SourceRef == node.ID {
				node.Outgoing = append(node.Outgoing, flow.ID)
			}
		}
	}
	return def
}
