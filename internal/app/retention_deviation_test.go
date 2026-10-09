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
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// waitingWaive is a server with one instance waiting at a step, and a request
// to waive that step waiting for a second administrator.
type waitingWaive struct {
	app      *App
	db       *gorm.DB
	tenant   context.Context
	instance uuid.UUID
	request  uuid.UUID
}

// as is the request of an administrator of the organization, signed in under
// name with an account of their own.
func (w waitingWaive) as(name string) context.Context {
	return context.WithValue(w.tenant, pkgauth.UserContextKey, entities.User{
		ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("retention-test-account:"+name)), Username: name, Roles: []string{entities.RoleAdmin},
	})
}

// askForAWaive builds the server as TestARunningServerForgetsWhatCanNoLongerBeAsked
// does and has ana ask for the step of a new instance to be waived, counting
// as outputs. The process is one step and then a gateway that takes "accept"
// or "reject" and has no default, so a waive has to say what it counts as.
func askForAWaive(t *testing.T, outputs map[string]any) waitingWaive {
	t.Helper()
	gormDB := testutils.SetupTestDB(t)
	conn := testutils.StormConn(gormDB)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	w := waitingWaive{db: gormDB, app: &App{db: gormDB, storm: conn, repo: repo, sse: sse,
		svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "retention-deviation-test", nil, nil, nil)}}
	svc := w.app.svc

	org, err := svc.CreateOrganization(t.Context(), "Retention Org", "")
	if err != nil {
		t.Fatalf("create the organization: %v", err)
	}
	w.tenant = entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: org.ID.String()})
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
	if w.instance, err = svc.StartProcess(w.tenant, project.ID, "claim", nil); err != nil {
		t.Fatalf("start the instance: %v", err)
	}

	command := entities.DeviationCommand{InstanceID: w.instance, Kind: entities.DeviationWaive, NodeID: "review",
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
	w.request = asked.PendingApproval.RequestID
	return w
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
		if err := w.db.Raw(`SELECT status FROM deviation_requests WHERE id = ?`, w.request).Scan(&request).Error; err != nil {
			t.Fatalf("read the request: %v", err)
		}
		if err := w.db.Raw(`SELECT status FROM instance_deviations WHERE request_id = ?`, w.request).Scan(&row).Error; err != nil {
			t.Fatalf("read the ledger row: %v", err)
		}
		return request, row
	}

	// A pass before the deadline leaves it waiting, and says nothing of it.
	w.app.sweepRetention(entities.WithSystemContext(t.Context()), time.Now())
	if request, row := stored(); request != "pending_approval" || row != "pending_approval" {
		t.Fatalf("a pass before the deadline left the request %q and its row %q", request, row)
	}
	if lines := logs.said("waiting for a second administrator"); len(lines) != 0 {
		t.Fatalf("a pass with nothing to expire said %v", lines)
	}

	w.app.sweepRetention(entities.WithSystemContext(t.Context()), time.Now().Add(73*time.Hour))
	if request, row := stored(); request != "expired" || row != "expired" {
		t.Fatalf("after a pass past the deadline the request is stored as %q and its row as %q, want both expired", request, row)
	}
	lines := logs.said("waiting for a second administrator")
	if len(lines) != 1 || lines[0]["level"] != "info" || lines[0]["expired"] != float64(1) || lines[0]["database"] != "main" {
		t.Fatalf("the pass that expired a request said %v, want one line at info with expired = 1 for the main database", lines)
	}
	// The step is where it was: an expiry changes nothing of the instance.
	var open int
	if err := w.db.Raw(`SELECT count(*) FROM tasks WHERE instance_id = ? AND status IN ('unclaimed', 'claimed')`, w.instance).Scan(&open).Error; err != nil || open != 1 {
		t.Fatalf("%d task(s) are open on the instance (%v), want the one that was", open, err)
	}
}

// A ledger row written by a pod of the previous release holds its visit and
// has no live key, so the database's own guard does not see it. Migration 34
// fills those once; a row written after it ran — during a rolling upgrade, or
// after a rollback — is filled by the retention pass.
func TestTheRetentionPassGivesALiveLedgerRowItsKey(t *testing.T) {
	w := askForAWaive(t, map[string]any{"verdict": "accept"})
	if err := w.db.Exec(`UPDATE instance_deviations SET live_visit_key = NULL WHERE request_id = ?`, w.request).Error; err != nil {
		t.Fatalf("write the row as the previous release does: %v", err)
	}
	logs := captureLogs(t)
	w.app.sweepRetention(entities.WithSystemContext(t.Context()), time.Now())

	var row struct{ VisitKey, LiveVisitKey *string }
	if err := w.db.Raw(`SELECT visit_key, live_visit_key FROM instance_deviations WHERE request_id = ?`, w.request).Scan(&row).Error; err != nil {
		t.Fatalf("read the row: %v", err)
	}
	if row.VisitKey == nil || row.LiveVisitKey == nil || *row.LiveVisitKey != *row.VisitKey {
		t.Fatalf("after the pass the row's live key is %v and its visit key %v; want the live key filled", row.LiveVisitKey, row.VisitKey)
	}
	lines := logs.said("the key that holds")
	if len(lines) != 1 || lines[0]["level"] != "warn" || lines[0]["filled"] != float64(1) {
		t.Fatalf("the pass that filled a key said %v, want one warning with filled = 1", lines)
	}
	// With nothing to fill it says nothing: every pass would say it otherwise.
	w.app.sweepRetention(entities.WithSystemContext(t.Context()), time.Now())
	if lines := logs.said("the key that holds"); len(lines) != 1 {
		t.Fatalf("a pass with no key to fill said something: %v", lines)
	}
}

// The loop has nobody to return an error to, so the pass says what it closed,
// says when it could not, and says nothing when there was nothing to do.
func TestAnExpiryPassSaysWhatItClosedAndWhenItCouldNot(t *testing.T) {
	logs := captureLogs(t)
	about := "waiting for a second administrator"
	expire(t.Context(), "main", func(context.Context) (int64, error) { return 0, nil })
	if lines := logs.said(about); len(lines) != 0 {
		t.Fatalf("a pass with nothing to expire said %v", lines)
	}
	expire(t.Context(), "staging", func(context.Context) (int64, error) { return 3, nil })
	lines := logs.said(about)
	if len(lines) != 1 || lines[0]["level"] != "info" || lines[0]["expired"] != float64(3) || lines[0]["database"] != "staging" {
		t.Fatalf("a pass that expired three said %v", lines)
	}
	// A pass that closed some and failed on another says both.
	expire(t.Context(), "main", func(context.Context) (int64, error) { return 2, errors.New("request 0193 could not be closed") })
	lines = logs.said(about)
	if len(lines) != 3 || lines[1]["level"] != "info" || lines[1]["expired"] != float64(2) || lines[2]["level"] != "warn" ||
		lines[2]["error"] != "request 0193 could not be closed" || lines[2]["database"] != "main" {
		t.Fatalf("a pass that closed two and failed on one said %v, want what it closed and then a warning", lines)
	}
	want := "Could not record the expiry of requests waiting for a second administrator; they read as expired and none can be approved, " +
		"but the table says pending until a sweep succeeds."
	if lines[2]["message"] != want {
		t.Fatalf("the warning reads\n  %v\nwant\n  %s", lines[2]["message"], want)
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
		if len(lines) != 1 || lines[0]["level"] != "warn" || lines[0]["request"] != w.request.String() || lines[0]["actor"] != account {
			t.Fatalf("the log holds %v, want one warning naming request %s and %s", lines, w.request, account)
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
	var status string
	if err := w.db.Raw(`SELECT status FROM deviation_requests WHERE id = ?`, w.request).Scan(&status).Error; err != nil || status != "pending_approval" {
		t.Fatalf("after both the request is stored as %q (%v), want it still waiting", status, err)
	}
}
