package instancemigration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// ledgerMissingOnce is the real ledger store, except that one write — the
// failOn-th — fails as something not found: the class a row that has gone
// under a run comes back with.
type ledgerMissingOnce struct {
	repocontracts.DeviationRepository
	writes *atomic.Int32
	failOn int32
}

func (l ledgerMissingOnce) Create(ctx context.Context, deviation entities.Deviation) (entities.Deviation, error) {
	if l.writes.Add(1) == l.failOn {
		return entities.Deviation{}, fmt.Errorf("the instance of this ledger row: %w", apierr.ErrNotFound)
	}
	return l.DeviationRepository.Create(ctx, deviation)
}

type ledgerMissingOnceRepository struct {
	repositories.Repository
	writes *atomic.Int32
	failOn int32
}

func (r ledgerMissingOnceRepository) Deviation() repocontracts.DeviationRepository {
	return ledgerMissingOnce{DeviationRepository: r.Repository.Deviation(), writes: r.writes, failOn: r.failOn}
}

// TestAnApprovedRunThatStopsPartWayIsTheServersFailureWhateverClassItsCauseHas.
//
// Root cause: what an approver is told of a run that did not finish kept the
// class of whatever stopped it. That is right for a refusal — the plan's, the
// gate's — made before anything moved. A run that stops part-way on a failure
// that happens to carry a class is not one: answered "not found", the approve
// route said the request was not there, with one instance already moved under
// it.
func TestAnApprovedRunThatStopsPartWayIsTheServersFailureWhateverClassItsCauseHas(t *testing.T) {
	writes := &atomic.Int32{}
	f := newFixtureOver(t, func(repo repositories.Repository) repositories.Repository {
		return ledgerMissingOnceRepository{Repository: repo, writes: writes, failOn: 2}
	})
	v1, v2 := f.severalParkedOnOpsApprove(t, 2)
	dita := adminAs(f.ctx, "dita")
	pending, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, skipOps("the role was eliminated")...)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "")
	if err == nil || out.Applied || out.MigrationResult == nil || out.MigrationResult.Changed != 1 {
		t.Fatalf("the run whose second instance failed: %+v %v, want the failure with one instance moved", out, err)
	}
	for _, class := range []error{apierr.ErrNotFound, apierr.ErrInvalidArgument, apierr.ErrForbidden} {
		if errors.Is(err, class) {
			t.Errorf("the failure is answered as %v: %v; the run stopped part-way, and that is the server's", class, err)
		}
	}
	said := err.Error()
	if !strings.HasPrefix(said, "the approved migration did not finish: ") ||
		!strings.HasSuffix(said, "Request "+pending.RequestID.String()+" now reads interrupted; what its run had done stands, and what remains has to be asked for again") ||
		strings.Contains(said, "run the same migration again") {
		t.Errorf("the failure reads: %s", said)
	}
	read, err := f.svc.GetDeviationRequest(dita, pending.RequestID)
	if err != nil || read.Status != entities.DeviationRequestInterrupted || read.Outcome["changed"] != float64(1) {
		t.Fatalf("the request reads %q with %v (err %v), want interrupted with one instance changed", read.Status, read.Outcome, err)
	}
	if on := f.stillOn(t, v2.String()); len(on) != 1 {
		t.Fatalf("%d instance(s) are on v2, want the one the run moved before it stopped", len(on))
	}
}
