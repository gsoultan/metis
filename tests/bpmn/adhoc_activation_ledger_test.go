package bpmn_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

func startAdHocResearch(t *testing.T, h engineHarness, key string) uuid.UUID {
	t.Helper()
	def := adHocDefinition(key, "reviewsDone >= 2")
	def.Project = &entities.Project{ID: h.projID}
	h.deploy(t, &def)
	instanceID, err := h.svc.StartProcess(h.Ctx(), h.projID, key, map[string]any{"reviewsDone": 0})
	if err != nil {
		t.Fatalf("start %s: %v", key, err)
	}
	return instanceID
}

func asOperatorNamed(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, pkgauth.UserContextKey, entities.User{Username: name, Roles: []string{entities.RoleOperator}})
}

// BPMN 2.0.2 §10.2.5 (Ad-Hoc Sub-Process): the performers decide which
// activities run and when. That choice is a modelled option, not an override,
// so it needs no reason — but it is a choice somebody made about this case,
// and the ledger and the trail now name who made it.
func TestAnActivationIsLedgeredWithItsActorAndReason(t *testing.T) {
	h := newEngineHarness(t, "Activation Ledger Project")
	instanceID := startAdHocResearch(t, h, "claim-research-ledger")

	if err := h.svc.ActivateTask(asOperatorNamed(h.Ctx(), "olga"), instanceID, "research", "call-customer",
		servicecontracts.WithActivationReason("the customer asked for a call back")); err != nil {
		t.Fatalf("activate: %v", err)
	}
	rows, err := h.repo.Deviation().ListByInstance(h.Ctx(), instanceID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("the ledger holds %d rows (err %v), want one", len(rows), err)
	}
	row := rows[0]
	if row.Kind != entities.DeviationAdHocActivation || row.Origin != entities.DeviationOriginAdHoc || row.Scope != entities.DeviationScopeTask ||
		row.Actor != "olga" || row.Reason != "the customer asked for a call back" ||
		row.Node == nil || row.Node.ID != "call-customer" || row.Details["sub_process"] != "research" {
		t.Fatalf("the activation's row: %+v", row)
	}
	entries, err := h.svc.GetAuditLogs(h.Ctx(), instanceID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	for _, entry := range entries {
		if entry.Type != serviceimpl.EventStepActivated {
			continue
		}
		if entry.Data["actor"] != "olga" || entry.Data["deviation_id"] != row.ID.String() || entry.ID != row.AuditEntryID {
			t.Fatalf("the activation's entry %+v does not name olga and its row", entry)
		}
		return
	}
	t.Fatal("the trail has no step_activated entry")
}

// Nobody signed in is the server acting for itself — a script through the
// service, a test. It is recorded as the system, as a migration with no actor
// is, rather than refused: refusing would change what an activation does for
// every caller that has always made one this way.
func TestAnActivationWithNobodySignedInIsLedgeredAsTheSystem(t *testing.T) {
	h := newEngineHarness(t, "Activation System Project")
	instanceID := startAdHocResearch(t, h, "claim-research-system")
	if err := h.svc.ActivateTask(h.Ctx(), instanceID, "research", "check-records"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	rows, _ := h.repo.Deviation().ListByInstance(h.Ctx(), instanceID)
	if len(rows) != 1 || rows[0].Actor != "System" || rows[0].Reason != "" {
		t.Fatalf("the ledger: %+v, want one row by the system with no reason", rows)
	}
}

type refusingDeviationStore struct{}

func (refusingDeviationStore) Create(context.Context, entities.Deviation) (entities.Deviation, error) {
	return entities.Deviation{}, errors.New("the ledger table is not there")
}
func (refusingDeviationStore) ListByInstance(context.Context, uuid.UUID) ([]entities.Deviation, error) {
	return nil, nil
}

func (refusingDeviationStore) FindLiveByVisit(context.Context, uuid.UUID, string) (entities.Deviation, bool, error) {
	return entities.Deviation{}, false, nil
}

type withoutLedger struct{ repositories.Repository }

func (withoutLedger) Deviation() repocontracts.DeviationRepository { return refusingDeviationStore{} }

// An activation that cannot be recorded is not made: no token, no task.
func TestAnActivationThatCannotBeLedgeredIsNotMade(t *testing.T) {
	h := newEngineHarness(t, "Activation Unledgered Project")
	instanceID := startAdHocResearch(t, h, "claim-research-unledgered")
	activator := serviceimpl.NewAdHocActivator(h.engine, withoutLedger{h.repo})
	if err := activator.ActivateTask(asOperatorNamed(h.Ctx(), "olga"), instanceID, "research", "call-customer"); err == nil {
		t.Fatal("an activation that could not be ledgered reported success")
	}
	if h.waitingAt(h.Ctx(), t, instanceID, "call-customer") {
		t.Fatal("the step was offered although its activation was not recorded")
	}
	if tokens := tokenIterationsOn(h.Ctx(), t, h, instanceID, "call-customer"); len(tokens) != 0 {
		t.Fatalf("the step holds %d token(s) after a refused activation", len(tokens))
	}
}
