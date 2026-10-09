package instancemigration

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// listingThatFails is the store of instances, whose listing of a version's
// instances fails for as long as it is told to: a database that is away.
type listingThatFails struct {
	repocontracts.ProcessRepository
	away atomic.Bool
}

func (p *listingThatFails) ListByDefinition(ctx context.Context, id uuid.UUID) ([]models.ProcessInstanceModel, error) {
	if p.away.Load() {
		return nil, errors.New("the database is away")
	}
	return p.ProcessRepository.ListByDefinition(ctx, id)
}

type awayRepository struct {
	repositories.Repository
	process *listingThatFails
}

func (r *awayRepository) Process() repocontracts.ProcessRepository { return r.process }

// An approval plans the migration again before it approves. A plan that
// cannot be made because of what it asks — a version that is gone, an
// instance that no longer runs there — makes the request stale. A plan that
// cannot be made because something failed says nothing about the request:
// the approver is told of the failure, as the server's, and the request waits
// exactly as it did, to be approved once the failure has passed.
func TestAnApprovalThatCannotPlanJustNowLeavesTheRequestWaiting(t *testing.T) {
	listing := &listingThatFails{}
	f := newFixtureOver(t, func(repo repositories.Repository) repositories.Repository {
		listing.ProcessRepository = repo.Process()
		return &awayRepository{Repository: repo, process: listing}
	})
	dita, v1, v2, requestID := f.askToSkipOps(t)

	listing.away.Store(true)
	out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), requestID, "agreed")
	listing.away.Store(false)
	if err == nil || out.Applied || out.MigrationResult != nil {
		t.Fatalf("an approval whose plan could not be made: %+v %v, want the failure", out, err)
	}
	for _, class := range []error{apierr.ErrInvalidArgument, apierr.ErrNotFound, apierr.ErrForbidden} {
		if errors.Is(err, class) {
			t.Fatalf("the failure is answered as %v: %v; it is the server's, and nothing the approver can put right", class, err)
		}
	}
	if !strings.Contains(err.Error(), "planning the migration request "+requestID.String()+" asks for") {
		t.Fatalf("the failure does not say what was being done: %v", err)
	}
	read, err := f.svc.GetDeviationRequest(dita, requestID)
	if err != nil || read.Status != entities.DeviationRequestPending || f.storedStatus(t, requestID) != "pending_approval" ||
		read.DecidedBy != "" || read.DecidedAt != nil || len(read.Outcome) != 0 {
		t.Fatalf("the request after the failed approval: %q decided by %q with %v (err %v), want it waiting as it was", read.Status, read.DecidedBy, read.Outcome, err)
	}
	f.assertNothingMoved(t, v1)

	// The failure past, the same approval is taken.
	out, err = f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), requestID, "agreed")
	if err != nil || !out.Applied || out.MigrationResult.Changed != 1 {
		t.Fatalf("the approval once the listing answers again: %+v %v", out, err)
	}
	if instance := f.onlyInstance(t); instance.Definition == nil || instance.Definition.ID != v2 {
		t.Fatal("the approved migration did not move the instance")
	}
}
