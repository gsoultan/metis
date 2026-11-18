package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/identity"
	"github.com/gsoultan/metis/server/domains/entities"
)

type fakeIdentity struct {
	principal identity.Principal
	ok        bool
	err       error
	calls     int
}

func (f *fakeIdentity) Authenticate(context.Context, *http.Request) (identity.Principal, bool, error) {
	f.calls++
	return f.principal, f.ok, f.err
}

func (f *fakeIdentity) SetPassword(context.Context, string, string) error { return nil }

func (f *fakeIdentity) RemoveSecondFactor(context.Context, string) error { return nil }

func TestIdentityInterceptor(t *testing.T) {
	t.Parallel()

	known := uuid.New()
	keyID, org := uuid.New(), uuid.New()
	key := identity.Principal{UserID: keyID.String(), ViaKey: true, Roles: map[string][]string{org.String(): {"DESIGNER"}}}
	account := func(_ context.Context, id uuid.UUID) (entities.User, error) {
		if id != known {
			return entities.User{}, errors.New("no such account")
		}
		return entities.User{ID: known, Username: "ada"}, nil
	}

	testCases := []struct {
		name      string
		path      string
		identity  *fakeIdentity
		want      int
		wantCalls int
		admitted  bool
	}{
		{name: "not signed in", path: "/api/v1/processes", identity: &fakeIdentity{}, want: http.StatusUnauthorized, wantCalls: 1},
		{name: "identity store failed", path: "/api/v1/processes", identity: &fakeIdentity{err: errors.New("down")}, want: http.StatusServiceUnavailable, wantCalls: 1},
		{name: "principal is not a uuid", path: "/api/v1/processes", identity: &fakeIdentity{ok: true, principal: identity.Principal{UserID: "ada"}}, want: http.StatusUnauthorized, wantCalls: 1},
		{name: "principal has no engine account", path: "/api/v1/processes", identity: &fakeIdentity{ok: true, principal: identity.Principal{UserID: uuid.NewString()}}, want: http.StatusUnauthorized, wantCalls: 1},
		{name: "session admitted", path: "/api/v1/processes", identity: &fakeIdentity{ok: true, principal: identity.Principal{UserID: known.String()}}, want: http.StatusOK, wantCalls: 1, admitted: true},
		{name: "session manages accounts", path: "/api/v1/users", identity: &fakeIdentity{ok: true, principal: identity.Principal{UserID: known.String()}}, want: http.StatusOK, wantCalls: 1, admitted: true},
		{name: "key admitted to work without an account", path: "/api/v1/processes", identity: &fakeIdentity{ok: true, principal: key}, want: http.StatusOK, wantCalls: 1, admitted: true},
		{name: "key with no organization", path: "/api/v1/processes", identity: &fakeIdentity{ok: true, principal: identity.Principal{UserID: keyID.String(), ViaKey: true}}, want: http.StatusUnauthorized, wantCalls: 1},
		{name: "key refused on accounts", path: "/api/v1/users/" + known.String(), identity: &fakeIdentity{ok: true, principal: key}, want: http.StatusForbidden, wantCalls: 1},
		{name: "key refused on keys", path: "/api/v1/integration-keys", identity: &fakeIdentity{ok: true, principal: key}, want: http.StatusForbidden, wantCalls: 1},
		{name: "key not refused on a lookalike prefix", path: "/api/v1/usersettings", identity: &fakeIdentity{ok: true, principal: key}, want: http.StatusOK, wantCalls: 1, admitted: true},
		{name: "own sign-in is elsewhere", path: "/api/v1/login", identity: &fakeIdentity{ok: true, principal: identity.Principal{UserID: known.String()}}, want: http.StatusNotFound},
		{name: "own password is elsewhere", path: "/api/v1/users/me/password", identity: &fakeIdentity{ok: true, principal: identity.Principal{UserID: known.String()}}, want: http.StatusNotFound},
		{name: "public path skips the identity", path: "/api/v1/setup/status", identity: &fakeIdentity{}, want: http.StatusOK},
		{name: "outside the api", path: "/healthz", identity: &fakeIdentity{}, want: http.StatusOK},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var admitted bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if user, ok := r.Context().Value(pkgauth.UserContextKey).(entities.User); ok {
					admitted = user.ID == known && !user.ViaIntegrationKey() ||
						user.ID == keyID && user.IntegrationKey == keyID
				}
				w.WriteHeader(http.StatusOK)
			})
			handler := NewIdentityHTTPAuthInterceptor(tc.identity, account, nil, []string{"/api/v1/setup/status"}).Wrap(next)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil))

			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
			if tc.identity.calls != tc.wantCalls {
				t.Fatalf("identity asked %d times, want %d", tc.identity.calls, tc.wantCalls)
			}
			if admitted != tc.admitted {
				t.Fatalf("admitted as the account = %v, want %v", admitted, tc.admitted)
			}
		})
	}
}

func TestSessionOnly(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]bool{
		"/api/v1/users":             true,
		"/api/v1/users/x":           true,
		"/api/v1/settings/smtp":     true,
		"/api/v1/usersettings":      false,
		"/api/v1/process-instances": false,
		"/api/v1/organizations/a/b": true,
	} {
		if got := SessionOnly(path); got != want {
			t.Errorf("SessionOnly(%q) = %v, want %v", path, got, want)
		}
	}
}

// A key is a principal of its own: a member of its organization alone, with
// its roles there and none globally, and never an administrator.
func TestAKeyActsInItsOrganizationAlone(t *testing.T) {
	t.Parallel()
	keyID, org, other := uuid.New(), uuid.New(), uuid.New()
	user, ok := keyPrincipal(keyID, map[string][]string{org.String(): {"DESIGNER", "admin", "OPERATOR"}})
	if !ok {
		t.Fatal("a key with an organization was refused")
	}
	if !user.ViaIntegrationKey() || user.ID != keyID || user.Username != "integration:"+keyID.String() {
		t.Fatalf("the key acts as %+v", user)
	}
	if len(user.Roles) != 0 {
		t.Fatalf("the key holds %v globally, want nothing", user.Roles)
	}
	if got := user.RolesIn(org); len(got) != 2 || got[0] != "DESIGNER" || got[1] != "OPERATOR" {
		t.Fatalf("the key holds %v in its organization, want DESIGNER and OPERATOR without ADMIN", got)
	}
	if got := user.RolesIn(other); len(got) != 0 {
		t.Fatalf("the key holds %v in another organization", got)
	}
	if len(user.Organizations) != 1 || user.Organizations[0].ID != org {
		t.Fatalf("the key belongs to %v, want only its organization", user.Organizations)
	}
	if _, ok := keyPrincipal(keyID, map[string][]string{"acme": {"DESIGNER"}}); ok {
		t.Fatal("a key naming an organization that is not an id was admitted")
	}
}
