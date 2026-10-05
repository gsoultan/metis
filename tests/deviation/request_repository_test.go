package deviation_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

func (h *deviationHarness) sampleRequest(instanceID uuid.UUID, fingerprint string) entities.DeviationRequest {
	return entities.DeviationRequest{
		ID: uuid.Must(uuid.NewV7()), Project: &entities.Project{ID: h.projID},
		Kind: entities.DeviationRequestInstanceWaive, Status: entities.DeviationRequestPending,
		Instance: &entities.ProcessInstance{ID: instanceID}, RequestedBy: "ana", RequestedByID: uuid.Must(uuid.NewV7()),
		Reason: "the approver has left", Fingerprint: fingerprint,
		Command:   map[string]any{"node_id": "step", "outputs": map[string]any{"amount": "eighty-thousand"}},
		Plan:      map[string]any{"node_id": "step", "because": []string{"“Approve” would be waived"}},
		ExpiresAt: time.Now().Add(72 * time.Hour),
	}
}

func (h *deviationHarness) createRequest(ctx context.Context, r entities.DeviationRequest) (entities.DeviationRequest, error) {
	var out entities.DeviationRequest
	err := h.repo.UnitOfWork().Do(ctx, func(tx context.Context) error {
		var err error
		out, err = h.repo.DeviationRequest().Create(tx, r)
		return err
	})
	return out, err
}

func (h *deviationHarness) mustCreateRequest(t *testing.T, r entities.DeviationRequest) entities.DeviationRequest {
	t.Helper()
	written, err := h.createRequest(h.tenantContext(), r)
	if err != nil {
		t.Fatalf("create the request %s: %v", r.Fingerprint, err)
	}
	return written
}

// transition moves a request inside a unit of work of its own.
func (h *deviationHarness) transition(ctx context.Context, id uuid.UUID, from entities.DeviationRequestStatus, change repocontracts.DeviationRequestChange) (entities.DeviationRequest, error) {
	var out entities.DeviationRequest
	err := h.repo.UnitOfWork().Do(ctx, func(tx context.Context) error {
		var err error
		out, err = h.repo.DeviationRequest().Transition(tx, id, from, change)
		return err
	})
	return out, err
}

// budi is the administrator who decides in these tests.
var budi = uuid.Must(uuid.NewV7())

// decisionTo is budi moving a request to status, now.
func decisionTo(status entities.DeviationRequestStatus) repocontracts.DeviationRequestChange {
	return repocontracts.DeviationRequestChange{
		Status: status, DecidedBy: "budi", DecidedByID: budi, DecisionReason: "seen and decided", DecidedAt: time.Now(),
	}
}

// movesTo is the way a request reaches each status: by the moves the product
// makes, never by a row written at that status.
var movesTo = map[entities.DeviationRequestStatus][]entities.DeviationRequestStatus{
	entities.DeviationRequestPending:     nil,
	entities.DeviationRequestApproved:    {entities.DeviationRequestApproved},
	entities.DeviationRequestApplied:     {entities.DeviationRequestApplied},
	entities.DeviationRequestInterrupted: {entities.DeviationRequestApproved, entities.DeviationRequestInterrupted},
	entities.DeviationRequestStale:       {entities.DeviationRequestStale},
	entities.DeviationRequestRejected:    {entities.DeviationRequestRejected},
	entities.DeviationRequestExpired:     {entities.DeviationRequestExpired},
}

// requestAt writes r and walks it to status.
func (h *deviationHarness) requestAt(t *testing.T, r entities.DeviationRequest, status entities.DeviationRequestStatus) entities.DeviationRequest {
	t.Helper()
	request := h.mustCreateRequest(t, r)
	for _, next := range movesTo[status] {
		moved, err := h.transition(h.tenantContext(), request.ID, request.Status, decisionTo(next))
		if err != nil {
			t.Fatalf("move the request %s from %s to %s: %v", r.Fingerprint, request.Status, next, err)
		}
		request = moved
	}
	if request.Status != status {
		t.Fatalf("the request %s is %s, want %s", r.Fingerprint, request.Status, status)
	}
	return request
}

// storedRequest is a request's row as the table holds it.
type storedRequest struct {
	status, command, plan, approvedInstances, outcome string
	liveKey                                           sql.NullString
}

func (h *deviationHarness) storedRequest(t *testing.T, id uuid.UUID) storedRequest {
	t.Helper()
	var row storedRequest
	if err := h.db.Raw(`SELECT status, command::text, plan::text, approved_instances::text, outcome::text, live_key
		FROM deviation_requests WHERE id = ?`, id).Row().
		Scan(&row.status, &row.command, &row.plan, &row.approvedInstances, &row.outcome, &row.liveKey); err != nil {
		t.Fatalf("read the stored request: %v", err)
	}
	return row
}

func (h *deviationHarness) requestCountIn(t *testing.T, projectID uuid.UUID) int {
	t.Helper()
	var n int
	if err := h.db.Raw(`SELECT count(*) FROM deviation_requests WHERE project_id = ?`, projectID).Row().Scan(&n); err != nil {
		t.Fatalf("count the requests: %v", err)
	}
	return n
}

// inAnotherOrganization is a second organization with a project of its own,
// reached through the same repository.
func (h *deviationHarness) inAnotherOrganization(t *testing.T, name string) *deviationHarness {
	t.Helper()
	org, err := h.svc.CreateOrganization(context.Background(), name, "")
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	other := *h
	other.orgID, other.deployed = org.ID, 0
	project, err := h.svc.CreateProject(other.tenantContext(), org.ID, name+" Project", "")
	if err != nil {
		t.Fatalf("create the project of %s: %v", name, err)
	}
	other.projID = project.ID
	return &other
}

// isTheWritersMistake reports whether err is a plain error: not one a client
// is told is theirs, and not one of the answers a caller acts on.
func isTheWritersMistake(err error) bool {
	if err == nil {
		return false
	}
	for _, known := range []error{
		apierr.ErrInvalidArgument, apierr.ErrNotFound, apierr.ErrForbidden,
		repocontracts.ErrDeviationRequestDecided, repocontracts.ErrDeviationRequestAlreadyWaiting,
		repocontracts.ErrDeviationRequestOutsideTransaction, repocontracts.ErrDeviationRowDecided,
		repocontracts.ErrDeviationOutsideTransaction,
	} {
		if errors.Is(err, known) {
			return false
		}
	}
	return true
}

// A request is written with the change that asks for it, sealed like the
// ledger, scoped to its organization, and decided exactly once.
func TestARequestIsWrittenInATransactionSealedScopedAndDecidedOnce(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	if _, err := h.repo.DeviationRequest().Create(h.tenantContext(), h.sampleRequest(instanceID, "dv1-a")); !errors.Is(err, repocontracts.ErrDeviationRequestOutsideTransaction) {
		t.Fatalf("a write outside a transaction: %v", err)
	}
	if n := h.requestCountIn(t, h.projID); n != 0 {
		t.Fatalf("a refused write left %d request(s)", n)
	}
	written, err := h.createRequest(h.tenantContext(), h.sampleRequest(instanceID, "dv1-a"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	stored := h.storedRequest(t, written.ID)
	if strings.Contains(stored.command, "eighty-thousand") {
		t.Fatalf("the command is stored in clear: %s", stored.command)
	}
	if strings.Contains(stored.plan, "would be waived") {
		t.Fatalf("the plan is stored in clear: %s", stored.plan)
	}
	got, err := h.repo.DeviationRequest().Get(h.tenantContext(), written.ID)
	if err != nil || got.Command["outputs"].(map[string]any)["amount"] != "eighty-thousand" || got.Status != entities.DeviationRequestPending {
		t.Fatalf("read back %+v, %v", got, err)
	}
	if because := got.Because(); len(because) != 1 || because[0] != "“Approve” would be waived" {
		t.Fatalf("the plan reads back saying %q", because)
	}

	// A second live request for the same thing is the one refusal here a
	// caller meets and has to recognise: a state, not the writer's mistake
	// and not something the client typed wrongly.
	_, err = h.createRequest(h.tenantContext(), h.sampleRequest(instanceID, "dv1-a"))
	if !errors.Is(err, repocontracts.ErrDeviationRequestAlreadyWaiting) {
		t.Fatalf("a second live request for the same fingerprint: %v, want ErrDeviationRequestAlreadyWaiting", err)
	}
	if errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("the repository classed the refusal for the client (%v); the service says what it means", err)
	}
	other, _ := h.svc.CreateOrganization(context.Background(), "Request Other Org", "")
	elsewhere := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: other.ID.String()})
	if _, err := h.repo.DeviationRequest().Get(elsewhere, written.ID); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("another organization reads the request: %v", err)
	}

	decide := func(from entities.DeviationRequestStatus) error {
		return h.repo.UnitOfWork().Do(h.tenantContext(), func(tx context.Context) error {
			_, err := h.repo.DeviationRequest().Transition(tx, written.ID, from, repocontracts.DeviationRequestChange{
				Status: entities.DeviationRequestRejected, DecidedBy: "budi", DecidedByID: uuid.Must(uuid.NewV7()),
				DecisionReason: "not needed", DecidedAt: time.Now()})
			return err
		})
	}
	if err := decide(entities.DeviationRequestPending); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if err := decide(entities.DeviationRequestPending); !errors.Is(err, repocontracts.ErrDeviationRequestDecided) {
		t.Fatalf("a second decision: %v, want ErrDeviationRequestDecided", err)
	}
	if _, err := h.createRequest(h.tenantContext(), h.sampleRequest(instanceID, "dv1-a")); err != nil {
		t.Fatalf("asking again after a rejection was refused: %v", err)
	}
}

// definitionOf is the version an instance runs on.
func (h *deviationHarness) definitionOf(t *testing.T, instanceID uuid.UUID) uuid.UUID {
	t.Helper()
	var id string
	if err := h.db.Raw(`SELECT definition_id::text FROM process_instances WHERE id = ?`, instanceID).Row().Scan(&id); err != nil {
		t.Fatalf("read the instance's version: %v", err)
	}
	return uuid.MustParse(id)
}

// sameRequest compares two requests field by field, instants as instants.
func sameRequest(t *testing.T, what string, got, want entities.DeviationRequest) {
	t.Helper()
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("%s: the deadline reads back as %v, want %v", what, got.ExpiresAt, want.ExpiresAt)
	}
	if (got.DecidedAt == nil) != (want.DecidedAt == nil) || (got.DecidedAt != nil && !got.DecidedAt.Equal(*want.DecidedAt)) {
		t.Errorf("%s: decided at reads back as %v, want %v", what, got.DecidedAt, want.DecidedAt)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("%s: the request has no creation or update time", what)
	}
	got.ExpiresAt, want.ExpiresAt = time.Time{}, time.Time{}
	got.DecidedAt, want.DecidedAt = nil, nil
	got.CreatedAt, got.UpdatedAt, want.CreatedAt, want.UpdatedAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: the request reads back as\n%+v\nwant\n%+v", what, got, want)
	}
}

// Every column the repository writes comes back as it went in — on the
// insert, through each read, and after a decision.
func TestARequestReadsBackExactlyAsItWasWritten(t *testing.T) {
	h := newDeviationHarness(t)
	first := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	second := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	want := entities.DeviationRequest{
		ID: uuid.Must(uuid.NewV7()), Project: &entities.Project{ID: h.projID},
		Kind: entities.DeviationRequestMigration, Status: entities.DeviationRequestPending,
		Instance:         &entities.ProcessInstance{ID: first},
		SourceDefinition: &entities.ProcessDefinition{ID: h.definitionOf(t, first)},
		TargetDefinition: &entities.ProcessDefinition{ID: h.definitionOf(t, second)},
		RequestedBy:      "ana ", RequestedByID: uuid.Must(uuid.NewV7()),
		Reason:      "the step was retired",
		Command:     map[string]any{"node_mapping": map[string]any{"approve": "review"}, "instances": []any{first.String()}},
		Plan:        map[string]any{"instances": float64(2), "because": []any{"“Approve” is skipped"}},
		Fingerprint: "mf1-exact", ApprovedInstances: []uuid.UUID{first, second},
		ExpiresAt: time.Date(2031, 10, 3, 9, 30, 0, 0, time.UTC),
		Outcome:   map[string]any{},
	}
	written, err := h.createRequest(h.tenantContext(), want)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	sameRequest(t, "the row the write answers", written, want)
	got, err := h.repo.DeviationRequest().Get(h.tenantContext(), want.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	sameRequest(t, "the row read back", got, want)
	found, ok, err := h.repo.DeviationRequest().FindLive(h.tenantContext(), h.projID, "mf1-exact")
	if err != nil || !ok {
		t.Fatalf("the live request of the fingerprint: %v, %v", ok, err)
	}
	sameRequest(t, "the live request", found, want)
	if err := h.repo.UnitOfWork().Do(h.tenantContext(), func(tx context.Context) error {
		locked, err := h.repo.DeviationRequest().GetForUpdate(tx, want.ID)
		if err == nil {
			sameRequest(t, "the row read under its lock", locked, want)
		}
		return err
	}); err != nil {
		t.Fatalf("get for update: %v", err)
	}
	if _, err := h.repo.DeviationRequest().GetForUpdate(h.tenantContext(), want.ID); !errors.Is(err, repocontracts.ErrDeviationRequestOutsideTransaction) {
		t.Fatalf("a lock taken outside a transaction holds nothing: %v, want ErrDeviationRequestOutsideTransaction", err)
	}

	decided := time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)
	want.Status, want.DecidedBy, want.DecidedByID = entities.DeviationRequestApproved, "budi", uuid.Must(uuid.NewV7())
	want.DecisionReason, want.DecidedAt = "seen with the process owner", &decided
	want.Outcome = map[string]any{"changed": float64(2), "passed_over": float64(0)}
	moved, err := h.transition(h.tenantContext(), want.ID, entities.DeviationRequestPending, repocontracts.DeviationRequestChange{
		Status: want.Status, DecidedBy: want.DecidedBy, DecidedByID: want.DecidedByID,
		DecisionReason: want.DecisionReason, DecidedAt: decided, Outcome: want.Outcome,
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	sameRequest(t, "the row the decision answers", moved, want)
	got, err = h.repo.DeviationRequest().Get(h.tenantContext(), want.ID)
	if err != nil {
		t.Fatalf("get the decided request: %v", err)
	}
	sameRequest(t, "the decided row read back", got, want)
	if !got.UpdatedAt.After(written.UpdatedAt) {
		t.Errorf("the decision left updated_at at %v; the row was written at %v", got.UpdatedAt, written.UpdatedAt)
	}
}

// The four documents of a request are NOT NULL and have no default: a request
// with nothing to carry is stored with an empty object or list in each, never
// refused and never a NULL, and reads back as empty rather than absent.
func TestARequestWithNothingToCarryIsStoredWithEmptyDocuments(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	for name, empty := range map[string]map[string]any{"nil": nil, "empty": {}} {
		r := h.sampleRequest(instanceID, "dv1-bare-"+name)
		r.Command, r.Plan, r.Outcome, r.ApprovedInstances, r.Reason = empty, empty, empty, nil, ""
		written, err := h.createRequest(h.tenantContext(), r)
		if err != nil {
			t.Fatalf("%s documents: the request was refused: %v", name, err)
		}
		stored := h.storedRequest(t, written.ID)
		if stored.command != "{}" || stored.plan != "{}" || stored.outcome != "{}" || stored.approvedInstances != "[]" {
			t.Errorf("%s documents: stored as command=%s plan=%s outcome=%s approved_instances=%s; want {} {} {} []",
				name, stored.command, stored.plan, stored.outcome, stored.approvedInstances)
		}
		for what, got := range map[string]entities.DeviationRequest{"the write": written, "a read": h.mustGetRequest(t, written.ID)} {
			if got.Command == nil || len(got.Command) != 0 || got.Plan == nil || len(got.Plan) != 0 ||
				got.Outcome == nil || len(got.Outcome) != 0 || got.ApprovedInstances == nil || len(got.ApprovedInstances) != 0 {
				t.Errorf("%s documents: %s answers command=%v plan=%v outcome=%v approved=%v; want each empty and none nil",
					name, what, got.Command, got.Plan, got.Outcome, got.ApprovedInstances)
			}
		}

		// A decision that says nothing of the outcome, or says there is
		// none, leaves an empty object there — never a NULL.
		rejection := decisionTo(entities.DeviationRequestRejected)
		rejection.Outcome = empty
		moved, err := h.transition(h.tenantContext(), written.ID, entities.DeviationRequestPending, rejection)
		if err != nil {
			t.Fatalf("%s documents: reject: %v", name, err)
		}
		if after := h.storedRequest(t, written.ID); after.outcome != "{}" || moved.Outcome == nil {
			t.Errorf("%s documents: after a decision the outcome is stored as %s and answered as %v", name, after.outcome, moved.Outcome)
		}
	}
}

func (h *deviationHarness) mustGetRequest(t *testing.T, id uuid.UUID) entities.DeviationRequest {
	t.Helper()
	got, err := h.repo.DeviationRequest().Get(h.tenantContext(), id)
	if err != nil {
		t.Fatalf("read the request: %v", err)
	}
	return got
}

// A request the code that asks for it got wrong is that code's mistake, not
// the client's: a plain error (a 500 that is logged), the unit of work rolls
// back, and no row is left.
func TestAMalformedRequestIsTheWritersMistakeAndLeavesNoRow(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	malformed := map[string]func(*entities.DeviationRequest){
		"no project":               func(r *entities.DeviationRequest) { r.Project = nil },
		"a project with no id":     func(r *entities.DeviationRequest) { r.Project = &entities.Project{} },
		"a kind outside the set":   func(r *entities.DeviationRequest) { r.Kind = "waive-ish" },
		"no kind":                  func(r *entities.DeviationRequest) { r.Kind = "" },
		"a status outside the set": func(r *entities.DeviationRequest) { r.Status = "waiting" },
		"no status":                func(r *entities.DeviationRequest) { r.Status = "" },
		"born approved":            func(r *entities.DeviationRequest) { r.Status = entities.DeviationRequestApproved },
		"born applied":             func(r *entities.DeviationRequest) { r.Status = entities.DeviationRequestApplied },
		"born rejected":            func(r *entities.DeviationRequest) { r.Status = entities.DeviationRequestRejected },
		"nobody asked":             func(r *entities.DeviationRequest) { r.RequestedBy = "" },
		"no account asked":         func(r *entities.DeviationRequest) { r.RequestedByID = uuid.Nil },
		"no fingerprint":           func(r *entities.DeviationRequest) { r.Fingerprint = "" },
		"no deadline":              func(r *entities.DeviationRequest) { r.ExpiresAt = time.Time{} },
		"a waive of no instance":   func(r *entities.DeviationRequest) { r.Instance = nil },
		"a waive of a nil instance": func(r *entities.DeviationRequest) {
			r.Instance = &entities.ProcessInstance{}
		},
		"a migration between no versions": func(r *entities.DeviationRequest) {
			r.Kind, r.Instance = entities.DeviationRequestMigration, nil
		},
		"a migration to no version": func(r *entities.DeviationRequest) {
			r.Kind, r.Instance = entities.DeviationRequestMigration, nil
			r.SourceDefinition = &entities.ProcessDefinition{ID: h.definitionOf(t, instanceID)}
		},
		"a migration from no version": func(r *entities.DeviationRequest) {
			r.Kind, r.Instance = entities.DeviationRequestMigration, nil
			r.TargetDefinition = &entities.ProcessDefinition{ID: h.definitionOf(t, instanceID)}
		},
		"a migration between versions with no id": func(r *entities.DeviationRequest) {
			r.Kind = entities.DeviationRequestMigration
			r.SourceDefinition, r.TargetDefinition = &entities.ProcessDefinition{}, &entities.ProcessDefinition{}
		},
		"already decided by somebody": func(r *entities.DeviationRequest) { r.DecidedBy, r.DecidedByID = "budi", budi },
		"already decided at some time": func(r *entities.DeviationRequest) {
			at := time.Now()
			r.DecidedAt = &at
		},
		"asked by a name of spaces": func(r *entities.DeviationRequest) { r.RequestedBy = "   " },
	}
	for name, spoil := range malformed {
		r := h.sampleRequest(instanceID, "dv1-malformed")
		spoil(&r)
		_, err := h.createRequest(h.tenantContext(), r)
		if err == nil {
			t.Errorf("%s: the request was written", name)
			continue
		}
		if !isTheWritersMistake(err) {
			t.Errorf("%s: refused as %v; a writer's mistake is a plain server error", name, err)
		}
	}
	if n := h.requestCountIn(t, h.projID); n != 0 {
		t.Fatalf("refused requests left %d row(s)", n)
	}
}

// A request is its organization's record. A caller from another organization
// can neither plant one against it, nor read, find, list, lock or decide one;
// and a request names only an instance of the project it is in.
func TestAnotherOrganizationNeitherWritesNorReadsNorDecidesARequest(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	ours := h.mustCreateRequest(t, h.sampleRequest(instanceID, "dv1-ours"))
	other := h.inAnotherOrganization(t, "Request Other Org")
	theirInstance := other.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	elsewhere := other.tenantContext()
	requests := h.repo.DeviationRequest()

	// Writes: into our project, or naming our instance from a project of theirs.
	if _, err := h.createRequest(elsewhere, h.sampleRequest(instanceID, "dv1-planted")); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("another organization writing into this project: %v, want not found", err)
	}
	if _, err := h.createRequest(elsewhere, other.sampleRequest(instanceID, "dv1-borrowed")); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("another organization naming this organization's instance: %v, want not found", err)
	}
	if _, err := h.createRequest(h.tenantContext(), h.sampleRequest(theirInstance, "dv1-reaching")); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("a request naming an instance of another organization: %v, want not found", err)
	}
	second, err := h.svc.CreateProject(h.tenantContext(), h.orgID, "Another Project", "")
	if err != nil {
		t.Fatalf("create the second project: %v", err)
	}
	wrongProject := h.sampleRequest(instanceID, "dv1-wrong-project")
	wrongProject.Project = &entities.Project{ID: second.ID}
	if _, err := h.createRequest(h.tenantContext(), wrongProject); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("a request naming an instance of another project of its organization: %v, want not found", err)
	}
	if _, err := h.createRequest(h.tenantContext(), h.sampleRequest(uuid.Must(uuid.NewV7()), "dv1-missing")); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("a request naming an instance that does not exist: %v, want not found", err)
	}

	// Reads.
	if _, err := requests.Get(elsewhere, ours.ID); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("another organization reading the request: %v, want not found", err)
	}
	if _, found, err := requests.FindLive(elsewhere, h.projID, "dv1-ours"); !errors.Is(err, apierr.ErrNotFound) || found {
		t.Errorf("another organization asking for this project's live request: found %v, %v; want not found", found, err)
	}
	for name, query := range map[string]entities.DeviationRequestQuery{
		"this project":  {Project: &entities.Project{ID: h.projID}},
		"every project": {},
	} {
		rows, total, err := requests.List(elsewhere, query, time.Now())
		if err != nil || total != 0 || len(rows) != 0 {
			t.Errorf("another organization listing %s: %d row(s) of %d, %v; want none", name, len(rows), total, err)
		}
	}

	// Locks and decisions.
	err = h.repo.UnitOfWork().Do(elsewhere, func(tx context.Context) error {
		_, err := requests.GetForUpdate(tx, ours.ID)
		return err
	})
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("another organization locking the request: %v, want not found", err)
	}
	if _, err := h.transition(elsewhere, ours.ID, entities.DeviationRequestPending, decisionTo(entities.DeviationRequestRejected)); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("another organization rejecting the request: %v, want not found", err)
	}
	for name, read := range map[string]func(tx context.Context) ([]entities.DeviationRequest, error){
		"overdue": func(tx context.Context) ([]entities.DeviationRequest, error) {
			return requests.ListOverdue(tx, time.Now().Add(100*time.Hour), 10)
		},
		"unreported": func(tx context.Context) ([]entities.DeviationRequest, error) {
			return requests.ListUnreported(tx, time.Now().Add(100*time.Hour), 10)
		},
	} {
		var rows []entities.DeviationRequest
		err := h.repo.UnitOfWork().Do(elsewhere, func(tx context.Context) error {
			var err error
			rows, err = read(tx)
			return err
		})
		if err != nil || len(rows) != 0 {
			t.Errorf("another organization sweeping what is %s: %d row(s), %v; want none", name, len(rows), err)
		}
	}

	if got := h.mustGetRequest(t, ours.ID); got.Status != entities.DeviationRequestPending || got.DecidedBy != "" {
		t.Errorf("after another organization's attempts the request is %s, decided by %q", got.Status, got.DecidedBy)
	}
	if n := h.requestCountIn(t, h.projID) + h.requestCountIn(t, other.projID) + h.requestCountIn(t, second.ID); n != 1 {
		t.Errorf("the refused writes left %d request(s) in all; want the one", n)
	}
	rows, total, err := requests.List(h.tenantContext(), entities.DeviationRequestQuery{}, time.Now())
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != ours.ID {
		t.Errorf("the organization's own list: %d row(s) of %d, %v", len(rows), total, err)
	}
}
