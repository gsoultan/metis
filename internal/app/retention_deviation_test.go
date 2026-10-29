package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/deviationrequest"
	"github.com/gsoultan/metis/server/repositories/store/instancedeviation"
	"github.com/gsoultan/metis/server/repositories/store/task"
	"github.com/gsoultan/metis/tests/testutils"
)

// waitingWaive is a server with one instance waiting at a step, and a request
// to waive that step waiting for a second administrator.
type waitingWaive struct {
	app      *App
	db       *db.Conn
	tenant   context.Context
	project  uuid.UUID
	instance uuid.UUID
	request  uuid.UUID
}

// account is the account an administrator of the fixture signs in with.
func (w waitingWaive) account(name string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("retention-test-account:"+name))
}

// as is the request of an administrator of the organization, signed in under
// name with an account of their own.
func (w waitingWaive) as(name string) context.Context {
	return context.WithValue(w.tenant, pkgauth.UserContextKey, entities.User{
		ID: w.account(name), Username: name, Roles: []string{entities.RoleAdmin},
	})
}

// another starts one more instance and has ana ask for its step to be waived,
// and answers the instance and the request that waits.
func (w waitingWaive) another(t *testing.T, outputs map[string]any) (instance, request uuid.UUID) {
	t.Helper()
	svc := w.app.svc
	instance, err := svc.StartProcess(w.tenant, w.project, "claim", nil)
	if err != nil {
		t.Fatalf("start the instance: %v", err)
	}
	command := entities.DeviationCommand{InstanceID: instance, Kind: entities.DeviationWaive, NodeID: "review",
		Reason: "the reviewer is away and the claim is small", Outputs: outputs, DryRun: true}
	preview, err := svc.DeviateInstance(w.as("ana"), command)
	if err != nil {
		t.Fatalf("preview the waive: %v", err)
	}
	command.VisitKey, command.DryRun = preview.Plan.VisitKey, false
	asked, err := svc.DeviateInstance(w.as("ana"), command)
	if err != nil || asked.PendingApproval == nil {
		t.Fatalf("ask for the waive: %+v, %v", asked, err)
	}
	return instance, asked.PendingApproval.RequestID
}

// askForAWaive builds the server as TestARunningServerForgetsWhatCanNoLongerBeAsked
// does and has ana ask for the step of a new instance to be waived, counting
// as outputs. The process is one step and then a gateway that takes "accept"
// or "reject" and has no default, so a waive has to say what it counts as.
func askForAWaive(t *testing.T, outputs map[string]any) waitingWaive {
	t.Helper()
	conn := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	w := waitingWaive{db: conn, app: &App{storm: conn, repo: repo, sse: sse,
		svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "retention-deviation-test", nil, nil, nil)}}
	svc := w.app.svc

	org, err := svc.CreateOrganization(t.Context(), "Retention Org", "")
	if err != nil {
		t.Fatalf("create the organization: %v", err)
	}
	w.tenant = entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: org.ID.String()})
	// ana, who asks, is an account of the organization, written once: an
	// approval asks whether whoever made the request still administers it,
	// of the accounts. She is its only one — budi, who approves in some
	// tests, is a principal and no account — so that a test of an
	// organization with one administrator has one.
	if err := testutils.EnrolAdministrator(w.tenant, repo, org.ID, w.account("ana"), "ana"); err != nil {
		t.Fatal(err)
	}
	project, err := svc.CreateProject(w.tenant, org.ID, "Retention Project", "")
	if err != nil {
		t.Fatalf("create the project: %v", err)
	}
	if _, err := svc.CreateDefinition(w.tenant, &entities.ProcessDefinition{
		Project: &entities.Project{ID: project.ID}, Key: "claim", Name: "Claim",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the claim", Assignee: "rita", Properties: testutils.FormDeclaring("verdict")},
			{ID: "decide", Type: entities.ExclusiveGateway, Name: "Verdict?"},
			{ID: "accepted", Type: entities.EndEvent},
			{ID: "rejected", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "decide"},
			{ID: "yes", SourceRef: "decide", TargetRef: "accepted", Condition: "verdict = accept"},
			{ID: "no", SourceRef: "decide", TargetRef: "rejected", Condition: "verdict = reject"},
		},
	}); err != nil {
		t.Fatalf("deploy the process: %v", err)
	}
	w.project = project.ID
	w.instance, w.request = w.another(t, outputs)
	return w
}

// requestStatus is how request is stored.
func (w waitingWaive) requestStatus(t *testing.T, request uuid.UUID) string {
	t.Helper()
	row, found, err := deviationrequest.New().IDEq(request).One(t.Context(), testutils.Executor(w.db))
	if err != nil || !found {
		t.Fatalf("read request %s: found %v, %v", request, found, err)
	}
	return row.Status
}

// expiredBefore moves request's deadline to ago before now.
func (w waitingWaive) expiredBefore(t *testing.T, request uuid.UUID, ago time.Duration) {
	t.Helper()
	deadline := deviationrequest.MutateKey(request)
	deadline.SetExpiresAt(time.Now().Add(-ago))
	if err := deadline.Update(t.Context(), testutils.Executor(w.db)); err != nil {
		t.Fatalf("let time pass: %v", err)
	}
}

// said is the lines kept that carry a fragment of a message, each as the
// fields it was logged with.
func (l *logTap) said(fragment string) []map[string]any {
	l.mu.Lock()
	kept := bytes.Clone(l.kept.Bytes())
	l.mu.Unlock()

	var lines []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(kept))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := map[string]any{}
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}
		if message, _ := line["message"].(string); strings.Contains(message, fragment) {
			lines = append(lines, line)
		}
	}
	return lines
}

// The retention pass is what records expiry on every database; without it a
// request past its deadline stays pending in the table for ever.
func TestTheRetentionPassExpiresOverdueDeviationRequests(t *testing.T) {
	w := askForAWaive(t, map[string]any{"verdict": "accept"})
	logs := captureLogs(t)
	stored := func() (request, row string) {
		t.Helper()
		ledger, found, err := instancedeviation.New().RequestIDEq(w.request).One(t.Context(), testutils.Executor(w.db))
		if err != nil || !found {
			t.Fatalf("read the ledger row: found %v, %v", found, err)
		}
		return w.requestStatus(t, w.request), ledger.Status
	}

	// A pass before the deadline leaves it waiting, and says nothing of it.
	w.app.sweepRetention(entities.WithSystemContext(t.Context()), time.Now(), optInRetention{})
	if request, row := stored(); request != "pending_approval" || row != "pending_approval" {
		t.Fatalf("a pass before the deadline left the request %q and its row %q", request, row)
	}
	if lines := logs.said("waiting for a second administrator"); len(lines) != 0 {
		t.Fatalf("a pass with nothing to expire said %v", lines)
	}

	w.app.sweepRetention(entities.WithSystemContext(t.Context()), time.Now().Add(73*time.Hour), optInRetention{})
	if request, row := stored(); request != "expired" || row != "expired" {
		t.Fatalf("after a pass past the deadline the request is stored as %q and its row as %q, want both expired", request, row)
	}
	lines := logs.said("waiting for a second administrator")
	if len(lines) != 1 || lines[0]["level"] != "info" || lines[0]["expired"] != float64(1) || lines[0]["database"] != "main" {
		t.Fatalf("the pass that expired a request said %v, want one line at info with expired = 1 for the main database", lines)
	}
	// The step is where it was: an expiry changes nothing of the instance.
	open, err := task.New().InstanceIDEq(w.instance).
		StatusIn(string(models.TaskUnclaimed), string(models.TaskClaimed)).Count(t.Context(), testutils.Executor(w.db))
	if err != nil || open != 1 {
		t.Fatalf("%d task(s) are open on the instance (%v), want the one that was", open, err)
	}
}

// The loop has nobody to return an error to, so the pass says what it closed,
// says when it could not, and says nothing when there was nothing to do.
func TestAnExpiryPassSaysWhatItClosedAndWhenItCouldNot(t *testing.T) {
	logs := captureLogs(t)
	about := "a second administrator"
	expire(t.Context(), "main", func(context.Context) (entities.SweptRequests, error) { return entities.SweptRequests{}, nil })
	if lines := logs.said(about); len(lines) != 0 {
		t.Fatalf("a pass with nothing to expire said %v", lines)
	}
	expire(t.Context(), "staging", func(context.Context) (entities.SweptRequests, error) {
		return entities.SweptRequests{Expired: 3}, nil
	})
	lines := logs.said(about)
	if len(lines) != 1 || lines[0]["level"] != "info" || lines[0]["expired"] != float64(3) || lines[0]["database"] != "staging" {
		t.Fatalf("a pass that expired three said %v", lines)
	}
	// A pass that closed some and failed on another says both.
	expire(t.Context(), "main", func(context.Context) (entities.SweptRequests, error) {
		return entities.SweptRequests{Expired: 2}, errors.New("request 0193 could not be closed")
	})
	lines = logs.said(about)
	if len(lines) != 3 || lines[1]["level"] != "info" || lines[1]["expired"] != float64(2) || lines[2]["level"] != "warn" ||
		lines[2]["error"] != "request 0193 could not be closed" || lines[2]["database"] != "main" {
		t.Fatalf("a pass that closed two and failed on one said %v, want what it closed and then a warning", lines)
	}
	want := "Could not record everything the clock has decided about requests for a second administrator; each reads as expired " +
		"or interrupted and none can be approved or run under, but its row says otherwise until a sweep succeeds."
	if lines[2]["message"] != want {
		t.Fatalf("the warning reads\n  %v\nwant\n  %s", lines[2]["message"], want)
	}
	// An approved request whose run never reported is not an expiry, and is
	// not said as one: it is counted apart, in words that are true of it,
	// and as a warning — a server that stopped mid-run leaves exactly this.
	about = "requests for a second administrator were interrupted"
	expire(t.Context(), "main", func(context.Context) (entities.SweptRequests, error) {
		return entities.SweptRequests{Expired: 1, Interrupted: 2}, nil
	})
	interrupted := logs.said(about)
	if len(interrupted) != 1 || interrupted[0]["level"] != "warn" || interrupted[0]["interrupted"] != float64(2) || interrupted[0]["database"] != "main" {
		t.Fatalf("a pass that interrupted two said %v, want one warning counting the two", interrupted)
	}
	if _, said := interrupted[0]["expired"]; said {
		t.Fatalf("the line about interrupted requests counts expired ones too: %v", interrupted[0])
	}
	if expired := logs.said("Recorded the expiry of requests"); len(expired) != 3 || expired[2]["expired"] != float64(1) {
		t.Fatalf("the same pass said %v of what expired, want the one, counted by itself", expired)
	}
}

// An attempt to approve one's own request, and an approval whose waive could
// not move the instance on, each change nothing — so nothing in the ledger or
// on the trail says they happened. The server's log does: a warning that
// names the request and the account, and carries no value anybody asked for.
func TestARefusedSelfApprovalAndAnApprovalThatCouldNotAdvanceLeaveATrace(t *testing.T) {
	w := askForAWaive(t, map[string]any{"verdict": "maybe-next-quarter"})
	logs := captureLogs(t)
	named := func(lines []map[string]any, account string) {
		t.Helper()
		if len(lines) != 1 || lines[0]["level"] != "warn" || lines[0]["request"] != w.request.String() || lines[0]["actor"] != account ||
			lines[0]["actor_id"] != w.account(account).String() {
			t.Fatalf("the log holds %v, want one warning naming request %s and %s, by name and by account id", lines, w.request, account)
		}
		written, _ := json.Marshal(lines[0])
		if strings.Contains(string(written), "maybe-next-quarter") || strings.Contains(string(written), "the reviewer is away") {
			t.Fatalf("the trace carries what was asked for: %s", written)
		}
	}

	if _, err := w.app.svc.ApproveDeviationRequest(w.as("ana"), w.request, "it is only mine"); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("ana approving her own request: %v, want forbidden", err)
	}
	named(logs.said("tried to approve their own request"), "ana")

	_, err := w.app.svc.ApproveDeviationRequest(w.as("budi"), w.request, "")
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "The request is still waiting") {
		t.Fatalf("budi approving a waive its gateway cannot follow: %v, want it refused with the request still waiting", err)
	}
	named(logs.said("could not be applied"), "budi")
	if status := w.requestStatus(t, w.request); status != "pending_approval" {
		t.Fatalf("after both the request is stored as %q, want it still waiting", status)
	}
}

// A pass that could not close some of what it read says so in one line at its
// end — how many it closed, how many it could not — however many those are;
// and it gets past all of them to what is behind. Twenty-five requests whose
// ledger row no longer waits (written by hand: the product does not make
// one) are older than one healthy request. The pass closes the healthy one,
// names the first few it could not close one by one, counts the rest, and
// both the service's line and the retention loop's say what was left.
func TestAPassThatLeavesRequestsBehindSaysHowManyItClosedAndHowManyItCouldNot(t *testing.T) {
	w := askForAWaive(t, map[string]any{"verdict": "accept"})
	const broken = 25
	requests := []uuid.UUID{w.request}
	for range broken {
		_, request := w.another(t, map[string]any{"verdict": "accept"})
		requests = append(requests, request)
	}
	for i, request := range requests {
		w.expiredBefore(t, request, time.Duration(len(requests)-i)*time.Minute)
	}
	healthy := requests[broken]
	var rejected instancedeviation.Mut
	rejected.SetStatus("rejected")
	rejected.SetLiveVisitKeyNull()
	keys := make([][16]byte, broken)
	for i, request := range requests[:broken] {
		keys[i] = request
	}
	if _, err := instancedeviation.New().RequestIDIn(keys...).UpdateAll(t.Context(), testutils.Executor(w.db), rejected); err != nil {
		t.Fatalf("break the ledger rows of the first %d: %v", broken, err)
	}
	logs := captureLogs(t)
	w.app.sweepRetention(entities.WithSystemContext(t.Context()), time.Now(), optInRetention{})

	if status := w.requestStatus(t, healthy); status != "expired" {
		t.Fatalf("behind %d requests that cannot be closed, the one that can is stored as %q after a pass; want expired", broken, status)
	}
	end := logs.said("left some as they were")
	if len(end) != 1 || end[0]["level"] != "warn" || end[0]["closed"] != float64(1) || end[0]["not_closed"] != float64(broken) || end[0]["budget_spent"] != false {
		t.Fatalf("the end of the pass said %v; want one warning with closed = 1 and not_closed = %d", end, broken)
	}
	if each := logs.said("could not be written down as closed"); len(each) != 5 || each[0]["request"] != requests[0].String() {
		t.Fatalf("%d requests were named one by one (%v); want the first five, the oldest first, and the rest counted", len(each), each)
	}
	loop := logs.said("Could not record everything the clock has decided")
	if len(loop) != 1 {
		t.Fatalf("the retention loop said %v; want one line", loop)
	}
	if text, _ := loop[0]["error"].(string); !strings.Contains(text, "25 requests past their deadline could not be closed (1 closed)") ||
		!strings.Contains(text, requests[0].String()) {
		t.Fatalf("the retention loop said %v; want it to say 25 could not be closed and 1 was, naming the first", loop)
	}
	if told := logs.said("Recorded the expiry of requests"); len(told) != 1 || told[0]["expired"] != float64(1) {
		t.Fatalf("the retention loop said %v of what was closed; want the one", told)
	}
}

// A request whose ledger row another transaction holds is not a request that
// cannot be closed: whoever holds the row is deciding it, or about to, and the
// next pass finds it free. The pass counts it apart — `held`, not
// `not_closed` — does not name it among the requests that could not be
// written down, and what it answers says held, not broken.
func TestAPassCountsARequestSomebodyHoldsApartFromOneItCannotClose(t *testing.T) {
	w := askForAWaive(t, map[string]any{"verdict": "accept"})
	_, free := w.another(t, map[string]any{"verdict": "accept"})
	for i, request := range []uuid.UUID{w.request, free} {
		w.expiredBefore(t, request, time.Duration(10-i)*time.Minute)
	}
	holder := testutils.Begin(t, w.db)
	if _, err := instancedeviation.New().RequestIDEq(w.request).ForUpdate().All(t.Context(), holder, nil); err != nil {
		t.Fatalf("hold the ledger row of the first request: %v", err)
	}

	logs := captureLogs(t)
	w.app.sweepRetention(entities.WithSystemContext(t.Context()), time.Now(), optInRetention{})

	stored := func(request uuid.UUID) string {
		t.Helper()
		return w.requestStatus(t, request)
	}
	if stored(w.request) != "pending_approval" || stored(free) != "expired" {
		t.Fatalf("the held request is stored as %q and the free one as %q; want the first as it was and the second expired", stored(w.request), stored(free))
	}
	end := logs.said("left some as they were")
	if len(end) != 1 || end[0]["closed"] != float64(1) || end[0]["held"] != float64(1) || end[0]["not_closed"] != float64(0) {
		t.Fatalf("the end of the pass said %v; want one line with closed = 1, held = 1 and not_closed = 0", end)
	}
	if each := logs.said("could not be written down as closed"); len(each) != 0 {
		t.Fatalf("a held request was named as one that could not be written down: %v", each)
	}
	loop := logs.said("Could not record everything the clock has decided")
	if len(loop) != 1 {
		t.Fatalf("the retention loop said %v; want one line", loop)
	}
	if text, _ := loop[0]["error"].(string); !strings.Contains(text, "1 request past their deadline was held by another transaction and left for the next pass (1 closed)") ||
		!strings.Contains(text, w.request.String()) || strings.Contains(text, "could not be closed") {
		t.Fatalf("the retention loop said %q; want the held request named as held, and nothing called broken", text)
	}

	// Let go, the next pass closes it and has nothing to say.
	if err := holder.Rollback(t.Context()); err != nil {
		t.Fatalf("let go of the ledger row: %v", err)
	}
	logs = captureLogs(t)
	w.app.sweepRetention(entities.WithSystemContext(t.Context()), time.Now(), optInRetention{})
	if stored(w.request) != "expired" || len(logs.said("left some as they were")) != 0 {
		t.Fatalf("after the row was let go the request is stored as %q and the pass said %v; want it expired and nothing said", stored(w.request), logs.said("left some as they were"))
	}
}
