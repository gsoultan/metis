package serverconnection_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// project is one installation's copy of the same project: the same
// organization and project names, its own ids.
type project struct {
	ctx context.Context
	id  uuid.UUID
}

func newProject(t *testing.T, s server) project {
	t.Helper()
	ctx, _, id := testutils.ScopedProject(t, s.repo)
	return project{ctx: ctx, id: id}
}

// connect connects downstream to upstream with a code made upstream, and has
// a second administrator downstream accept it. It answers the connection's
// id on each side.
func connect(t *testing.T, upstream, downstream server) (up, down uuid.UUID) {
	t.Helper()
	code, err := upstream.svc.MakeServerConnectionCode(admin("dana"))
	if err != nil {
		t.Fatalf("make a code: %v", err)
	}
	pending, err := downstream.svc.CreateServerConnection(admin("lee"), upstream.url, code.Code)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := downstream.svc.AcceptServerConnection(admin("kim"), pending.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	upSide, err := upstream.svc.ListServerConnections(admin("dana"))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, c := range upSide {
		if c.Key == identityKey(t, downstream) {
			return c.ID, pending.ID
		}
	}
	t.Fatal("the upstream server does not list the downstream one")
	return uuid.Nil, uuid.Nil
}

// cut designs a decision and a process in p and cuts them as a release.
func cut(t *testing.T, s server, p project, name string) {
	t.Helper()
	if _, err := s.svc.CreateDecision(p.ctx, entities.DecisionDefinition{
		Project: &entities.Project{ID: p.id}, Key: "limit", Name: "Limit", HitPolicy: "FIRST",
		Outputs: []entities.DecisionOutput{{ID: "o", Label: "Limit", Name: "limit", Type: "string"}},
		Rules:   []entities.DecisionRule{{ID: "r", Outputs: []any{"low"}}},
	}); err != nil {
		t.Fatalf("design the decision: %v", err)
	}
	if _, err := s.svc.CreateDefinition(p.ctx, &entities.ProcessDefinition{
		Project: &entities.Project{ID: p.id}, Key: "claims", Name: "Claims",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "decide", Type: entities.BusinessRuleTask, Name: "Decide", Properties: map[string]any{"decision_key": "limit"}},
			{ID: "end", Type: entities.EndEvent, Name: "End"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "decide"},
			{ID: "f2", SourceRef: "decide", TargetRef: "end"},
		},
	}); err != nil {
		t.Fatalf("design the process: %v", err)
	}
	if _, err := s.svc.CutRelease(p.ctx, contracts.CutRelease{ProjectID: p.id, Name: name, Processes: []string{"claims"}}); err != nil {
		t.Fatalf("cut: %v", err)
	}
}

// sentState is how far development says a release got, as of the last
// check-in.
func sentState(t *testing.T, s server, p project, release string) entities.ReleaseDelivery {
	t.Helper()
	sent, err := s.svc.ListReleaseDeliveries(p.ctx, p.id)
	if err != nil {
		t.Fatalf("list the releases sent: %v", err)
	}
	for _, d := range sent {
		if d.Release == release {
			return d
		}
	}
	t.Fatalf("%s was not sent: %+v", release, sent)
	return entities.ReleaseDelivery{}
}

func collect(t *testing.T, s server, connection uuid.UUID) []contracts.CollectedReleaseResult {
	t.Helper()
	got, err := s.svc.CollectReleases(admin("lee"), connection)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	return got.Releases
}

// Development sends a release. Production, which nothing outside reaches,
// collects it when it checks in: staged, imported by the server it came
// from, and development sees how far it got as of that check-in, up to live.
func TestASentReleaseIsCollectedStagedAndReportedBackUntilLive(t *testing.T) {
	allowLoopback(t)
	dev := newServer(t, "Development", entities.ServerDevelopment)
	prod := newServer(t, "Production", entities.ServerProduction)
	toProd, fromDev := connect(t, dev, prod)
	devProject, prodProject := newProject(t, dev), newProject(t, prod)
	cut(t, dev, devProject, "r1")

	sent, err := dev.svc.SendRelease(devProject.ctx, devProject.id, "r1", toProd)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sent.State != entities.DeliveryWaiting || sent.ServerName != "Production" {
		t.Fatalf("sent %+v, want waiting for production", sent)
	}
	if again, err := dev.svc.SendRelease(devProject.ctx, devProject.id, "r1", toProd); err != nil || again.ID != sent.ID {
		t.Fatalf("sending it twice made %+v (%v), want the first", again, err)
	}

	got := collect(t, prod, fromDev)
	if len(got) != 1 || got[0].State != entities.DeliveryStaged || got[0].Release != "r1" {
		t.Fatalf("production collected %+v, want r1 staged", got)
	}
	staged, err := prod.svc.GetRelease(prodProject.ctx, prodProject.id, "r1")
	if err != nil || staged.State != entities.ReleaseStaged || staged.ImportedBy != "server Development" {
		t.Fatalf("production holds %+v (%v), want r1 staged, imported by development", staged, err)
	}
	d := sentState(t, dev, devProject, "r1")
	if d.State != entities.DeliveryStaged || d.CollectedAt == nil || d.ReportedAt == nil || d.LastSeenAt == nil {
		t.Fatalf("development sees %+v, want staged as of production's check-in", d)
	}

	if _, err := prod.svc.GoLive(prodProject.ctx, prodProject.id, "r1"); err != nil {
		t.Fatalf("go live: %v", err)
	}
	if got := collect(t, prod, fromDev); len(got) != 1 || got[0].State != entities.DeliveryLive {
		t.Fatalf("production reported %+v, want r1 live", got)
	}
	if d := sentState(t, dev, devProject, "r1"); d.State != entities.DeliveryLive {
		t.Fatalf("development sees %+v, want live", d)
	}
	if got := collect(t, prod, fromDev); len(got) != 0 {
		t.Fatalf("a live release was handed over again: %+v", got)
	}
}

// A release for a project production does not have is refused, with why,
// and sent again once the project is there.
func TestAReleaseForAProjectProductionLacksIsRefusedAndCanBeSentAgain(t *testing.T) {
	allowLoopback(t)
	dev := newServer(t, "Development", entities.ServerDevelopment)
	prod := newServer(t, "Production", entities.ServerProduction)
	toProd, fromDev := connect(t, dev, prod)
	devProject := newProject(t, dev)
	cut(t, dev, devProject, "r1")
	if _, err := dev.svc.SendRelease(devProject.ctx, devProject.id, "r1", toProd); err != nil {
		t.Fatalf("send: %v", err)
	}

	got := collect(t, prod, fromDev)
	if len(got) != 1 || got[0].State != entities.DeliveryRefused || !strings.Contains(got[0].Detail, "no project") {
		t.Fatalf("production collected %+v, want r1 refused for want of the project", got)
	}
	if d := sentState(t, dev, devProject, "r1"); d.State != entities.DeliveryRefused || !strings.Contains(d.Detail, `"Test Project"`) {
		t.Fatalf("development sees %+v, want refused, naming the project", d)
	}
	if got := collect(t, prod, fromDev); len(got) != 0 {
		t.Fatalf("a refused release was handed over again: %+v", got)
	}

	ctx, orgID := testutils.ScopedContext(t, prod.repo)
	if err := prod.repo.Project().Create(entities.WithSystemContext(ctx), models.ProjectModel{
		Base: models.Base{ID: models.UUID(uuid.Must(uuid.NewV7()))}, OrganizationID: models.UUID(orgID), Name: "Test Project",
	}); err != nil {
		t.Fatalf("create the project in production: %v", err)
	}
	if again, err := dev.svc.SendRelease(devProject.ctx, devProject.id, "r1", toProd); err != nil || again.State != entities.DeliveryWaiting {
		t.Fatalf("sending it again got %+v (%v), want waiting", again, err)
	}
	if got := collect(t, prod, fromDev); len(got) != 1 || got[0].State != entities.DeliveryStaged {
		t.Fatalf("production collected %+v, want r1 staged", got)
	}
}

// Releases go forwards: development sends only to a server after it.
func TestAReleaseIsNotSentToTheServerItCameFrom(t *testing.T) {
	allowLoopback(t)
	dev := newServer(t, "Development", entities.ServerDevelopment)
	prod := newServer(t, "Production", entities.ServerProduction)
	_, fromDev := connect(t, dev, prod)
	prodProject := newProject(t, prod)
	cut(t, prod, prodProject, "r1")
	if _, err := prod.svc.SendRelease(prodProject.ctx, prodProject.id, "r1", fromDev); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("sending back to development got %v, want a refusal", err)
	}
}

// Only a server connected with its token is handed anything: nobody else,
// and not with a token of a server it gives releases to written into a guess.
func TestOnlyTheConnectedServerIsHandedItsReleases(t *testing.T) {
	allowLoopback(t)
	dev := newServer(t, "Development", entities.ServerDevelopment)
	for _, auth := range []string{"", "Bearer ", "Bearer not-the-token", "Basic Zm9vOmJhcg=="} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, dev.url+serviceimpl.ReleaseCollectPath,
			bytes.NewReader([]byte(`{"format":"`+contracts.ReleaseCollectionFormat+`"}`)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("check in: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("a check-in with %q got %s, want forbidden", auth, resp.Status)
		}
	}
}

// With a staging between them, production takes the release from staging
// once it went live there, with staging's signed note that it did, and can
// make it live.
func TestStagingPassesTheReleaseOnWithItsLiveNote(t *testing.T) {
	allowLoopback(t)
	dev := newServer(t, "Development", entities.ServerDevelopment)
	staging := newServer(t, "Staging", entities.ServerStaging)
	prod := newServer(t, "Production", entities.ServerProduction)
	toStaging, fromDev := connect(t, dev, staging)
	toProd, fromStaging := connect(t, staging, prod)
	devProject, stagingProject, prodProject := newProject(t, dev), newProject(t, staging), newProject(t, prod)
	cut(t, dev, devProject, "r1")

	if _, err := dev.svc.SendRelease(devProject.ctx, devProject.id, "r1", toStaging); err != nil {
		t.Fatalf("send to staging: %v", err)
	}
	collect(t, staging, fromDev)
	if _, err := staging.svc.GoLive(stagingProject.ctx, stagingProject.id, "r1"); err != nil {
		t.Fatalf("go live in staging: %v", err)
	}
	if _, err := staging.svc.SendRelease(stagingProject.ctx, stagingProject.id, "r1", toProd); err != nil {
		t.Fatalf("send to production: %v", err)
	}
	if got := collect(t, prod, fromStaging); len(got) != 1 || got[0].State != entities.DeliveryStaged || got[0].Detail != "" {
		t.Fatalf("production collected %+v, want r1 staged with nothing amiss", got)
	}
	staged, err := prod.svc.GetRelease(prodProject.ctx, prodProject.id, "r1")
	if err != nil || staged.LiveNote == nil {
		t.Fatalf("production holds %+v (%v), want staging's live note attached", staged, err)
	}
	if _, err := prod.svc.GoLive(prodProject.ctx, prodProject.id, "r1"); err != nil {
		t.Fatalf("go live in production: %v", err)
	}
	collect(t, staging, fromDev)
	if d := sentState(t, dev, devProject, "r1"); d.State != entities.DeliveryLive {
		t.Fatalf("development sees %+v, want live in staging", d)
	}
}
