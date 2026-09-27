package connectcodes_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	pbendpoints "github.com/gsoultan/metis/api/proto/endpoints"
	"github.com/gsoultan/metis/api/proto/services/servicesconnect"
	"github.com/gsoultan/metis/internal/app"
	"github.com/gsoultan/metis/internal/pkg/health"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

const memberPassword = "correct-horse-battery"

// A refusal over Connect is the refusal it is over REST. The Connect handlers
// returned the endpoint chain's errors as they were, and Connect encodes an
// error it did not make as `unknown`, which it sends as HTTP 500. So a member
// asking for something only an administrator may do was answered as a server
// fault, spent the error budget, and told the client nothing it could act on.
// (A service's own refusal travels in the reply's error field, as it always
// has over Connect; that is not changed here.)
func TestAConnectRefusalCarriesTheCodeItsRESTTwinAnswers(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "connect-codes-test", nil, nil, nil)
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, testutils.StormConn(db))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	org, err := svc.CreateOrganization(t.Context(), "Acme", "")
	if err != nil {
		t.Fatalf("create the organization: %v", err)
	}
	tenant := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: org.ID.String()})
	if err := svc.CreateUser(tenant, entities.User{
		Username: "mallory", Roles: []string{entities.RoleUser}, Organizations: []*entities.Organization{{ID: org.ID}},
	}, memberPassword); err != nil {
		t.Fatalf("create mallory: %v", err)
	}
	token := signIn(t, server, "mallory")

	client := &http.Client{Transport: bearerTransport{server.Client().Transport}}
	organizations := servicesconnect.NewOrganizationServiceClient(client, server.URL+"/api/v1")
	for _, tc := range []struct {
		name string
		call func(ctx context.Context) error
		want connect.Code
	}{
		{"a member creating an organization, which needs an administrator", func(ctx context.Context) error {
			_, err := organizations.CreateOrganization(ctx, connect.NewRequest(&pbendpoints.CreateOrganizationRequest{Name: "Globex"}))
			return err
		}, connect.CodePermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := contextWithToken(t.Context(), token)
			err := tc.call(ctx)
			if got := connect.CodeOf(err); got != tc.want {
				t.Errorf("code %v (%v), want %v", got, err, tc.want)
			}
		})
	}

	// Nobody signed in is unauthenticated, not a server fault.
	_, err = organizations.GetOrganization(t.Context(), connect.NewRequest(&pbendpoints.GetOrganizationRequest{Id: org.ID.String()}))
	if got := connect.CodeOf(err); got != connect.CodeUnauthenticated {
		t.Errorf("with nobody signed in: code %v (%v), want %v", got, err, connect.CodeUnauthenticated)
	}
}

type tokenKey struct{}

// contextWithToken carries a bearer token to the client interceptor below.
func contextWithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey{}, token)
}

// bearerTransport sends the token a request's context carries.
type bearerTransport struct{ next http.RoundTripper }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if token, ok := r.Context().Value(tokenKey{}).(string); ok && token != "" {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return b.next.RoundTrip(r)
}

func signIn(t *testing.T, server *httptest.Server, username string) string {
	t.Helper()
	body := `{"username":"` + username + `","password":"` + memberPassword + `"}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/api/v1/login", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build the sign-in request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("sign in as %s: %v", username, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var session struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil || session.Token == "" {
		t.Fatalf("sign in as %s: status %d, %v", username, resp.StatusCode, err)
	}
	return session.Token
}
