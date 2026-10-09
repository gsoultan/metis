package deviation_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/app"
	"github.com/gsoultan/metis/internal/pkg/health"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/interceptors/tenant"
	"github.com/gsoultan/metis/tests/testutils"
)

const (
	// soleWaits is what a sole administrator approving their own request is
	// told while the installation does not allow it.
	soleWaits = "You asked for this, and nobody else administers this organization, so it waits. " +
		"Give another person's account the administrator role so they can approve it, reject it yourself, or let it expire. " +
		"Whoever operates this installation can name this organization as one that has a single administrator, " +
		"whose own approval is then accepted; until they do, the request waits: " +
		"see \"A second administrator approves waivers and skips\" in docs/upgrading.md."
	// anotherMustApprove is what the requester is told while somebody else
	// administers the organization, whatever the installation allows.
	anotherMustApprove = "You asked for this. A different administrator has to approve it."
	// sayWhy is what a self-approval with no reason is told.
	sayWhy = "Say why you are approving your own request: with nobody else to approve it, the reason is the record."
	// selfApprovalLogged is a fragment of the line each self-approval logs,
	// and refusedSelfApprovalLogged of the line a refused attempt logs.
	selfApprovalLogged        = "approved their own request"
	refusedSelfApprovalLogged = "tried to approve their own request"
	// staleNameLogged is a fragment of the line logged when the requester is
	// refused in an organization that is named as having one administrator,
	// because it has another.
	staleNameLogged = "names this organization as having one administrator"
)

// restarted is the tests' server started again over the same database, as an
// operator restarts it after changing its environment: the list of
// organizations is read from the environment, once, and the services are put
// together afresh with what was read — as internal/app does when a server
// starts (its own test shows that what it reads is what it announces and what
// it builds with). What the database holds — the organization, its project,
// its accounts — is as it was.
func (h *deviationHarness) restarted(t *testing.T) *deviationHarness {
	t.Helper()
	sse := observersimpl.NewSSEObserver()
	dispatcher := observersimpl.NewEventDispatcher()
	dispatcher.Register(observersimpl.NewAuditLogObserver(h.repo.Audit()))
	dispatcher.Register(observersimpl.NewNotificationObserver(serviceimpl.NewNotificationService(h.repo.Notification())))
	organizations, _ := serviceimpl.SoleAdministratorOrganizations()
	svc := services.NewServiceFacade(h.repo, dispatcher, sse, "deviation-test-secret", nil, nil, nil,
		services.WithSoleAdministratorOrganizations(organizations))
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, testutils.StormConn(h.db))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	again := *h
	again.server, again.svc, again.seconder = server, svc, ""
	return &again
}

// at is the organization and project of where, reached through h's server.
func (h *deviationHarness) at(where *deviationHarness) *deviationHarness {
	there := *h
	there.orgID, there.projID, there.deployed, there.seconder = where.orgID, where.projID, where.deployed, ""
	return &there
}

// soleOrganizations starts the tests' server with the list of organizations
// written as list says, given the organization the harness made. An
// organization has its id before an operator can name it, so the server is
// started, the list written, and the server started again — which is when
// the list is read.
func soleOrganizations(t *testing.T, list func(organization uuid.UUID) string) *deviationHarness {
	t.Helper()
	h := newDeviationRouteHarness(t)
	t.Setenv(serviceimpl.EnvSoleAdministratorOrganizations, list(h.orgID))
	return h.restarted(t)
}

// withOrganizationNamed starts the tests' server with the harness's
// organization named as one whose only administrator may approve their own
// request.
func withOrganizationNamed(t *testing.T) *deviationHarness {
	t.Helper()
	return soleOrganizations(t, uuid.UUID.String)
}

// enrolled is an account as a test wants it to be: what it holds on the
// account, where it belongs, and what it holds there.
type enrolled struct {
	// id is the account's id, when the test has to know it before the
	// account exists; otherwise one is made.
	id uuid.UUID
	// global is the roles held on the account: in every organization it
	// belongs to.
	global []string
	// here says the account belongs to the organization under test, and
	// rolesHere what it holds in that organization alone.
	here      bool
	rolesHere []string
	// elsewhere says it belongs to another organization as well (or only),
	// and rolesElsewhere what it holds in that one alone.
	elsewhere      bool
	rolesElsewhere []string
}

// enrol creates the account name as spec says — written as the harness
// writes every account (createAccount), with the grants an administrator
// makes made through the account service — signs it in and answers its id
// and its token. Everything is granted before the account's first request, so no
// request is ever answered from what the account used to be.
func (h *deviationHarness) enrol(t *testing.T, name string, spec enrolled) (uuid.UUID, string) {
	t.Helper()
	system := entities.WithSystemContext(context.Background())
	account := entities.User{ID: spec.id, Username: name, Roles: spec.global}
	if account.ID == uuid.Nil {
		account.ID = uuid.Must(uuid.NewV7())
	}
	grants := map[uuid.UUID][]string{}
	if spec.here {
		account.Organizations = append(account.Organizations, &entities.Organization{ID: h.orgID})
		grants[h.orgID] = spec.rolesHere
	}
	if spec.elsewhere {
		org, err := h.svc.CreateOrganization(context.Background(), "Elsewhere of "+name, "")
		if err != nil {
			t.Fatalf("create the other organization of %s: %v", name, err)
		}
		account.Organizations = append(account.Organizations, &entities.Organization{ID: org.ID})
		grants[org.ID] = spec.rolesElsewhere
	}
	h.createAccount(t, account)
	for organization, roles := range grants {
		if len(roles) == 0 {
			continue
		}
		there := entities.WithTenantContext(system, entities.TenantContext{TenantID: organization.String()})
		if err := h.svc.SetOrganizationRoles(there, account.ID, roles); err != nil {
			t.Fatalf("give %s the roles %v in %s: %v", name, roles, organization, err)
		}
	}
	return account.ID, h.login(t, name)
}

// remove deletes an account, as the account service deletes one.
func (h *deviationHarness) remove(t *testing.T, id uuid.UUID) {
	t.Helper()
	if err := h.svc.DeleteUser(entities.WithSystemContext(context.Background()), id); err != nil {
		t.Fatalf("delete account %s: %v", id, err)
	}
}

func (h *deviationHarness) login(t *testing.T, name string) string {
	t.Helper()
	status, body := h.do(t, http.MethodPost, "", "/api/v1/login", map[string]string{"username": name, "password": harnessPassword})
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &out); status != http.StatusOK || err != nil || out.Token == "" {
		t.Fatalf("login %s: %d (%s)", name, status, body)
	}
	return out.Token
}

// decideHere sends a decision as a request for the harness's organization,
// named in the header an account of several organizations chooses one with.
func (h *deviationHarness) decideHere(t *testing.T, token, requestID, verb, reason string) (int, requestReply, string) {
	t.Helper()
	body := "{}"
	if reason != "" {
		encoded, err := json.Marshal(map[string]string{"reason": reason})
		if err != nil {
			t.Fatalf("encode the decision: %v", err)
		}
		body = string(encoded)
	}
	status, raw, _ := h.call(t, http.MethodPost, token, requestPath(requestID)+"/"+verb, body, tenant.OrganizationHeader, h.orgID.String())
	var reply requestReply
	if status == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &reply); err != nil {
			t.Fatalf("decode: %v (%s)", err, raw)
		}
	}
	return status, reply, raw
}

// posted is what a request sent from a goroutine of a test's own was answered.
type posted struct {
	status int
	raw    string
}

// post sends one request and answers what came back, failing nothing: it is
// for a request a test sends from a goroutine of its own, which may not fail
// the test itself.
func (h *deviationHarness) post(token, path, body string) (posted, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.server.URL+path, strings.NewReader(body))
	if err != nil {
		return posted{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := routeClient.Do(req)
	if err != nil {
		return posted{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return posted{status: resp.StatusCode, raw: strings.TrimSpace(string(raw))}, err
}

// entriesOf is the trail entries of one type an instance has.
func (h *deviationHarness) entriesOf(t *testing.T, instanceID uuid.UUID, eventType string) []entities.AuditEntry {
	t.Helper()
	entries, err := h.svc.GetAuditLogs(h.tenantContext(), instanceID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	var found []entities.AuditEntry
	for _, entry := range entries {
		if entry.Type == eventType {
			found = append(found, entry)
		}
	}
	return found
}
