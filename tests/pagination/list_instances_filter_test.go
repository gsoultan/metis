package pagination_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/endpoints/process"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	processhttp "github.com/gsoultan/metis/server/transports/https/processes"
)

// The instance list can now narrow by state in the database rather than in the
// browser. These cover the two ways that goes wrong at the edge: a filter the
// transport never reads, so the feature is unreachable; and a filter the
// endpoint accepts without understanding, so it widens instead of narrowing.

// A filter the decoder never lifts off the query string is a filter that does
// not exist over HTTP — which is exactly what happened to this endpoint's
// paging, and is why the test beside this one exists.
func TestListInstancesReadsTheFilterFromTheQuery(t *testing.T) {
	var got process.ListInstancesRequest
	mux := http.NewServeMux()
	processhttp.RegisterHandlers(mux, captureInstances(&got), nil)

	projectID := uuid.Must(uuid.NewV7()).String()
	definitionID := uuid.Must(uuid.NewV7()).String()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
		"/api/v1/instances?project_id="+projectID+"&status=failed&definition_id="+definitionID, nil)
	mux.ServeHTTP(httptest.NewRecorder(), request)

	if got.Status != "failed" {
		t.Errorf("status = %q, want %q — the query parameter is not reaching the endpoint", got.Status, "failed")
	}
	if got.DefinitionID != definitionID {
		t.Errorf("definition_id = %q, want %q", got.DefinitionID, definitionID)
	}
}

// An unrecognised status must be refused, not dropped.
//
// This is the one that matters. A repository predicate built from an unknown
// status matches no row, and an endpoint that quietly drops it instead answers
// "show me everything that failed" with every instance in the project. Both are
// wrong; the second is worse, because a full list of healthy runs reads as
// "nothing is wrong", and that is the answer this page exists to give correctly.
func TestListInstancesRefusesAStatusItDoesNotKnow(t *testing.T) {
	ep := process.MakeListInstancesEndpoint(nil)

	for _, status := range []string{"broken", "active'; DROP TABLE process_instances; --", "0"} {
		got, err := ep(t.Context(), process.ListInstancesRequest{
			ProjectID: uuid.Must(uuid.NewV7()).String(),
			Status:    status,
		})
		if err != nil {
			t.Fatalf("the endpoint returned a transport error: %v", err)
		}
		res, ok := got.(process.ListInstancesResponse)
		if !ok {
			t.Fatalf("unexpected response type %T", got)
		}
		if res.Err == nil {
			t.Fatalf("status %q was accepted; the listing would have widened to every state", status)
		}
		if !strings.Contains(res.Err.Error(), "not a process state") {
			t.Errorf("the error does not explain what was wrong: %v", res.Err)
		}
		if len(res.Instances) != 0 {
			t.Errorf("instances were returned for an unusable status: %d", len(res.Instances))
		}
	}
}

// Every state the engine actually writes has to be accepted, or the chips the
// list offers are controls that return an error when pressed.
func TestListInstancesAcceptsEveryRealStatus(t *testing.T) {
	ep := process.MakeListInstancesEndpoint(stubFacade{})

	for _, status := range []string{"active", "completed", "suspended", "failed", "FAILED", " failed "} {
		got, err := ep(t.Context(), process.ListInstancesRequest{
			ProjectID: uuid.Must(uuid.NewV7()).String(),
			Status:    status,
		})
		if err != nil {
			t.Fatalf("the endpoint returned a transport error: %v", err)
		}
		res, ok := got.(process.ListInstancesResponse)
		if !ok {
			t.Fatalf("unexpected response type %T", got)
		}
		if res.Err != nil {
			t.Errorf("status %q was refused: %v", status, res.Err)
		}
	}
}

// The validated status is what reaches the service, normalised — otherwise the
// chips work and the query they run does not.
func TestListInstancesPassesTheFilterDown(t *testing.T) {
	stub := &recordingFacade{}
	definitionID := uuid.Must(uuid.NewV7())

	_, err := process.MakeListInstancesEndpoint(stub)(t.Context(), process.ListInstancesRequest{
		ProjectID:    uuid.Must(uuid.NewV7()).String(),
		Status:       " Failed ",
		DefinitionID: definitionID.String(),
	})
	if err != nil {
		t.Fatalf("the endpoint returned a transport error: %v", err)
	}

	if stub.listed.Status != models.ProcessFailed {
		t.Errorf("the service saw status %q, want %q", stub.listed.Status, models.ProcessFailed)
	}
	if stub.listed.DefinitionID != definitionID {
		t.Errorf("the service saw definition %v, want %v", stub.listed.DefinitionID, definitionID)
	}
	// The counts describe every state, so the chip for one state cannot be the
	// only one with a number once that state is selected.
	if stub.counted.Status != "" {
		t.Errorf("the status filter reached the counts as %q; it must not, or choosing a state zeroes the others",
			stub.counted.Status)
	}
	if stub.counted.DefinitionID != definitionID {
		t.Errorf("the counts saw definition %v, want %v — they must describe the same rows the page is drawn from",
			stub.counted.DefinitionID, definitionID)
	}
}

// A malformed definition id must narrow nothing, for the same reason a
// malformed project id must: a filter that cannot be honoured and is dropped
// anyway is a filter that widens.
func TestListInstancesRefusesAMalformedDefinitionID(t *testing.T) {
	ep := process.MakeListInstancesEndpoint(nil)

	got, err := ep(t.Context(), process.ListInstancesRequest{
		ProjectID:    uuid.Must(uuid.NewV7()).String(),
		DefinitionID: "not-a-uuid",
	})
	if err != nil {
		t.Fatalf("the endpoint returned a transport error: %v", err)
	}
	res, ok := got.(process.ListInstancesResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", got)
	}
	if res.Err == nil {
		t.Fatal("a malformed definition id was accepted; the listing would not have been narrowed to a process")
	}
	if !strings.Contains(res.Err.Error(), "not a valid identifier") {
		t.Errorf("the error does not explain what was wrong: %v", res.Err)
	}
}

// The chips are drawn in the order the server lists them, so that order has to
// be fixed. Go randomises map iteration, and counts served straight out of one
// would reshuffle somebody's filter row on every poll.
func TestListInstancesReportsStatusCountsInAStableOrder(t *testing.T) {
	stub := &recordingFacade{counts: map[models.ProcessStatus]int64{
		models.ProcessCompleted: 4120,
		models.ProcessFailed:    12,
		models.ProcessActive:    37,
		models.ProcessSuspended: 2,
	}}
	ep := process.MakeListInstancesEndpoint(stub)

	var first []string
	for range 20 {
		got, err := ep(t.Context(), process.ListInstancesRequest{ProjectID: uuid.Must(uuid.NewV7()).String()})
		if err != nil {
			t.Fatalf("the endpoint returned a transport error: %v", err)
		}
		res, ok := got.(process.ListInstancesResponse)
		if !ok {
			t.Fatalf("unexpected response type %T", got)
		}
		order := make([]string, 0, len(res.StatusCounts))
		for _, count := range res.StatusCounts {
			order = append(order, count.Status)
		}
		if first == nil {
			first = order
			continue
		}
		if strings.Join(order, ",") != strings.Join(first, ",") {
			t.Fatalf("the counts came back in a different order: %v then %v", first, order)
		}
	}
	if strings.Join(first, ",") != "active,completed,suspended,failed" {
		t.Errorf("counts = %v, want the declared status order", first)
	}
}

// stubFacade answers a list request with nothing, so a test about validation
// does not need a database. The embedded interface is nil on purpose: a method
// this test did not expect to be called panics rather than returning a zero
// value that looks like a pass.
type stubFacade struct{ services.ServiceFacade }

func (stubFacade) ListInstancesPaged(
	_ context.Context,
	_ uuid.UUID,
	_ repocontracts.InstanceFilter,
	page repocontracts.Pagination,
) (repocontracts.Page[entities.ProcessInstance], error) {
	return repocontracts.NewPage([]entities.ProcessInstance{}, 0, page), nil
}

func (stubFacade) CountInstancesByStatus(
	_ context.Context,
	_ uuid.UUID,
	_ repocontracts.InstanceFilter,
) (map[models.ProcessStatus]int64, error) {
	return map[models.ProcessStatus]int64{}, nil
}

func (stubFacade) InstanceAttention(
	_ context.Context,
	_ uuid.UUID,
	_ repocontracts.InstanceFilter,
	_ []uuid.UUID,
) (entities.InstanceAttention, error) {
	return entities.InstanceAttention{}, nil
}

// recordingFacade keeps what the endpoint asked it for.
type recordingFacade struct {
	services.ServiceFacade
	listed          repocontracts.InstanceFilter
	counted         repocontracts.InstanceFilter
	attentionFilter repocontracts.InstanceFilter
	counts          map[models.ProcessStatus]int64
}

func (f *recordingFacade) ListInstancesPaged(
	_ context.Context,
	_ uuid.UUID,
	filter repocontracts.InstanceFilter,
	page repocontracts.Pagination,
) (repocontracts.Page[entities.ProcessInstance], error) {
	f.listed = filter
	return repocontracts.NewPage([]entities.ProcessInstance{}, 0, page), nil
}

func (f *recordingFacade) CountInstancesByStatus(
	_ context.Context,
	_ uuid.UUID,
	filter repocontracts.InstanceFilter,
) (map[models.ProcessStatus]int64, error) {
	f.counted = filter
	return f.counts, nil
}

func (f *recordingFacade) InstanceAttention(
	_ context.Context,
	_ uuid.UUID,
	filter repocontracts.InstanceFilter,
	_ []uuid.UUID,
) (entities.InstanceAttention, error) {
	f.attentionFilter = filter
	return entities.InstanceAttention{}, nil
}
