package bpmn_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
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

// requireNothingStarted fails unless an activation of nodeID that was refused
// left nothing behind: no task, no token, no ledger row and no entry saying
// somebody started it.
func requireNothingStarted(t *testing.T, h engineHarness, instanceID uuid.UUID, nodeID string) {
	t.Helper()
	if n := tasksEverOn(t, h, instanceID, nodeID); n != 0 {
		t.Fatalf("the refused activation left %d task(s) on %s", n, nodeID)
	}
	if tokens := tokenIterationsOn(h.Ctx(), t, h, instanceID, nodeID); len(tokens) != 0 {
		t.Fatalf("the refused activation left %d token(s) on %s", len(tokens), nodeID)
	}
	rows, err := h.repo.Deviation().ListByInstance(h.Ctx(), instanceID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("the refused activation left %d ledger row(s) (err %v)", len(rows), err)
	}
	entries, err := h.svc.GetAuditLogs(h.Ctx(), instanceID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	for _, entry := range entries {
		if entry.Type == serviceimpl.EventStepActivated || (entry.Node != nil && entry.Node.ID == nodeID) {
			t.Fatalf("the refused activation left an entry on the trail: %+v", entry)
		}
	}
}

// "System" is for nobody signed in. An account that is signed in but has no
// name is somebody, and the ledger cannot say who: recording it as the system
// would hide that a person acted. The ledger refuses a row with no actor, so
// the activation is not made.
func TestAnActivationByAnAccountWithNoNameIsRefusedAndNothingIsLeft(t *testing.T) {
	h := newEngineHarness(t, "Activation Nameless Project")
	instanceID := startAdHocResearch(t, h, "claim-research-nameless")
	for _, name := range []string{"", "   "} {
		if err := h.svc.ActivateTask(asOperatorNamed(h.Ctx(), name), instanceID, "research", "call-customer"); err == nil {
			t.Fatalf("an activation by a signed-in account named %q was made", name)
		}
		requireNothingStarted(t, h, instanceID, "call-customer")
	}
}

// refusingTheEntry is the real audit store, except that it refuses to write an
// activation's entry.
type refusingTheEntry struct{ repocontracts.AuditRepository }

func (a refusingTheEntry) Create(ctx context.Context, entry models.AuditModel) error {
	if entry.Type == serviceimpl.EventStepActivated {
		return errors.New("the trail cannot be written")
	}
	return a.AuditRepository.Create(ctx, entry)
}

type withoutTheEntry struct{ repositories.Repository }

func (r withoutTheEntry) Audit() repocontracts.AuditRepository {
	return refusingTheEntry{r.Repository.Audit()}
}

// The row and the entry are one record of one act. When the entry cannot be
// written the row already has been, in the same transaction: both go, and the
// step is not started.
func TestAnActivationWhoseEntryCannotBeWrittenIsNotMade(t *testing.T) {
	h := newEngineHarness(t, "Activation Untold Project")
	instanceID := startAdHocResearch(t, h, "claim-research-untold")
	activator := serviceimpl.NewAdHocActivator(h.engine, withoutTheEntry{h.repo})
	if err := activator.ActivateTask(asOperatorNamed(h.Ctx(), "olga"), instanceID, "research", "call-customer"); err == nil {
		t.Fatal("an activation whose entry could not be written reported success")
	}
	requireNothingStarted(t, h, instanceID, "call-customer")
}

// notStarting is the real engine, except that no step it is asked to execute
// starts.
type notStarting struct {
	servicecontracts.ExecutionEngine
}

func (notStarting) ExecuteNode(context.Context, *entities.ProcessInstance, *entities.ProcessDefinition, string) error {
	return errors.New("the step could not be started")
}

// The activation is recorded before the step executes, so that the trail reads
// in the order things happened. A step that then fails to start must not leave
// a row and an entry saying somebody started it.
func TestAStepThatFailsToStartLeavesNoRecordOfBeingStarted(t *testing.T) {
	h := newEngineHarness(t, "Activation Failing Project")
	instanceID := startAdHocResearch(t, h, "claim-research-failing")
	activator := serviceimpl.NewAdHocActivator(notStarting{h.engine}, h.repo)
	if err := activator.ActivateTask(asOperatorNamed(h.Ctx(), "olga"), instanceID, "research", "call-customer"); err == nil {
		t.Fatal("an activation whose step did not start reported success")
	}
	requireNothingStarted(t, h, instanceID, "call-customer")
}

// The trail is read oldest first, in the order its entries were written.
// Somebody starting the step is the cause of everything the step then does, so
// it is the first thing the trail says about it.
func TestAnActivationIsToldBeforeWhatTheStepThenDid(t *testing.T) {
	h := newEngineHarness(t, "Activation Order Project")
	instanceID := startAdHocResearch(t, h, "claim-research-order")
	if err := h.svc.ActivateTask(asOperatorNamed(h.Ctx(), "olga"), instanceID, "research", "call-customer"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	entries, err := h.svc.GetAuditLogs(h.Ctx(), instanceID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	var about []string
	for _, entry := range entries {
		if entry.Node != nil && entry.Node.ID == "call-customer" {
			about = append(about, entry.Type)
		}
	}
	if len(about) < 2 || about[0] != serviceimpl.EventStepActivated {
		t.Fatalf("the trail tells the step as %v, want step_activated first and what it did after", about)
	}
}

// A reason the ledger would refuse is refused before anything is read, locked
// or started — an instance that is not there is never looked for — and the
// refusal is about the reason, in words, with no step id in it.
func TestAnOverLongReasonIsRefusedBeforeAnythingIsReadOrStarted(t *testing.T) {
	h := newEngineHarness(t, "Activation Long Reason Project")
	instanceID := startAdHocResearch(t, h, "claim-research-long-reason")
	tooLong := servicecontracts.WithActivationReason(strings.Repeat("x", entities.MaxDeviationReasonLength+1))

	for name, id := range map[string]uuid.UUID{"a running instance": instanceID, "an instance that is not there": uuid.New()} {
		err := h.svc.ActivateTask(asOperatorNamed(h.Ctx(), "olga"), id, "research", "call-customer", tooLong)
		if !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Fatalf("%s: an over-long reason gave %v, want the reason refused as the caller's to correct", name, err)
		}
		if strings.Contains(err.Error(), "call-customer") || strings.Contains(err.Error(), "research") {
			t.Fatalf("%s: the refusal names a step by id: %v", name, err)
		}
	}
	requireNothingStarted(t, h, instanceID, "call-customer")
}
