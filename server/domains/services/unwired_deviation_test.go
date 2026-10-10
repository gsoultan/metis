package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A facade put together without the deviation service — the ledger's reads,
// the in-place command, the requests for a second administrator — answers
// every one of its methods with a plain error, at once. It used to hold a nil
// interface there: a call through it is a nil dereference, which a test has
// been seen not to return from at all.
func TestAFacadeWiredWithoutTheDeviationServiceRefusesEveryMethodOfIt(t *testing.T) {
	facade := NewService(ServiceParams{})
	ctx := context.Background()
	id := uuid.Must(uuid.NewV7())
	calls := map[string]func() error{
		"ListInstanceDeviations": func() error { _, err := facade.ListInstanceDeviations(ctx, id); return err },
		"DeviateInstance": func() error {
			_, err := facade.DeviateInstance(ctx, entities.DeviationCommand{InstanceID: id, Kind: entities.DeviationWaive})
			return err
		},
		"ApproveDeviationRequest": func() error { _, err := facade.ApproveDeviationRequest(ctx, id, "agreed"); return err },
		"RejectDeviationRequest":  func() error { _, err := facade.RejectDeviationRequest(ctx, id, "no"); return err },
		"ListDeviationRequests": func() error {
			_, _, err := facade.ListDeviationRequests(ctx, entities.DeviationRequestQuery{})
			return err
		},
		"GetDeviationRequest": func() error { _, err := facade.GetDeviationRequest(ctx, id); return err },
		// The one the server's own schedule calls, every few minutes.
		"SweepDeviationRequests": func() error {
			_, err := facade.SweepDeviationRequests(entities.WithSystemContext(ctx), time.Now())
			return err
		},
	}
	for name, call := range calls {
		answered := make(chan error, 1)
		go func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					answered <- errors.New("it panicked")
				}
			}()
			answered <- call()
		}()
		select {
		case err := <-answered:
			if err == nil || err.Error() == "it panicked" {
				t.Errorf("%s on a facade with no deviation service: %v, want an error that says the server was wired without it", name, err)
				continue
			}
			for _, class := range []error{apierr.ErrInvalidArgument, apierr.ErrNotFound, apierr.ErrForbidden} {
				if errors.Is(err, class) {
					t.Errorf("%s is answered as %v: %v; the wiring is the server's fault", name, class, err)
				}
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s on a facade with no deviation service did not return", name)
		}
	}
}
