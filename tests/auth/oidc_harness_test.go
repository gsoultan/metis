package auth_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/app"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/health"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

const (
	oidcClientID = "metis"
	oidcKeyID    = "test-signing-key"

	// organizationClaim is the claim the tests' provider puts a person's
	// organizations in, and envOrganizationClaim the setting that names it.
	organizationClaim    = "metis_organizations"
	envOrganizationClaim = "METIS_OIDC_ORGANIZATION_CLAIM"

	localPassword = "local-account-password"

	// requestTimeout bounds one request, so a hang fails the test rather than
	// the run.
	requestTimeout = 30 * time.Second
)

// identityProvider is a fake OpenID Connect issuer: go-oidc's own test server
// for the discovery document and the key set, and ID tokens signed with the
// key it advertises. Nothing here reaches a real provider.
type identityProvider struct {
	server *httptest.Server
	key    *rsa.PrivateKey
}

func newIdentityProvider(t *testing.T) *identityProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate the provider's signing key: %v", err)
	}
	fake := &oidctest.Server{PublicKeys: []oidctest.PublicKey{
		{PublicKey: key.Public(), KeyID: oidcKeyID, Algorithm: oidc.RS256},
	}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	fake.SetIssuer(server.URL)
	return &identityProvider{server: server, key: key}
}

// issuer is the provider's issuer URL, which is what an account it signs in is
// linked by, together with the subject.
func (p *identityProvider) issuer() string { return p.server.URL }

// token signs an ID token for subject, with claims on top of the ones every ID
// token carries. Each call is a new token, as each sign-in is.
func (p *identityProvider) token(t *testing.T, subject string, claims map[string]any) string {
	t.Helper()
	now := time.Now()
	body := map[string]any{
		"iss": p.issuer(),
		"aud": oidcClientID,
		"sub": subject,
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
		"jti": uuid.NewString(),
	}
	maps.Copy(body, claims)
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode the token's claims: %v", err)
	}
	return oidctest.SignIDToken(p.key, oidcKeyID, oidc.RS256, string(raw))
}

// apiHarness serves the production request chain, built by app.BuildAPIHandler.
type apiHarness struct {
	t        *testing.T
	provider *identityProvider
	svc      services.ServiceFacade
	server   *httptest.Server
}

// newOIDCHarness serves the API with OIDC sign-in configured against a fake
// provider, and with organizationClaim ("" leaves it unset) as the claim that
// carries a person's organizations.
func newOIDCHarness(t *testing.T, organizationClaim string) *apiHarness {
	t.Helper()
	t.Setenv(envOrganizationClaim, organizationClaim)
	provider := newIdentityProvider(t)
	validator, err := pkgauth.NewTokenValidator(t.Context(), provider.issuer(), oidcClientID)
	if err != nil {
		t.Fatalf("configure OIDC against the fake provider: %v", err)
	}
	h := newAPIHarness(t, validator)
	h.provider = provider
	return h
}

// newLocalHarness serves the API the way an installation without OIDC does:
// local accounts, signing in with a password.
func newLocalHarness(t *testing.T) *apiHarness {
	t.Helper()
	return newAPIHarness(t, nil)
}

func newAPIHarness(t *testing.T, validator *pkgauth.TokenValidator) *apiHarness {
	t.Helper()
	// Every request reaches the database. A sign-in remembered for a few
	// seconds would hide whether a second one found the first one's account.
	t.Setenv("METIS_AUTH_CACHE_TTL", "0s")

	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse,
		"oidc-test-secret", nil, nil, nil, func(*gorm.DB) {})
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, validator,
		map[string]health.Checker{}, testutils.StormConn(db))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &apiHarness{t: t, svc: svc, server: server}
}

// organization creates an organization with one project, named after it.
func (h *apiHarness) organization(name string) uuid.UUID {
	h.t.Helper()
	org, err := h.svc.CreateOrganization(h.t.Context(), name, "")
	if err != nil {
		h.t.Fatalf("create organization %s: %v", name, err)
	}
	if _, err := h.svc.CreateProject(inOrganization(h.t.Context(), org.ID), org.ID, name+" Project", ""); err != nil {
		h.t.Fatalf("create %s's project: %v", name, err)
	}
	return org.ID
}

// localAccount creates an account that signs in with a password, as an
// administrator of org would.
func (h *apiHarness) localAccount(username, email string, org uuid.UUID) entities.User {
	h.t.Helper()
	account := entities.User{
		ID:            uuid.Must(uuid.NewV7()),
		Username:      username,
		Email:         email,
		Roles:         []string{entities.RoleDesigner},
		Organizations: []*entities.Organization{{ID: org}},
	}
	if err := h.svc.CreateUser(inOrganization(h.t.Context(), org), account, localPassword); err != nil {
		h.t.Fatalf("create local account %s: %v", username, err)
	}
	return account
}

// login signs a local account in with its password.
func (h *apiHarness) login(username string) string {
	h.t.Helper()
	status, body := h.do(http.MethodPost, "/api/v1/login", "", "",
		map[string]string{"username": username, "password": localPassword})
	if status != http.StatusOK {
		h.t.Fatalf("login %s: status %d (%s)", username, status, body)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.Token == "" {
		h.t.Fatalf("login %s returned no token: %s", username, body)
	}
	return out.Token
}

func (h *apiHarness) get(path, token, organization string) (int, string) {
	h.t.Helper()
	return h.do(http.MethodGet, path, token, organization, nil)
}

func (h *apiHarness) do(method, path, token, organization string, body any) (int, string) {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			h.t.Fatalf("encode the body of %s %s: %v", method, path, err)
		}
	}
	ctx, cancel := context.WithTimeout(h.t.Context(), requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, h.server.URL+path, &buf)
	if err != nil {
		h.t.Fatalf("build %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if organization != "" {
		req.Header.Set("X-Organization-ID", organization)
	}
	resp, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("read the reply to %s %s: %v", method, path, err)
	}
	return resp.StatusCode, string(out)
}

// projects lists the projects a request reaches, which is the organization it
// was scoped to.
func (h *apiHarness) projects(token, organization string) []string {
	h.t.Helper()
	status, body := h.get("/api/v1/projects", token, organization)
	if status != http.StatusOK {
		h.t.Fatalf("list projects: status %d (%s), want 200", status, body)
	}
	var out struct {
		Projects []struct {
			Name string `json:"name"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		h.t.Fatalf("decode the projects: %v (%s)", err, body)
	}
	names := make([]string, 0, len(out.Projects))
	for _, p := range out.Projects {
		names = append(names, p.Name)
	}
	slices.Sort(names)
	return names
}

// ownAccount is the account a token signs in as, as the account's owner sees it.
type ownAccount struct {
	ID               string   `json:"id"`
	Username         string   `json:"username"`
	Email            string   `json:"email"`
	Roles            []string `json:"roles"`
	IdentityProvider string   `json:"identity_provider"`
}

func (h *apiHarness) me(token string) ownAccount {
	h.t.Helper()
	status, body := h.get("/api/v1/users/me", token, "")
	if status != http.StatusOK {
		h.t.Fatalf("read the signed-in account: status %d (%s), want 200", status, body)
	}
	var out struct {
		User ownAccount `json:"user"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		h.t.Fatalf("decode the signed-in account: %v (%s)", err, body)
	}
	return out.User
}

// members lists the usernames of an organization's accounts, as the
// organization's own directory shows them.
func (h *apiHarness) members(org uuid.UUID) []string {
	h.t.Helper()
	users, err := h.svc.ListUsers(inOrganization(h.t.Context(), org), org)
	if err != nil {
		h.t.Fatalf("list the members of %s: %v", org, err)
	}
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, u.Username)
	}
	slices.Sort(names)
	return names
}

func inOrganization(ctx context.Context, org uuid.UUID) context.Context {
	return entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.String()})
}
