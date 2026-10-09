package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// expireMigrationRequest records that a request for a migration passed its
// deadline undecided. Its caller holds the request's row and has found it
// still waiting and overdue.
//
// The request's own row is the whole record. A migration's request is for a
// version, not an instance: no ledger row waits on it, and nothing was done
// to any instance whose trail could say that a request about its version
// expired. The request is never deleted, and reads expired — with who asked,
// for what and until when — to every administrator of its organization.
//
// It is what the sweep closes such a request with, what an approval or a
// rejection that finds one overdue closes it with, and what asking again
// closes it with.
func expireMigrationRequest(ctx context.Context, requests repocontracts.DeviationRequestRepository, request entities.DeviationRequest) error {
	_, err := requests.Transition(ctx, request.ID, entities.DeviationRequestPending,
		repocontracts.DeviationRequestChange{Status: entities.DeviationRequestExpired})
	if err != nil {
		return fmt.Errorf("closing request %s as %s: %w", request.ID, entities.DeviationRequestExpired, err)
	}
	return nil
}
