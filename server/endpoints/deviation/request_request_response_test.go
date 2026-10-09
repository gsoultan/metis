package deviation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
)

// wholeRequest is a migration request as a single read answers it: every
// document read, a decision made.
func wholeRequest(instances int) entities.DeviationRequest {
	decided := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	request := entities.DeviationRequest{
		ID: uuid.Must(uuid.NewV7()), CreatedAt: decided.Add(-time.Hour), Project: &entities.Project{ID: uuid.Must(uuid.NewV7())},
		Kind: entities.DeviationRequestMigration, Status: entities.DeviationRequestApplied,
		SourceDefinition: &entities.ProcessDefinition{ID: uuid.Must(uuid.NewV7())},
		TargetDefinition: &entities.ProcessDefinition{ID: uuid.Must(uuid.NewV7())},
		RequestedBy:      "boss", RequestedByID: uuid.Must(uuid.NewV7()), Reason: "the step was retired",
		Command:     map[string]any{"node_mapping": map[string]any{"a": "b"}},
		Plan:        map[string]any{"instances": float64(instances), "because": []any{"“Approve” would be skipped"}},
		Fingerprint: "fp-secret", ApprovedInstances: []uuid.UUID{}, ExpiresAt: decided.Add(72 * time.Hour),
		DecidedBy: "deputy", DecidedByID: uuid.Must(uuid.NewV7()), DecisionReason: "checked", DecidedAt: &decided,
		Outcome: map[string]any{"changed": float64(instances)},
	}
	for range instances {
		request.ApprovedInstances = append(request.ApprovedInstances, uuid.Must(uuid.NewV7()))
	}
	return request
}

func written(t *testing.T, view any) (map[string]any, string) {
	t.Helper()
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return fields, string(raw)
}

func fieldsOf(fields map[string]any) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// A request is read by people: it names who asked and who decided, and
// carries neither account id — nor the fingerprint, which is the server's.
func TestARequestViewNamesPeopleAndCarriesNoAccountId(t *testing.T) {
	t.Parallel()
	request := wholeRequest(2)
	fields, raw := written(t, RequestViewOf(request))
	want := []string{"because", "command", "created_at", "decided_at", "decided_by", "decision_reason", "expires_at", "id", "instances",
		"instances_in_all", "kind", "outcome", "plan", "project_id", "reason", "requested_by", "self_approved",
		"source_definition_id", "status", "target_definition_id"}
	if got := fieldsOf(fields); !slices.Equal(got, want) {
		t.Fatalf("a decided migration request has the fields %v, want %v", got, want)
	}
	if fields["requested_by"] != "boss" || fields["decided_by"] != "deputy" || fields["self_approved"] != false ||
		fields["status"] != "applied" || fields["kind"] != "migration" || fields["instances_in_all"] != float64(2) {
		t.Fatalf("the view: %s", raw)
	}
	for _, withheld := range []string{request.RequestedByID.String(), request.DecidedByID.String(), "fp-secret", "requested_by_id", "decided_by_id", "fingerprint"} {
		if strings.Contains(raw, withheld) {
			t.Errorf("the view carries %s: %s", withheld, raw)
		}
	}
	// Whoever approved their own request is said to have, by account and not
	// by name.
	request.DecidedByID, request.DecidedBy = request.RequestedByID, "renamed"
	if fields, raw := written(t, RequestViewOf(request)); fields["self_approved"] != true {
		t.Errorf("a request its requester approved: %s, want self_approved true", raw)
	}
}

// What a request has nothing of is left out or empty, never null: an id it
// does not have and a decision nobody made are left out; a document that was
// read and holds nothing is an empty object or an empty list.
func TestARequestWithNothingInItIsWrittenWithoutNull(t *testing.T) {
	t.Parallel()
	waive := entities.DeviationRequest{
		ID: uuid.Must(uuid.NewV7()), Project: &entities.Project{ID: uuid.Must(uuid.NewV7())},
		Kind: entities.DeviationRequestInstanceWaive, Status: entities.DeviationRequestPending,
		Instance: &entities.ProcessInstance{ID: uuid.Must(uuid.NewV7())}, RequestedBy: "boss", RequestedByID: uuid.Must(uuid.NewV7()),
		Command: map[string]any{}, Plan: map[string]any{}, ApprovedInstances: []uuid.UUID{}, Outcome: nil,
	}
	fields, raw := written(t, RequestViewOf(waive))
	want := []string{"because", "command", "created_at", "expires_at", "id", "instance_id", "instances", "instances_in_all", "kind",
		"outcome", "plan", "project_id", "reason", "requested_by", "self_approved", "status"}
	if got := fieldsOf(fields); !slices.Equal(got, want) {
		t.Fatalf("a waiting waive has the fields %v, want %v", got, want)
	}
	for _, empty := range []string{`"because":[]`, `"instances":[]`, `"instances_in_all":0`, `"command":{}`, `"plan":{}`, `"outcome":{}`} {
		if !strings.Contains(raw, empty) {
			t.Errorf("the view has no %s: %s", empty, raw)
		}
	}
	if strings.Contains(raw, "null") {
		t.Errorf("the view holds null: %s", raw)
	}
}

// Rulings §14, Ruling 37. The list of instances a migration request covers is
// for reading: at most 200 ids, beside how many there are. A list cut short
// never stands alone as though it were all of them.
func TestARequestListsAtMostTwoHundredInstancesBesideHowManyThereAre(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ covered, listed int }{{0, 0}, {1, 1}, {200, 200}, {201, 200}, {1500, 200}} {
		request := wholeRequest(c.covered)
		view := RequestViewOf(request)
		if len(view.Instances) != c.listed || view.InstancesInAll == nil || *view.InstancesInAll != c.covered {
			t.Fatalf("%d instances covered: %d listed beside a count of %v, want %d beside %d",
				c.covered, len(view.Instances), view.InstancesInAll, c.listed, c.covered)
		}
		for i, id := range view.Instances {
			if id != request.ApprovedInstances[i].String() {
				t.Fatalf("%d instances covered: the id listed at %d is not the one the request holds there", c.covered, i)
			}
		}
		// The request's own list is not what was cut.
		if len(request.ApprovedInstances) != c.covered {
			t.Fatalf("listing %d instances shortened the request's own list to %d", c.covered, len(request.ApprovedInstances))
		}
	}
}

// The queue does not read what was asked, what the requester was shown or
// which instances a request covers. A listed request leaves them out — it
// does not write an empty command, an empty plan or "0 instances", which
// would each read as a fact about the request.
func TestAListedRequestLeavesOutWhatTheQueueDidNotRead(t *testing.T) {
	t.Parallel()
	listed := wholeRequest(3)
	listed.Command, listed.Plan, listed.ApprovedInstances = nil, nil, nil
	fields, raw := written(t, ListedRequestViewOf(listed))
	want := []string{"created_at", "decided_at", "decided_by", "decision_reason", "expires_at", "id", "kind", "outcome", "project_id",
		"reason", "requested_by", "self_approved", "source_definition_id", "status", "target_definition_id"}
	if got := fieldsOf(fields); !slices.Equal(got, want) {
		t.Fatalf("a listed request has the fields %v, want %v", got, want)
	}
	if strings.Contains(raw, "null") {
		t.Errorf("a listed request holds null: %s", raw)
	}
	// Handed a whole request, the queue's view still leaves them out: it is
	// the view that says what a list carries, not what happened to be read.
	if fields, _ := written(t, ListedRequestViewOf(wholeRequest(3))); !slices.Equal(fieldsOf(fields), want) {
		t.Errorf("a whole request, listed, has the fields %v, want %v", fieldsOf(fields), want)
	}
}

// A request whose stored documents no longer open is still answered: a
// decision on it is still told. What could not be read is left out and named
// as unavailable — never written as an empty plan, which would say the
// requester was shown nothing.
func TestARequestWhoseDocumentsNoLongerOpenSaysWhichAreUnavailable(t *testing.T) {
	t.Parallel()
	damaged := wholeRequest(3)
	damaged.Command, damaged.Plan, damaged.ApprovedInstances = nil, nil, nil
	fields, raw := written(t, RequestViewOf(damaged))
	for _, absent := range []string{"command", "plan", "because", "instances", "instances_in_all"} {
		if _, present := fields[absent]; present {
			t.Errorf("a request whose documents no longer open writes %s: %s", absent, raw)
		}
	}
	if got, _ := json.Marshal(fields["unavailable"]); string(got) != `["command","plan","instances"]` {
		t.Errorf("unavailable is %s, want the three documents that could not be read: %s", got, raw)
	}
	if strings.Contains(raw, "null") || fields["status"] != "applied" || fields["decided_by"] != "deputy" {
		t.Errorf("the rest of the request: %s", raw)
	}
	// A whole request says nothing is unavailable by not saying it.
	if fields, raw := written(t, RequestViewOf(wholeRequest(1))); fields["unavailable"] != nil || strings.Contains(raw, "unavailable") {
		t.Errorf("a whole request names something unavailable: %s", raw)
	}
	onlyThePlan := wholeRequest(1)
	onlyThePlan.Plan = nil
	if fields, raw := written(t, RequestViewOf(onlyThePlan)); fields["command"] == nil || fields["instances"] == nil {
		t.Errorf("a request that lost only its plan lost more in the view: %s", raw)
	} else if got, _ := json.Marshal(fields["unavailable"]); string(got) != `["plan"]` {
		t.Errorf("unavailable is %s, want the plan alone", got)
	}
}

// What a caller is told about the request their waive waits on: ids as
// strings, why as a list that is never null.
func TestWhatAWaiveWaitsOnIsWrittenWithoutNull(t *testing.T) {
	t.Parallel()
	id, until := uuid.Must(uuid.NewV7()), time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	fields, raw := written(t, PendingApprovalViewOf(entities.PendingApproval{RequestID: id, Status: entities.DeviationRequestPending, RequestedBy: "boss", ExpiresAt: until}))
	if got := fieldsOf(fields); !slices.Equal(got, []string{"because", "expires_at", "request_id", "requested_by", "status"}) {
		t.Fatalf("the fields are %v", got)
	}
	if fields["request_id"] != id.String() || fields["status"] != "pending_approval" || fields["requested_by"] != "boss" ||
		!strings.Contains(raw, `"because":[]`) || !strings.Contains(raw, `"expires_at":"2026-10-12T09:00:00Z"`) {
		t.Errorf("what a waive waits on: %s", raw)
	}
	said := PendingApprovalViewOf(entities.PendingApproval{Because: []string{"one"}})
	if !slices.Equal(said.Because, []string{"one"}) {
		t.Errorf("because is %v", said.Because)
	}
}

// "Waiting for a second administrator" is not "done": the reply that says a
// request waits is a 202, a first ask and a retry of it alike, and every
// other reply of the route is the 200 it was. pending_approval is left out
// of a reply that waits on nothing.
func TestAReplyThatWaitsIsA202AndEveryOtherIsA200(t *testing.T) {
	t.Parallel()
	row := entities.Deviation{ID: uuid.Must(uuid.NewV7()), Status: entities.DeviationPendingApproval}
	waiting := entities.PendingApproval{RequestID: uuid.Must(uuid.NewV7()), Status: entities.DeviationRequestPending}
	for name, c := range map[string]struct {
		outcome entities.DeviationOutcome
		status  int
	}{
		"a preview":                    {entities.DeviationOutcome{}, http.StatusOK},
		"an apply of a cancel":         {entities.DeviationOutcome{Applied: true, Deviation: &row}, http.StatusOK},
		"a replay of an applied waive": {entities.DeviationOutcome{Applied: true, Replayed: true, Deviation: &row}, http.StatusOK},
		"a waive that waits":           {entities.DeviationOutcome{Deviation: &row, PendingApproval: &waiting}, http.StatusAccepted},
		"the same ask again":           {entities.DeviationOutcome{Replayed: true, Deviation: &row, PendingApproval: &waiting}, http.StatusAccepted},
	} {
		reply := responseOf(c.outcome)
		fields, raw := written(t, reply)
		_, says := fields["pending_approval"]
		if reply.StatusCode() != c.status || says != (c.status == http.StatusAccepted) {
			t.Errorf("%s: status %d, pending_approval written %v (%s); want %d", name, reply.StatusCode(), says, raw, c.status)
		}
	}
}

// askedOfRequests is a facade that only reads and decides requests, and
// keeps what it was asked. Anything else an endpoint called would panic.
type askedOfRequests struct {
	services.ServiceFacade
	asked   []string
	queries []entities.DeviationRequestQuery
	reasons []string
	err     error
	request entities.DeviationRequest
	outcome entities.DeviationRequestOutcome
}

func (s *askedOfRequests) ListDeviationRequests(_ context.Context, query entities.DeviationRequestQuery) ([]entities.DeviationRequest, int64, error) {
	s.asked, s.queries = append(s.asked, "list"), append(s.queries, query)
	if s.err != nil {
		return nil, 0, s.err
	}
	return []entities.DeviationRequest{s.request}, 41, nil
}

func (s *askedOfRequests) GetDeviationRequest(_ context.Context, id uuid.UUID) (entities.DeviationRequest, error) {
	s.asked = append(s.asked, "get "+id.String())
	return s.request, s.err
}

func (s *askedOfRequests) ApproveDeviationRequest(_ context.Context, id uuid.UUID, reason string) (entities.DeviationRequestOutcome, error) {
	s.asked, s.reasons = append(s.asked, "approve "+id.String()), append(s.reasons, reason)
	return s.outcome, s.err
}

func (s *askedOfRequests) RejectDeviationRequest(_ context.Context, id uuid.UUID, reason string) (entities.DeviationRequest, error) {
	s.asked, s.reasons = append(s.asked, "reject "+id.String()), append(s.reasons, reason)
	return s.request, s.err
}

// An address that names no request is the caller's to fix, and the service is
// not asked about it. Whatever the service refuses travels in the reply's
// Failed, in its own class — never as a reply that went well.
func TestARequestRouteRefusesWhatIsNoIdAndCarriesTheServicesRefusal(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7())
	type failer interface{ Failed() error }
	call := func(svc *askedOfRequests, which, rawID string) error {
		t.Helper()
		var reply any
		var err error
		switch which {
		case "get":
			reply, err = MakeGetDeviationRequestEndpoint(svc)(context.Background(), GetDeviationRequestRequest{ID: rawID})
		case "approve":
			reply, err = MakeApproveDeviationRequestEndpoint(svc)(context.Background(), DecideDeviationRequestRequest{ID: rawID, Reason: "why"})
		case "reject":
			reply, err = MakeRejectDeviationRequestEndpoint(svc)(context.Background(), DecideDeviationRequestRequest{ID: rawID, Reason: "why"})
		}
		if err != nil {
			t.Fatalf("%s answered an error beside its reply: %v", which, err)
		}
		return reply.(failer).Failed()
	}
	for _, which := range []string{"get", "approve", "reject"} {
		svc := &askedOfRequests{}
		for _, notAnID := range []string{"not-an-id", "", " ", "0192e7a1-0000-7000-8000-00000000000"} {
			err := call(svc, which, notAnID)
			if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "is not a valid identifier") {
				t.Errorf("%s of the id %q: %v, want it refused as no identifier", which, notAnID, err)
			}
		}
		if len(svc.asked) != 0 {
			t.Errorf("%s asked the service about an id that is not one: %v", which, svc.asked)
		}
		for _, refusal := range []error{apierr.Forbiddenf("no"), apierr.Invalidf("no"), apierr.ErrNotFound, errors.New("the server could not")} {
			svc := &askedOfRequests{err: refusal}
			if err := call(svc, which, id.String()); !errors.Is(err, refusal) {
				t.Errorf("%s the service refused with %v failed with %v", which, refusal, err)
			}
			if len(svc.asked) != 1 || svc.asked[0] != which+" "+id.String() {
				t.Errorf("%s asked the service %v", which, svc.asked)
			}
		}
	}
	svc := &askedOfRequests{}
	if err := call(svc, "approve", id.String()); err != nil || len(svc.reasons) != 1 || svc.reasons[0] != "why" {
		t.Errorf("an approval's reason reached the service as %v (err %v)", svc.reasons, err)
	}
}

// The queue's endpoint passes on what it was asked, and lists requests as
// the queue reads them: without what only the single read has.
func TestTheQueueEndpointPassesOnWhatItWasAsked(t *testing.T) {
	t.Parallel()
	project := uuid.Must(uuid.NewV7())
	svc := &askedOfRequests{request: wholeRequest(2)}
	reply, err := MakeListDeviationRequestsEndpoint(svc)(context.Background(),
		ListDeviationRequestsRequest{Status: "expired", ProjectID: project.String(), Page: 3, PageSize: 7})
	listed, ok := reply.(ListDeviationRequestsResponse)
	if err != nil || !ok || listed.Err != nil || listed.Total != 41 || len(listed.Requests) != 1 {
		t.Fatalf("the queue: %+v, %v", reply, err)
	}
	if got := svc.queries[0]; got.Status != entities.DeviationRequestExpired || got.Project == nil || got.Project.ID != project || got.Page != 3 || got.PageSize != 7 {
		t.Errorf("the service was asked for %+v", got)
	}
	if view := listed.Requests[0]; view.Command != nil || view.Plan != nil || view.Instances != nil || view.InstancesInAll != nil || view.Because != nil {
		t.Errorf("a listed request carries what the queue does not read: %+v", view)
	}

	// No project named asks for every project the caller's organization has.
	if _, err := MakeListDeviationRequestsEndpoint(svc)(context.Background(), ListDeviationRequestsRequest{}); err != nil ||
		svc.queries[1].Project != nil || svc.queries[1].Status != "" {
		t.Errorf("the queue with nothing said asked the service for %+v (err %v)", svc.queries[1], err)
	}
	// A project that is no id is refused before the service is asked.
	reply, err = MakeListDeviationRequestsEndpoint(svc)(context.Background(), ListDeviationRequestsRequest{ProjectID: "not-an-id"})
	if refused := reply.(ListDeviationRequestsResponse).Err; err != nil || !errors.Is(refused, apierr.ErrInvalidArgument) ||
		refused.Error() != apierr.Invalidf(`project id "not-an-id" is not a valid identifier`).Error() || len(svc.queries) != 2 {
		t.Errorf("a project that is no id: %v (%v), after %d queries", refused, err, len(svc.queries))
	}
	// An empty page is a list, not null.
	if _, raw := written(t, ListDeviationRequestsResponse{Requests: listedViews(nil)}); !strings.Contains(raw, `"requests":[]`) {
		t.Errorf("an empty page is written %s", raw)
	}
}

// An approval answers what was done: the request as the decision left it,
// whether it applied, the ledger's record and the plan it was applied from.
func TestAnApprovalAnswersTheRequestTheRecordAndThePlan(t *testing.T) {
	t.Parallel()
	request := wholeRequest(0)
	row := entities.Deviation{ID: uuid.Must(uuid.NewV7()), Status: entities.DeviationApplied, Actor: "boss", ApprovedBy: "deputy",
		ActorID: request.RequestedByID, ApprovedByID: request.DecidedByID}
	plan := entities.DeviationPlan{NodeID: "step", RequiresSecondApprover: true}
	svc := &askedOfRequests{outcome: entities.DeviationRequestOutcome{Request: request, Applied: true, Deviation: &row, WaivePlan: &plan}}
	reply, err := MakeApproveDeviationRequestEndpoint(svc)(context.Background(), DecideDeviationRequestRequest{ID: request.ID.String()})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	fields, raw := written(t, reply)
	if got := fieldsOf(fields); !slices.Equal(got, []string{"applied", "deviation", "plan", "request"}) {
		t.Fatalf("an approval has the fields %v: %s", got, raw)
	}
	if fields["applied"] != true || fields["deviation"].(map[string]any)["approved_by"] != "deputy" ||
		fields["plan"].(map[string]any)["node_id"] != "step" || fields["request"].(map[string]any)["id"] != request.ID.String() {
		t.Errorf("the approval: %s", raw)
	}
	for _, withheld := range []string{request.RequestedByID.String(), request.DecidedByID.String()} {
		if strings.Contains(raw, withheld) {
			t.Errorf("the approval carries an account id: %s", raw)
		}
	}
	// A rejection answers the request and nothing else.
	rejected, err := MakeRejectDeviationRequestEndpoint(&askedOfRequests{request: request})(context.Background(),
		DecideDeviationRequestRequest{ID: request.ID.String(), Reason: "no"})
	if fields, raw := written(t, rejected); err != nil || !slices.Equal(fieldsOf(fields), []string{"request"}) {
		t.Errorf("a rejection has the fields %v (%v): %s", fieldsOf(fields), err, raw)
	}
}
