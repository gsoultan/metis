package instancemigration

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// newFixtureOver is newFixture over the real repository as wrap returns it, so
// a test can break one store and leave the rest working.
func newFixtureOver(t *testing.T, wrap func(repositories.Repository) repositories.Repository) *fixture {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := wrap(repositories.NewRepository(testutils.StormConn(db)))
	dispatcher := observersimpl.NewEventDispatcher()
	svc := services.NewServiceFacade(repo, dispatcher, observersimpl.NewSSEObserver(), "migration-test", nil, nil, nil)
	org, err := svc.CreateOrganization(context.Background(), "Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	tenantCtx := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: org.ID.String()})
	project, err := svc.CreateProject(tenantCtx, org.ID, "P", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return &fixture{svc: svc, ctx: tenantCtx, project: project.ID, dispatcher: dispatcher, db: db}
}

// trailRefusing is the real audit store, except that it refuses to write the
// kinds of entry named.
type trailRefusing struct {
	repocontracts.AuditRepository
	refused []string
}

func (a trailRefusing) Create(ctx context.Context, entry models.AuditModel) error {
	for _, refused := range a.refused {
		if entry.Type == refused {
			return errors.New("the trail cannot be written")
		}
	}
	return a.AuditRepository.Create(ctx, entry)
}

type trailRefusingRepository struct {
	repositories.Repository
	refused []string
}

func (r trailRefusingRepository) Audit() repocontracts.AuditRepository {
	return trailRefusing{AuditRepository: r.Repository.Audit(), refused: r.refused}
}

// ledgerFailingOnce is the real ledger store, except that one write — the
// failOn-th — is refused.
type ledgerFailingOnce struct {
	repocontracts.DeviationRepository
	writes *atomic.Int32
	failOn int32
}

func (l ledgerFailingOnce) Create(ctx context.Context, deviation entities.Deviation) (entities.Deviation, error) {
	if l.writes.Add(1) == l.failOn {
		return entities.Deviation{}, errors.New("the ledger lost its connection")
	}
	return l.DeviationRepository.Create(ctx, deviation)
}

type ledgerFailingOnceRepository struct {
	repositories.Repository
	writes *atomic.Int32
	failOn int32
}

func (r ledgerFailingOnceRepository) Deviation() repocontracts.DeviationRepository {
	return ledgerFailingOnce{DeviationRepository: r.Repository.Deviation(), writes: r.writes, failOn: r.failOn}
}

// severalParkedOnOpsApprove is parkedOnOpsApprove for count quotations.
func (f *fixture) severalParkedOnOpsApprove(t *testing.T, count int) (v1, v2 uuid.UUID) {
	t.Helper()
	v1, err := f.svc.CreateDefinition(f.ctx, quotationV1(f))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	for range count {
		if _, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil); err != nil {
			t.Fatalf("start a quotation: %v", err)
		}
		f.completeTaskOn(t, "supervisorReview", "sam")
	}
	v2, err = f.svc.CreateDefinition(f.ctx, quotationV2(f))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	return v1, v2
}

// trailTypes is the kinds of entry on an instance's trail.
func (f *fixture) trailTypes(t *testing.T, instanceID uuid.UUID) []string {
	t.Helper()
	entries, err := f.svc.GetAuditLogs(f.ctx, instanceID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	types := make([]string, 0, len(entries))
	for _, entry := range entries {
		types = append(types, entry.Type)
	}
	return types
}

// migrationEntries are the kinds of entry a migration writes about an instance.
var migrationEntries = []string{
	impl.EventNodeSkipped, impl.EventInstanceCancelled, impl.EventInstanceHeld, impl.EventInstanceMigrated,
}

// assertNoMigrationEntries fails when the trail says a migration did anything
// to the instance.
func (f *fixture) assertNoMigrationEntries(t *testing.T, instanceID uuid.UUID) {
	t.Helper()
	for _, found := range f.trailTypes(t, instanceID) {
		for _, written := range migrationEntries {
			if found == written {
				t.Fatalf("the trail holds a %s entry for a decision that was not made", found)
			}
		}
	}
}

// assertUntouched fails unless the instance is still running the source
// version with the operations approval open and nothing recorded against it.
func (f *fixture) assertUntouched(t *testing.T, instance entities.ProcessInstance, v1 uuid.UUID) {
	t.Helper()
	if instance.Status != entities.ProcessActive || instance.Definition == nil || instance.Definition.ID != v1 {
		t.Fatalf("instance %s is %s on %v; nothing should have changed", instance.ID, instance.Status, instance.Definition)
	}
	open := 0
	for _, task := range f.openTasks(t) {
		if task.Instance != nil && task.Instance.ID == instance.ID && task.NodeID() == "opsApprove" {
			open++
		}
	}
	if open != 1 {
		t.Fatalf("instance %s has %d open operations approvals, want the one it had", instance.ID, open)
	}
	if rows := f.ledger(t, instance.ID); len(rows) != 0 {
		t.Fatalf("instance %s has %d ledger row(s) for a decision that was not made", instance.ID, len(rows))
	}
	if incidents, err := f.svc.ListIncidents(f.ctx, instance.ID); err != nil || len(incidents) != 0 {
		t.Fatalf("instance %s has %d incident(s) (err %v), want none", instance.ID, len(incidents), err)
	}
	f.assertNoMigrationEntries(t, instance.ID)
}

var everyNodeAction = []servicecontracts.NodeActionKind{
	servicecontracts.NodeActionSkip, servicecontracts.NodeActionCancel, servicecontracts.NodeActionHold,
}

// The ledger keeps no more than 2000 characters of a reason, so a longer one
// is refused where every other refusal is made: in the plan, before anything
// is written. A dry run that calls a migration fine and an apply that then
// stops part-way is the disagreement the planner exists to prevent.
func TestAReasonTheLedgerCannotHoldIsRefusedBeforeAnythingMoves(t *testing.T) {
	tooLong := strings.Repeat("é", entities.MaxDeviationReasonLength+1)
	for _, kind := range everyNodeAction {
		t.Run(string(kind), func(t *testing.T) {
			f := newFixture(t)
			v1, v2 := f.parkedOnOpsApprove(t)
			opts := []servicecontracts.MigrationOption{
				servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{"opsApprove": {Kind: kind, Reason: tooLong}}),
				servicecontracts.WithActor("dita"),
			}
			plan, err := f.svc.PlanInstanceMigration(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, opts...)
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			const want = "has a reason longer than 2000 characters"
			if plan.Applicable() || !strings.Contains(strings.Join(plan.Refusals, "; "), want) {
				t.Fatalf("the dry run calls a %s with a reason the ledger cannot hold applicable: %v", kind, plan.Refusals)
			}
			err = f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, opts...)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("apply answered %v, want the plan's refusal", err)
			}
			f.assertUntouched(t, f.onlyInstance(t), uuidOf(t, v1))
		})
	}
}

// The limit is counted the way the ledger counts it: in characters, after the
// spaces around the reason are dropped. A reason exactly as long as the ledger
// takes is planned as applicable and is applied.
func TestAReasonAsLongAsTheLedgerTakesIsAccepted(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)
	reason := "  " + strings.Repeat("é", entities.MaxDeviationReasonLength) + "  "
	opts := skipOps(reason)
	plan, err := f.svc.PlanInstanceMigration(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, opts...)
	if err != nil || !plan.Applicable() {
		t.Fatalf("a reason of exactly the ledger's length was refused: %v (err %v)", plan.Refusals, err)
	}
	if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, opts...); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if rows := f.ledger(t, f.onlyInstance(t).ID); len(rows) != 1 || rows[0].Reason != strings.TrimSpace(reason) {
		t.Fatalf("the skip's ledger: %d row(s)", len(rows))
	}
}

// The trail entry of a decision is written with the decision, not after it: a
// skip, cancel or hold whose entry cannot be written is not made, leaves no
// ledger row behind, and the migration says which instance it stopped at.
func TestADecisionWhoseTrailEntryCannotBeWrittenIsNotMade(t *testing.T) {
	for _, kind := range everyNodeAction {
		t.Run(string(kind), func(t *testing.T) {
			f := newFixtureOver(t, func(repo repositories.Repository) repositories.Repository {
				return trailRefusingRepository{Repository: repo, refused: migrationEntries[:3]}
			})
			v1, v2 := f.parkedOnOpsApprove(t)
			err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil,
				servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{"opsApprove": {Kind: kind, Reason: "policy"}}),
				servicecontracts.WithActor("dita"))
			if err == nil {
				t.Fatalf("a %s whose trail entry could not be written reported success", kind)
			}
			instance := f.onlyInstance(t)
			if !strings.Contains(err.Error(), instance.ID.String()) {
				t.Errorf("the error does not say which instance it stopped at: %v", err)
			}
			f.assertUntouched(t, instance, uuidOf(t, v1))
		})
	}
}

// A migration works instance by instance. When the ledger fails part-way, what
// was done stays done and recorded, what was not is untouched, and the error
// names the instance to look at.
func TestALedgerFailureOnTheSecondInstanceLeavesTheFirstDoneAndTheRestUntouched(t *testing.T) {
	writes := &atomic.Int32{}
	f := newFixtureOver(t, func(repo repositories.Repository) repositories.Repository {
		return ledgerFailingOnceRepository{Repository: repo, writes: writes, failOn: 2}
	})
	v1, v2 := f.severalParkedOnOpsApprove(t, 3)

	err := f.svc.MigrateInstances(f.ctx, v1, v2, nil, skipOps("the operations manager role was eliminated")...)
	if err == nil {
		t.Fatal("a migration whose second skip could not be ledgered reported success")
	}
	if !strings.Contains(err.Error(), "1 of 3 instances had already been dealt with") {
		t.Errorf("the error does not say how far the run got: %v", err)
	}

	instances, listErr := f.svc.ListInstances(f.ctx, f.project)
	if listErr != nil || len(instances) != 3 {
		t.Fatalf("list instances: %d (err %v)", len(instances), listErr)
	}
	var migrated, untouched []entities.ProcessInstance
	for _, instance := range instances {
		if instance.Definition != nil && instance.Definition.ID == v2 {
			migrated = append(migrated, instance)
			continue
		}
		untouched = append(untouched, instance)
	}
	if len(migrated) != 1 || len(untouched) != 2 {
		t.Fatalf("%d instance(s) moved and %d stayed, want one and two", len(migrated), len(untouched))
	}

	first := migrated[0]
	rows := f.ledger(t, first.ID)
	if len(rows) != 1 || rows[0].Kind != entities.DeviationWaive {
		t.Fatalf("the migrated instance's ledger: %+v", rows)
	}
	if skipped := f.entryOf(t, first.ID, impl.EventNodeSkipped); skipped.ID != rows[0].AuditEntryID {
		t.Errorf("the migrated instance's row names entry %s; its skip entry is %s", rows[0].AuditEntryID, skipped.ID)
	}
	f.entryOf(t, first.ID, impl.EventInstanceMigrated)

	named := 0
	for _, instance := range untouched {
		f.assertUntouched(t, instance, v1)
		if strings.Contains(err.Error(), instance.ID.String()) {
			named++
		}
	}
	if named != 1 || strings.Contains(err.Error(), first.ID.String()) {
		t.Errorf("the error should name the one instance it stopped at, and only it: %v", err)
	}
}

// A step may be named at greater length than the ledger keeps. The row then
// carries as much of the name as fits — the id is what identifies the step —
// rather than failing a migration its dry run called fine.
func TestAStepWithAVeryLongNameIsStillLedgered(t *testing.T) {
	f := newFixture(t)
	long := strings.Repeat("persetujuan — ", 30)
	v1Def := quotationV1(f)
	for _, node := range v1Def.Nodes {
		if node.ID == "opsApprove" {
			node.Name = long
		}
	}
	v1, err := f.svc.CreateDefinition(f.ctx, v1Def)
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil); err != nil {
		t.Fatalf("start a quotation: %v", err)
	}
	f.completeTaskOn(t, "supervisorReview", "sam")
	v2, err := f.svc.CreateDefinition(f.ctx, quotationV2(f))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	opts := skipOps("the operations manager role was eliminated")
	if plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil, opts...); err != nil || !plan.Applicable() {
		t.Fatalf("plan: %v (err %v)", plan.Refusals, err)
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, nil, opts...); err != nil {
		t.Fatalf("the dry run called this applicable, and apply answered: %v", err)
	}
	rows := f.ledger(t, f.onlyInstance(t).ID)
	if len(rows) != 1 || rows[0].Node == nil || rows[0].Node.ID != "opsApprove" {
		t.Fatalf("the skip's ledger: %+v", rows)
	}
	if kept := []rune(rows[0].Node.Name); len(kept) != 255 || !strings.HasPrefix(long, rows[0].Node.Name) {
		t.Errorf("the row keeps %d characters of the step's name, want the first 255", len(kept))
	}
}
