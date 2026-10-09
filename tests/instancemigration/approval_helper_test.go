package instancemigration

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/endpoints/definition"
	"github.com/gsoultan/metis/server/endpoints/deviation"
)

// accountOf is the account id of the test administrator called name: derived
// from the name, so the same name is the same account wherever it is used.
func accountOf(name string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("metis-test-account:"+name))
}

// adminAs is ctx from a signed-in administrator called name, whose account id
// is derived from the name.
func adminAs(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, pkgauth.UserContextKey, entities.User{
		ID: accountOf(name), Username: name, Roles: []string{entities.RoleAdmin}})
}

// requestCount is how many requests for a second administrator there are.
func (f *fixture) requestCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw(`SELECT count(*) FROM deviation_requests`).Row().Scan(&n); err != nil {
		t.Fatalf("count the requests: %v", err)
	}
	return n
}

// storedStatus is a request's status as its row holds it, whatever a reader
// would be told it reads as now.
func (f *fixture) storedStatus(t *testing.T, requestID uuid.UUID) string {
	t.Helper()
	var stored string
	if err := f.db.Raw(`SELECT status FROM deviation_requests WHERE id = ?`, requestID).Row().Scan(&stored); err != nil {
		t.Fatalf("read the request: %v", err)
	}
	return stored
}

// assertNothingMoved fails unless the one instance is still running v1 with
// its operations approval open, nothing in its ledger and nothing on its
// trail that a migration writes.
func (f *fixture) assertNothingMoved(t *testing.T, v1 uuid.UUID) {
	t.Helper()
	instance := f.onlyInstance(t)
	if instance.Definition == nil || instance.Definition.ID != v1 || instance.Status != entities.ProcessActive {
		t.Fatalf("the instance is %s on %v, want it running the version it was started on", instance.Status, instance.Definition)
	}
	if open := f.openTasks(t); len(open) != 1 || open[0].NodeID() != "opsApprove" {
		t.Fatalf("%d task(s) are open (%+v), want the one operations approval", len(open), open)
	}
	if rows := f.ledger(t, instance.ID); len(rows) != 0 {
		t.Fatalf("the ledger holds %d row(s) for an instance nothing was done to: %+v", len(rows), rows)
	}
	f.assertNoMigrationEntries(t, instance.ID)
}

// atTheApprovedApplysListing runs fire when the apply an approval runs has
// listed its instances: the approval plans again, ApplyInstanceMigration plans
// so that it and a dry run cannot disagree, and the apply's listing is the third.
func (p *hookedProcess) atTheApprovedApplysListing(fire func()) {
	p.on = p.calls + 3
	p.fire = fire
}

// letTimePass moves a request's deadline minutes into the past: the state
// time passing creates.
func (f *fixture) letTimePass(t *testing.T, requestID uuid.UUID, minutes int) {
	t.Helper()
	if err := f.db.Exec(`UPDATE deviation_requests SET expires_at = now() - make_interval(mins => ?) WHERE id = ?`, minutes, requestID).Error; err != nil {
		t.Fatalf("move the deadline of request %s: %v", requestID, err)
	}
}

// A test that hooks "the apply's listing" counts listings from the moment it
// arms the hook: the plan lists, then the apply. Asking for an approval lists
// too. So the helpers below take a test's hook off while they ask, and put it
// back counting from the call that runs the migration.

// unhook takes off the listing hook a test armed, and answers what it was to
// run and how many listings ahead of now.
func (f *fixture) unhook() (fire func(), ahead int) {
	p := f.listing
	if p == nil {
		return nil, 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fire, ahead = p.fire, p.on-p.calls
	p.fire = nil
	return fire, ahead
}

// rehook arms it again, to run at the listing `ahead` from now.
func (f *fixture) rehook(fire func(), ahead int) {
	p := f.listing
	if p == nil || fire == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.on, p.fire = p.calls+ahead, fire
}

// applyWithApproval applies a migration the way it is done since a second
// administrator approves a skip: a plan that needs one is asked for by dita
// and approved by omar; any other plan — and any refused one — goes to
// ApplyInstanceMigration as before. It answers what the run did either way.
func (f *fixture) applyWithApproval(t *testing.T, source, target uuid.UUID, mapping map[string]string,
	opts ...servicecontracts.MigrationOption) (entities.MigrationResult, error) {
	t.Helper()
	fire, ahead := f.unhook()
	plan, err := f.svc.PlanInstanceMigration(f.ctx, source, target, mapping, opts...)
	if err != nil || !plan.Applicable() || !plan.RequiresSecondApprover {
		f.rehook(fire, ahead)
		return f.svc.ApplyInstanceMigration(f.ctx, source, target, mapping, opts...)
	}
	// What needs a second administrator must not go through on one call: the
	// apply itself has to refuse it, having moved nothing, or every test that
	// comes through here would pass over a gate that was not there. Made
	// while the test's hook is off, so it is not counted among its listings.
	if _, err := f.svc.ApplyInstanceMigration(f.ctx, source, target, mapping, opts...); !errors.Is(err, apierr.ErrForbidden) {
		t.Errorf("a migration that needs a second administrator was answered %v on one administrator's call, want it forbidden", err)
		return entities.MigrationResult{}, err
	}
	pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), source, target, mapping, opts...)
	if err != nil {
		return entities.MigrationResult{}, err
	}
	if pending.Status != entities.DeviationRequestPending {
		t.Errorf("asking for the migration answered %+v, want a request waiting for approval", pending)
	}
	f.underApproval = pending.RequestID
	// One listing more than a direct apply: the approval's own plan.
	f.rehook(fire, ahead+1)
	out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "")
	if out.MigrationResult == nil {
		return entities.MigrationResult{}, err
	}
	return *out.MigrationResult, err
}

// migrateWithApproval is applyWithApproval for a test that asks only whether
// the migration went through.
func (f *fixture) migrateWithApproval(t *testing.T, source, target uuid.UUID, mapping map[string]string,
	opts ...servicecontracts.MigrationOption) error {
	t.Helper()
	_, err := f.applyWithApproval(t, source, target, mapping, opts...)
	return err
}

// sameApproval carries the approval the fixture's last approved run is under,
// for a hook that runs the same migration again inside it. Nothing when that
// run needed nobody else.
func (f *fixture) sameApproval() []servicecontracts.MigrationOption {
	if f.underApproval == uuid.Nil {
		return nil
	}
	return []servicecontracts.MigrationOption{servicecontracts.WithApprovedRequest(f.underApproval)}
}

// asked is the migrate endpoint's answer to ctx, refused or not.
func (f *fixture) asked(t *testing.T, ctx context.Context, request definition.MigrateInstancesRequest) definition.MigrateInstancesResponse {
	t.Helper()
	reply, err := definition.MakeMigrateInstancesEndpoint(f.svc)(ctx, request)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return reply.(definition.MigrateInstancesResponse)
}

// answerOf is what the migrate endpoint answers — or, for an apply the endpoint
// sent for approval, what omar's approval of it answers: the reply that then
// says what the run did, under the same `applied` and `passed_over`.
func (f *fixture) answerOf(t *testing.T, request definition.MigrateInstancesRequest) any {
	t.Helper()
	answered := func(ctx context.Context, request definition.MigrateInstancesRequest) definition.MigrateInstancesResponse {
		t.Helper()
		reply := f.asked(t, ctx, request)
		if reply.Err != nil {
			t.Fatalf("the migration was refused: %v", reply.Err)
		}
		return reply
	}
	if request.DryRun == nil || *request.DryRun {
		return answered(f.ctx, request)
	}
	fire, ahead := f.unhook()
	preview := request
	preview.DryRun = nil
	if plan := answered(f.ctx, preview).Plan; !plan.Applicable() || !plan.RequiresSecondApprover {
		f.rehook(fire, ahead)
		return answered(f.ctx, request)
	}
	sent := answered(adminAs(f.ctx, "dita"), request)
	if sent.PendingApproval == nil || sent.Applied || len(sent.PassedOver) != 0 {
		t.Fatalf("an apply that needs a second administrator was not sent for approval: %+v", sent)
	}
	f.underApproval = sent.PendingApproval.RequestID
	// The approval's own plan stands where the endpoint's did.
	f.rehook(fire, ahead)
	approved, err := deviation.MakeApproveDeviationRequestEndpoint(f.svc)(adminAs(f.ctx, "omar"),
		deviation.DecideDeviationRequestRequest{ID: sent.PendingApproval.RequestID.String()})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if failed := approved.(deviation.ApproveDeviationRequestResponse).Err; failed != nil {
		t.Fatalf("the approval was refused: %v", failed)
	}
	return approved
}
