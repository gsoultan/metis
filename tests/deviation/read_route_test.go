package deviation_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

type deviationsReply struct {
	Deviations []map[string]any `json:"deviations"`
}

func (h *deviationHarness) readDeviations(t *testing.T, token, instanceID string) (int, deviationsReply, string) {
	t.Helper()
	status, body := h.do(t, http.MethodGet, token, "/api/v1/instances/"+instanceID+"/deviations", nil)
	var reply deviationsReply
	if status == http.StatusOK {
		if err := json.Unmarshal([]byte(body), &reply); err != nil {
			t.Fatalf("decode the reply: %v (%s)", err, body)
		}
	}
	return status, reply, body
}

// Readers of an instance's deviations are the readers of its audit trail: any
// signed-in member of its organization. The audit trail already shows them the
// instance's variables, so before and after disclose nothing new.
func TestAMemberReadsAnInstancesDeviationsOldestFirst(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	first, err := h.write(h.tenantContext(), h.sample(instanceID))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	second, err := h.write(h.tenantContext(), h.sample(instanceID))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, role := range []string{entities.RoleUser, entities.RoleOperator, entities.RoleDesigner, entities.RoleAdmin} {
		token := h.signIn(t, "reader-"+strings.ToLower(role), role)
		status, reply, body := h.readDeviations(t, token, instanceID.String())
		if status != http.StatusOK {
			t.Fatalf("a %s reading the deviations: %d (%s)", role, status, body)
		}
		if len(reply.Deviations) != 2 || reply.Deviations[0]["id"] != first.ID.String() || reply.Deviations[1]["id"] != second.ID.String() {
			t.Fatalf("a %s read %v, want the two rows oldest first", role, reply.Deviations)
		}
		row := reply.Deviations[0]
		for key, want := range map[string]any{"kind": "reassign", "scope": "task", "origin": "task", "status": "applied",
			"actor": "boss", "reason": "alice is on leave", "node_id": "step", "node_name": "Approve"} {
			if row[key] != want {
				t.Errorf("%s = %v, want %v", key, row[key], want)
			}
		}
	}
}

// An instance nobody has deviated from answers with an empty list, not null and
// not a refusal.
func TestAnInstanceWithoutDeviationsReadsAsAnEmptyList(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	status, _, body := h.readDeviations(t, h.signIn(t, "member", entities.RoleUser), instanceID.String())
	if status != http.StatusOK || !strings.Contains(body, `"deviations":[]`) {
		t.Fatalf("an instance with no deviations: %d (%s), want 200 and an empty list", status, body)
	}
}

// Anonymous is 401; somebody from another organization — an administrator
// there included — is told there is no such instance.
func TestTheDeviationsOfAnInstanceAreReadByItsOrganizationOnly(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	if _, err := h.write(h.tenantContext(), h.sample(instanceID)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if status, _, body := h.readDeviations(t, "", instanceID.String()); status != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d (%s), want 401", status, body)
	}
	outsider := h.signInElsewhere(t, "outsider-admin", entities.RoleAdmin)
	status, _, body := h.readDeviations(t, outsider, instanceID.String())
	if status != http.StatusNotFound {
		t.Fatalf("another organization's administrator: %d (%s), want 404", status, body)
	}
	if strings.Contains(body, "alice is on leave") {
		t.Fatalf("the refusal leaked a reason: %s", body)
	}
	outsiderMember := h.signInElsewhere(t, "outsider-member", entities.RoleUser)
	if status, _, body := h.readDeviations(t, outsiderMember, instanceID.String()); status != http.StatusNotFound {
		t.Fatalf("another organization's member: %d (%s), want 404", status, body)
	}
	if status, _, body := h.readDeviations(t, h.signIn(t, "member", entities.RoleUser), "not-an-id"); status != http.StatusBadRequest {
		t.Fatalf("a malformed id: %d (%s), want 400", status, body)
	}
}

// Account ids identify a person across renames inside the ledger; on the wire
// they are an identifier a member has no use for.
func TestTheReadRouteReturnsNoAccountIds(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	account := uuid.Must(uuid.NewV7())
	signedIn := context.WithValue(h.tenantContext(), pkgauth.UserContextKey,
		entities.User{ID: account, Username: "boss", Roles: []string{entities.RoleAdmin}})
	ledger := serviceimpl.NewDeviationLedger(h.repo)
	if err := h.repo.UnitOfWork().Do(signedIn, func(tx context.Context) error {
		_, err := ledger.Record(tx, h.sample(instanceID))
		return err
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	var stored int64
	if err := h.db.Raw(`SELECT count(*) FROM instance_deviations WHERE actor_id = ?`, account).Scan(&stored).Error; err != nil || stored != 1 {
		t.Fatalf("the row does not keep the account id (%d, %v); this test would prove nothing", stored, err)
	}
	_, _, body := h.readDeviations(t, h.signIn(t, "member", entities.RoleUser), instanceID.String())
	// The id itself, not only the names it could travel under: a field added
	// later under another name would carry it past a check of key names.
	for _, forbidden := range []string{`"actor_id"`, `"approved_by_id"`, account.String()} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("the reply carries %s: %s", forbidden, body)
		}
	}
}

// recordAs enters d in the ledger as the caller of ctx, through the seam every
// writer uses, so the row keeps the account id the seam gives it.
func (h *deviationHarness) recordAs(t *testing.T, ctx context.Context, d entities.Deviation) {
	t.Helper()
	ledger := serviceimpl.NewDeviationLedger(h.repo)
	if err := h.repo.UnitOfWork().Do(ctx, func(tx context.Context) error {
		_, err := ledger.Record(tx, d)
		return err
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
}

// The server, acting with nobody signed in, is written as "System" — and so is
// an account somebody named System. The row tells them apart by the account id
// it keeps, which the route does not return, so the route says which it was.
func TestTheReadRouteSaysWhetherTheServerOrAnAccountActed(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})

	byTheServer := h.sample(instanceID)
	byTheServer.Actor = "System"
	h.recordAs(t, h.tenantContext(), byTheServer)

	byAnAccount := h.sample(instanceID)
	byAnAccount.Actor = "System"
	h.recordAs(t, context.WithValue(h.tenantContext(), pkgauth.UserContextKey,
		entities.User{ID: uuid.Must(uuid.NewV7()), Username: "System", Roles: []string{entities.RoleAdmin}}), byAnAccount)

	status, reply, body := h.readDeviations(t, h.signIn(t, "member", entities.RoleUser), instanceID.String())
	if status != http.StatusOK || len(reply.Deviations) != 2 {
		t.Fatalf("read: %d (%s), want the two rows", status, body)
	}
	for i, want := range []bool{true, false} {
		row := reply.Deviations[i]
		if row["actor"] != "System" {
			t.Fatalf("row %d is by %v; both are written as System, which is the point", i, row["actor"])
		}
		if got, present := row["actor_is_server"]; !present || got != want {
			t.Errorf("row %d: actor_is_server = %v (present %v), want %v", i, got, present, want)
		}
	}
}

// before, after and details are always objects: a client reads row.before.tasks
// without first asking whether there is a before. A row that changed nothing
// has three empty ones.
func TestARowThatChangedNothingStillReadsWithItsThreeObjects(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	bare := h.sample(instanceID)
	bare.Before, bare.After, bare.Details = nil, nil, nil
	if _, err := h.write(h.tenantContext(), bare); err != nil {
		t.Fatalf("write: %v", err)
	}
	status, _, body := h.readDeviations(t, h.signIn(t, "member", entities.RoleUser), instanceID.String())
	if status != http.StatusOK {
		t.Fatalf("read: %d (%s)", status, body)
	}
	for _, want := range []string{`"before":{}`, `"after":{}`, `"details":{}`} {
		if !strings.Contains(body, want) {
			t.Errorf("the reply has no %s: %s", want, body)
		}
	}
}
