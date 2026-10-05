package deviation_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/store/deviationrequest"
)

// The queue: newest first, paged; a pending request past its deadline is
// listed as expired, not as pending, before any sweep runs.
func TestTheQueueListsByEffectiveStatus(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	overdue := h.sampleRequest(instanceID, "dv1-old")
	overdue.ExpiresAt = time.Now().Add(-time.Minute)
	if _, err := h.createRequest(h.tenantContext(), overdue); err != nil {
		t.Fatalf("create overdue: %v", err)
	}
	fresh, err := h.createRequest(h.tenantContext(), h.sampleRequest(instanceID, "dv1-new"))
	if err != nil {
		t.Fatalf("create fresh: %v", err)
	}
	list := func(status entities.DeviationRequestStatus) ([]entities.DeviationRequest, int64) {
		rows, total, err := h.repo.DeviationRequest().List(h.tenantContext(),
			entities.DeviationRequestQuery{Status: status, Page: 1, PageSize: 10}, time.Now())
		if err != nil {
			t.Fatalf("list %s: %v", status, err)
		}
		return rows, total
	}
	if rows, total := list(entities.DeviationRequestPending); total != 1 || rows[0].ID != fresh.ID {
		t.Fatalf("pending lists %d (%v); want only the fresh one", total, rows)
	}
	if rows, total := list(entities.DeviationRequestExpired); total != 1 || rows[0].ID != overdue.ID {
		t.Fatalf("expired lists %d (%v); want the overdue one", total, rows)
	}
}

// requestShape is one state a request's row can be in, and what it reads as.
type requestShape struct {
	name string
	// stored is the status the row holds; readsAs is what the clock makes of it.
	stored, readsAs entities.DeviationRequestStatus
	// expires and decided are relative to the moment the lists are read at.
	expires, decided time.Duration
	// undated removes the time of the decision, by SQL: the repository
	// refuses to approve at no recorded time, so nothing else can make the row.
	undated bool
	// overdue and unreported are what the sweep's two reads answer.
	overdue, unreported bool
}

const threeDays = 72 * time.Hour

// everyRequestShape is every kind of row the queue and the sweep meet: each
// stored status, and each way the clock closes a live one — exactly at the
// boundary too.
var everyRequestShape = []requestShape{
	{name: "waiting in time", stored: entities.DeviationRequestPending, readsAs: entities.DeviationRequestPending, expires: threeDays},
	{name: "waiting past its deadline", stored: entities.DeviationRequestPending, readsAs: entities.DeviationRequestExpired, expires: -time.Minute, overdue: true},
	{name: "waiting at its deadline to the microsecond", stored: entities.DeviationRequestPending, readsAs: entities.DeviationRequestExpired, overdue: true},
	{name: "approved in its window", stored: entities.DeviationRequestApproved, readsAs: entities.DeviationRequestApproved, expires: threeDays, decided: -10 * time.Minute},
	{name: "approved an hour ago to the microsecond", stored: entities.DeviationRequestApproved, readsAs: entities.DeviationRequestInterrupted, expires: threeDays, decided: -entities.ApprovedRunReportWindow, unreported: true},
	{name: "approved past the hour", stored: entities.DeviationRequestApproved, readsAs: entities.DeviationRequestInterrupted, expires: threeDays, decided: -2 * time.Hour, unreported: true},
	{name: "approved past its deadline", stored: entities.DeviationRequestApproved, readsAs: entities.DeviationRequestInterrupted, expires: -time.Minute, decided: -10 * time.Minute, unreported: true},
	{name: "approved at no recorded time", stored: entities.DeviationRequestApproved, readsAs: entities.DeviationRequestInterrupted, expires: threeDays, undated: true, unreported: true},
	{name: "applied", stored: entities.DeviationRequestApplied, readsAs: entities.DeviationRequestApplied, expires: threeDays, decided: -10 * time.Minute},
	{name: "applied long ago", stored: entities.DeviationRequestApplied, readsAs: entities.DeviationRequestApplied, expires: -time.Minute, decided: -2 * time.Hour},
	{name: "interrupted", stored: entities.DeviationRequestInterrupted, readsAs: entities.DeviationRequestInterrupted, expires: threeDays, decided: -10 * time.Minute},
	{name: "stale", stored: entities.DeviationRequestStale, readsAs: entities.DeviationRequestStale, expires: threeDays, decided: -10 * time.Minute},
	{name: "rejected", stored: entities.DeviationRequestRejected, readsAs: entities.DeviationRequestRejected, expires: -time.Minute, decided: -2 * time.Hour},
	{name: "expired", stored: entities.DeviationRequestExpired, readsAs: entities.DeviationRequestExpired, expires: -time.Minute, decided: -10 * time.Minute},
}

// requestOfShape writes a request in the state shape describes, as of now,
// through the moves the product makes.
func (h *deviationHarness) requestOfShape(t *testing.T, instanceID uuid.UUID, shape requestShape, now time.Time) entities.DeviationRequest {
	t.Helper()
	r := h.sampleRequest(instanceID, "dv1-shape-"+shape.name)
	r.ExpiresAt = now.Add(shape.expires)
	request := h.mustCreateRequest(t, r)
	for _, next := range movesTo[shape.stored] {
		change := moveTo(request.Status, next)
		if request.Status == entities.DeviationRequestPending {
			change.DecidedAt = now.Add(shape.decided)
		}
		moved, err := h.transition(h.tenantContext(), request.ID, request.Status, change)
		if err != nil {
			t.Fatalf("%s: move to %s: %v", shape.name, next, err)
		}
		request = moved
	}
	if shape.undated {
		if err := h.db.Exec(`UPDATE deviation_requests SET decided_at = NULL WHERE id = ?`, request.ID).Error; err != nil {
			t.Fatalf("%s: take the time of the approval away: %v", shape.name, err)
		}
		request = h.mustGetRequest(t, request.ID)
	}
	if request.Status != shape.stored || request.EffectiveStatus(now) != shape.readsAs {
		t.Fatalf("%s: the row is %s and reads as %s at %v; the fixture wants %s reading as %s",
			shape.name, request.Status, request.EffectiveStatus(now), now, shape.stored, shape.readsAs)
	}
	return request
}

func requestIDs(rows []entities.DeviationRequest) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func sameIDs(got, want []uuid.UUID) bool {
	g, w := slices.Clone(got), slices.Clone(want)
	order := func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) }
	slices.SortFunc(g, order)
	slices.SortFunc(w, order)
	return slices.Equal(g, w)
}

// What the queue lists under a status is exactly what reads as that status —
// EffectiveStatus is the rule, and the query has to agree with it for every
// kind of row, or a request is offered for a decision nobody can make, or is
// in no list at all.
func TestTheQueueAgreesWithTheClockForEveryShapeOfRequest(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	now := time.Now().UTC().Truncate(time.Microsecond)
	names := map[uuid.UUID]string{}
	readsAs := map[entities.DeviationRequestStatus][]uuid.UUID{}
	for _, shape := range everyRequestShape {
		request := h.requestOfShape(t, instanceID, shape, now)
		names[request.ID] = shape.name
		readsAs[shape.readsAs] = append(readsAs[shape.readsAs], request.ID)
	}
	named := func(ids []uuid.UUID) []string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			out = append(out, names[id])
		}
		slices.Sort(out)
		return out
	}

	listed := 0
	for _, status := range everyRequestStatus {
		rows, total, err := h.repo.DeviationRequest().List(h.tenantContext(), entities.DeviationRequestQuery{Status: status, PageSize: 100}, now)
		if err != nil {
			t.Fatalf("list %s: %v", status, err)
		}
		got := requestIDs(rows)
		if !sameIDs(got, readsAs[status]) || total != int64(len(readsAs[status])) {
			t.Errorf("listed as %s (%d in all): %q\nwant %q", status, total, named(got), named(readsAs[status]))
		}
		for _, row := range rows {
			if row.EffectiveStatus(now) != status {
				t.Errorf("%q is listed as %s and reads as %s", names[row.ID], status, row.EffectiveStatus(now))
			}
		}
		listed += len(rows)
	}
	if listed != len(everyRequestShape) {
		t.Errorf("the seven lists hold %d request(s) between them; there are %d, and each is in exactly one", listed, len(everyRequestShape))
	}

	// No status asked for: every one of them.
	rows, total, err := h.repo.DeviationRequest().List(h.tenantContext(), entities.DeviationRequestQuery{PageSize: 100}, now)
	if err != nil || total != int64(len(everyRequestShape)) || len(rows) != len(everyRequestShape) {
		t.Errorf("the list of every status holds %d of %d (%v); want %d", len(rows), total, err, len(everyRequestShape))
	}
	// A status the code does not know is the caller's mistake, not an empty list.
	if _, _, err := h.repo.DeviationRequest().List(h.tenantContext(), entities.DeviationRequestQuery{Status: "waiting"}, now); !isTheWritersMistake(err) {
		t.Errorf("a list by a status outside the set: %v; want a plain server error", err)
	}
}

// The sweep reads exactly what the clock has closed and nobody has written
// down: waiting requests at or past their deadline, and approved ones whose
// run window has closed — with no time of approval among them, at once.
func TestTheSweepReadsWhatIsOverdueAndWhatNeverReported(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	now := time.Now().UTC().Truncate(time.Microsecond)
	names := map[uuid.UUID]string{}
	var overdue, unreported []uuid.UUID
	for _, shape := range everyRequestShape {
		request := h.requestOfShape(t, instanceID, shape, now)
		names[request.ID] = shape.name
		if shape.overdue {
			overdue = append(overdue, request.ID)
		}
		if shape.unreported {
			unreported = append(unreported, request.ID)
		}
		if request.RunWindowClosed(now) != shape.unreported {
			t.Fatalf("%s: RunWindowClosed is %v; the fixture says %v", shape.name, request.RunWindowClosed(now), shape.unreported)
		}
	}
	named := func(rows []entities.DeviationRequest) []string {
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, names[row.ID])
		}
		return out
	}
	sweep := entities.WithSystemContext(context.Background())
	requests := h.repo.DeviationRequest()
	read := func(list func(context.Context, time.Time, int) ([]entities.DeviationRequest, error), limit int) ([]entities.DeviationRequest, error) {
		var rows []entities.DeviationRequest
		err := h.repo.UnitOfWork().Do(sweep, func(tx context.Context) error {
			var err error
			rows, err = list(tx, now, limit)
			return err
		})
		return rows, err
	}

	got, err := read(requests.ListOverdue, 100)
	if err != nil || !sameIDs(requestIDs(got), overdue) {
		t.Errorf("overdue: %q (%v); want the %d waiting at or past their deadline", named(got), err, len(overdue))
	}
	for _, row := range got {
		if row.Status != entities.DeviationRequestPending || row.EffectiveStatus(now) != entities.DeviationRequestExpired {
			t.Errorf("overdue lists %q, which is %s and reads as %s", names[row.ID], row.Status, row.EffectiveStatus(now))
		}
	}
	got, err = read(requests.ListUnreported, 100)
	if err != nil || !sameIDs(requestIDs(got), unreported) {
		t.Errorf("unreported: %q (%v); want the %d approved requests whose run window has closed", named(got), err, len(unreported))
	}
	for _, row := range got {
		if row.Status != entities.DeviationRequestApproved || !row.RunWindowClosed(now) {
			t.Errorf("unreported lists %q, which is %s with its window closed: %v", names[row.ID], row.Status, row.RunWindowClosed(now))
		}
	}

	// A batch is bounded, and takes the longest overdue first.
	got, err = read(requests.ListOverdue, 1)
	if err != nil || len(got) != 1 || names[got[0].ID] != "waiting past its deadline" {
		t.Errorf("a batch of one: %q (%v); want the request longest past its deadline", named(got), err)
	}
	if got, err = read(requests.ListUnreported, 2); err != nil || len(got) != 2 {
		t.Errorf("a batch of two unreported: %q (%v)", named(got), err)
	}
	for name, list := range map[string]func(context.Context, time.Time, int) ([]entities.DeviationRequest, error){
		"overdue": requests.ListOverdue, "unreported": requests.ListUnreported,
	} {
		if _, err := read(list, 0); !isTheWritersMistake(err) {
			t.Errorf("%s with no batch size: %v; want a plain server error", name, err)
		}
		// The rows are held for whoever moves them: a read outside a
		// transaction would hold nothing.
		if _, err := list(sweep, now, 100); !errors.Is(err, repocontracts.ErrDeviationRequestOutsideTransaction) {
			t.Errorf("%s outside a transaction: %v, want ErrDeviationRequestOutsideTransaction", name, err)
		}
	}
}

// The queue is paged, newest first, in an order that does not shift between
// pages — requests written in one transaction share their creation time — and
// its total is of every request, however many there are. A page is bounded
// whatever is asked for.
func TestTheQueueIsPagedNewestFirstStableAndCompletePastAThousand(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	const requests = 1001
	written := make([]uuid.UUID, 0, requests)
	err := h.repo.UnitOfWork().Do(h.tenantContext(), func(tx context.Context) error {
		for i := range requests {
			request, err := h.repo.DeviationRequest().Create(tx, h.sampleRequest(instanceID, fmt.Sprintf("dv1-page-%d", i)))
			if err != nil {
				return err
			}
			written = append(written, request.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("write %d requests: %v", requests, err)
	}
	now := time.Now()
	page := func(query entities.DeviationRequestQuery) ([]entities.DeviationRequest, int64) {
		t.Helper()
		query.Status = entities.DeviationRequestPending
		rows, total, err := h.repo.DeviationRequest().List(h.tenantContext(), query, now)
		if err != nil {
			t.Fatalf("list %+v: %v", query, err)
		}
		return rows, total
	}

	// Asked for everything at once: a full page and no more, and the true total.
	rows, total := page(entities.DeviationRequestQuery{PageSize: 1_000_000})
	if total != requests || len(rows) != repocontracts.MaxPageSize {
		t.Fatalf("a page of a million holds %d of %d; want %d of %d", len(rows), total, repocontracts.MaxPageSize, requests)
	}
	if rows, _ := page(entities.DeviationRequestQuery{}); len(rows) != repocontracts.DefaultPageSize {
		t.Errorf("a page of no stated size holds %d; want %d", len(rows), repocontracts.DefaultPageSize)
	}

	// Walked page by page: every request once, newest first.
	var walked []uuid.UUID
	for number := 1; ; number++ {
		rows, total := page(entities.DeviationRequestQuery{Page: number, PageSize: repocontracts.MaxPageSize})
		if total != requests {
			t.Fatalf("page %d says there are %d; want %d", number, total, requests)
		}
		walked = append(walked, requestIDs(rows)...)
		if len(rows) < repocontracts.MaxPageSize {
			break
		}
	}
	// One transaction, so one creation time: among them the id is the order.
	newestFirst := slices.Clone(written)
	slices.SortFunc(newestFirst, func(a, b uuid.UUID) int { return slices.Compare(b[:], a[:]) })
	if !slices.Equal(walked, newestFirst) {
		t.Fatalf("the pages hold %d request(s), %d of them where the newest-first order puts them; want all %d, each once",
			len(walked), agreeing(walked, newestFirst), requests)
	}

	// A request made later is newer, whatever its id.
	latest := h.sampleRequest(instanceID, "dv1-page-latest")
	latest.ID = uuid.UUID{0x00, 0x01}
	h.mustCreateRequest(t, latest)
	if rows, total := page(entities.DeviationRequestQuery{PageSize: 1}); total != requests+1 || len(rows) != 1 || rows[0].ID != latest.ID {
		t.Errorf("the first of %d is %v; want the request made last", total, requestIDs(rows))
	}
	h.mustReject(t, latest.ID)

	// A project's queue is its own.
	second, err := h.svc.CreateProject(h.tenantContext(), h.orgID, "Another Project", "")
	if err != nil {
		t.Fatalf("create the second project: %v", err)
	}
	if rows, total := page(entities.DeviationRequestQuery{Project: &entities.Project{ID: second.ID}}); total != 0 || len(rows) != 0 {
		t.Errorf("a project with no requests lists %d of %d", len(rows), total)
	}
	if _, total := page(entities.DeviationRequestQuery{Project: &entities.Project{ID: h.projID}}); total != requests {
		t.Errorf("the project's own queue says %d; want %d", total, requests)
	}
}

// agreeing counts the positions two orders agree at.
func agreeing(got, want []uuid.UUID) int {
	n := 0
	for i := range min(len(got), len(want)) {
		if got[i] == want[i] {
			n++
		}
	}
	return n
}

func (h *deviationHarness) mustReject(t *testing.T, id uuid.UUID) {
	t.Helper()
	if _, err := h.transition(h.tenantContext(), id, entities.DeviationRequestPending, decisionTo(entities.DeviationRequestRejected)); err != nil {
		t.Fatalf("reject the request: %v", err)
	}
}

// The queue reads a request without its command, its plan and the instances
// it covers. The list of instances has no bound — every running instance of a
// version — and the other two are sealed: read with every row, a page would
// cost what its largest requests weigh, for anyone who may list. They come
// with the request when it is read by itself.
func TestTheQueueReadsARequestWithoutItsHeavyDocuments(t *testing.T) {
	h := newDeviationHarness(t)
	first := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	second := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	const covered = 20_000
	instances := make([]uuid.UUID, 0, covered)
	for range covered {
		instances = append(instances, uuid.Must(uuid.NewV7()))
	}
	want := entities.DeviationRequest{
		ID: uuid.Must(uuid.NewV7()), Project: &entities.Project{ID: h.projID},
		Kind: entities.DeviationRequestMigration, Status: entities.DeviationRequestPending,
		SourceDefinition: &entities.ProcessDefinition{ID: h.definitionOf(t, first)},
		TargetDefinition: &entities.ProcessDefinition{ID: h.definitionOf(t, second)},
		RequestedBy:      "ana", RequestedByID: uuid.Must(uuid.NewV7()), Reason: "the step was retired",
		Command:     map[string]any{"node_mapping": map[string]any{"approve": "review"}},
		Plan:        map[string]any{"instances": float64(covered), "because": []any{"“Approve” is skipped"}},
		Fingerprint: "mf1-heavy", ApprovedInstances: instances,
		ExpiresAt: time.Date(2031, 10, 3, 9, 30, 0, 0, time.UTC), Outcome: map[string]any{},
	}
	h.mustCreateRequest(t, want)

	rows, total, err := h.repo.DeviationRequest().List(h.tenantContext(), entities.DeviationRequestQuery{}, time.Now())
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("the queue: %d row(s) of %d, %v", len(rows), total, err)
	}
	listed := rows[0]
	if listed.Command != nil || listed.Plan != nil || listed.ApprovedInstances != nil || listed.Because() != nil {
		t.Errorf("the queue's row carries a command of %d key(s), a plan of %d and %d instance(s); want none of the three",
			len(listed.Command), len(listed.Plan), len(listed.ApprovedInstances))
	}
	light := want
	light.Command, light.Plan, light.ApprovedInstances = nil, nil, nil
	sameRequest(t, "the queue's row", listed, light)

	// Not only left undecoded: the queue's read does not select them.
	for _, heavy := range []string{"Command", "Plan", "ApprovedInstances"} {
		if _, selected := reflect.TypeOf(deviationrequest.QueueRow{}).FieldByName(heavy); selected {
			t.Errorf("the queue's read selects %s", heavy)
		}
	}
	// The request read by itself has all of it.
	whole := h.mustGetRequest(t, want.ID)
	sameRequest(t, "the request read by itself", whole, want)
	if len(whole.ApprovedInstances) != covered {
		t.Errorf("the request read by itself covers %d instance(s); want %d", len(whole.ApprovedInstances), covered)
	}
}

// A project is deleted softly, and its requests are still there: one that
// waits past its deadline has a ledger row that holds its visit until
// somebody closes it. No tenant sees the project any more, so only the sweep
// can — it reads by status and time, with nothing that leaves a deleted
// project's rows out.
func TestTheSweepReachesARequestWhoseProjectWasDeleted(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	late := h.sampleRequest(instanceID, "dv1-orphan-overdue")
	late.ExpiresAt = time.Now().Add(-time.Minute)
	overdue := h.mustCreateRequest(t, late)
	waiting := h.pendingWaive(instanceID, overdue.Fingerprint)
	waiting.RequestID = overdue.ID
	row := h.mustWrite(t, waiting)
	approval := decisionTo(entities.DeviationRequestApproved)
	approval.DecidedAt = time.Now().Add(-2 * time.Hour)
	silent := h.mustCreateRequest(t, h.sampleRequest(instanceID, "dv1-orphan-unreported"))
	if _, err := h.transition(h.tenantContext(), silent.ID, entities.DeviationRequestPending, approval); err != nil {
		t.Fatalf("approve: %v", err)
	}

	if err := h.db.Exec(`UPDATE projects SET deleted_at = now() WHERE id = ?`, h.projID).Error; err != nil {
		t.Fatalf("delete the project: %v", err)
	}
	if _, err := h.repo.DeviationRequest().Get(h.tenantContext(), overdue.ID); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("the organization reading a request of its deleted project: %v, want not found", err)
	}

	sweep := entities.WithSystemContext(context.Background())
	now := time.Now()
	err := h.repo.UnitOfWork().Do(sweep, func(tx context.Context) error {
		requests := h.repo.DeviationRequest()
		late, err := requests.ListOverdue(tx, now, 100)
		if err != nil {
			return err
		}
		if !sameIDs(requestIDs(late), []uuid.UUID{overdue.ID}) {
			t.Errorf("overdue, the project deleted: %v; want the one request", requestIDs(late))
		}
		unreported, err := requests.ListUnreported(tx, now, 100)
		if err != nil {
			return err
		}
		if !sameIDs(requestIDs(unreported), []uuid.UUID{silent.ID}) {
			t.Errorf("unreported, the project deleted: %v; want the one request", requestIDs(unreported))
		}
		if _, err := h.repo.DeviationDecider().Decide(tx, row.ID, repocontracts.LedgerRowDecision{Status: entities.DeviationExpired, DecidedAt: now}); err != nil {
			return fmt.Errorf("close the ledger row: %w", err)
		}
		if _, err := requests.Transition(tx, overdue.ID, entities.DeviationRequestPending, repocontracts.DeviationRequestChange{Status: entities.DeviationRequestExpired}); err != nil {
			return fmt.Errorf("close the overdue request: %w", err)
		}
		_, err = requests.Transition(tx, silent.ID, entities.DeviationRequestApproved, repocontracts.DeviationRequestChange{
			Status: entities.DeviationRequestInterrupted, Outcome: map[string]any{"error": "the run did not report back"}})
		return err
	})
	if err != nil {
		t.Fatalf("the sweep over a deleted project's requests: %v", err)
	}
	for id, want := range map[uuid.UUID]string{overdue.ID: "expired", silent.ID: "interrupted"} {
		if stored := h.storedRequest(t, id); stored.status != want || stored.liveKey.Valid {
			t.Errorf("after the sweep the request is %s holding %+v; want %s holding nothing", stored.status, stored.liveKey, want)
		}
	}
	if live := h.liveVisitKeyOf(t, row.ID); live.Valid {
		t.Errorf("after the sweep the ledger row still holds its visit with %q", live.String)
	}
}
