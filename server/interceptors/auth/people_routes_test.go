package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

const (
	aPeopleToken   = "v4.local.c2VhbGVkLWZvci1hLXBlcnNvbg"
	aPlatformToken = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.c2ln"
)

// peopleRoutes is the interceptor as the server builds it: platform tokens
// through platform, People tokens through people. Each strategy admits only
// its own token and records that it was asked, so a test can say not only
// that a request was refused but that the wrong rules never saw it.
func peopleRoutes(t *testing.T) (http.Handler, *bool, *bool, *context.Context) {
	t.Helper()
	platformAsked, peopleAsked := false, false
	var reached context.Context
	platform := strategyFunc(func(_ context.Context, token string) (any, error) {
		platformAsked = true
		if token != aPlatformToken {
			return nil, pkgauth.ErrUnauthorized
		}
		return entities.User{Username: "admin"}, nil
	})
	people := strategyFunc(func(_ context.Context, token string) (any, error) {
		peopleAsked = true
		if token != aPeopleToken {
			return nil, pkgauth.ErrUnauthorized
		}
		return entities.Person{ID: uuid.New(), Username: "ada"}, nil
	})
	handler := NewMandatoryHTTPAuthInterceptorWithPeople(platform, people, []string{"/api/v1/login"}).
		Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = r.Context()
			w.WriteHeader(http.StatusOK)
		}))
	return handler, &platformAsked, &peopleAsked, &reached
}

func call(handler http.Handler, method, path, token string) int {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code
}

func TestAPeopleTokenIsRefusedOnEveryPlatformRoute(t *testing.T) {
	handler, platformAsked, peopleAsked, _ := peopleRoutes(t)
	for _, path := range []string{"/api/v1/tasks", "/api/v1/tasks/x/complete", "/api/v1/users/me", "/api/v1/people"} {
		if code := call(handler, http.MethodGet, path, aPeopleToken); code != http.StatusUnauthorized {
			t.Errorf("%s with a People token = %d, want 401", path, code)
		}
	}
	if *platformAsked || *peopleAsked {
		t.Fatalf("a People token on a platform route reached a strategy (platform %v, people %v)", *platformAsked, *peopleAsked)
	}
}

func TestAPlatformTokenIsRefusedOnEveryPeopleRoute(t *testing.T) {
	handler, platformAsked, _, _ := peopleRoutes(t)
	for _, path := range []string{"/api/v1/people/me", "/api/v1/people/tasks"} {
		if code := call(handler, http.MethodGet, path, aPlatformToken); code != http.StatusUnauthorized {
			t.Errorf("%s with a platform token = %d, want 401", path, code)
		}
	}
	if *platformAsked {
		t.Fatal("a People route asked the platform's rules")
	}
}

func TestAPeopleTokenSignsInAPersonAndNoUser(t *testing.T) {
	handler, _, _, reached := peopleRoutes(t)
	if code := call(handler, http.MethodGet, "/api/v1/people/me", aPeopleToken); code != http.StatusOK {
		t.Fatalf("People route with a People token = %d, want 200", code)
	}
	if _, ok := PersonFrom(*reached); !ok {
		t.Fatal("no person on the request")
	}
	if user := (*reached).Value(pkgauth.UserContextKey); user != nil {
		t.Fatalf("a People token put a User on the request: %v", user)
	}
}

func TestThePeopleSignInNeedsNoToken(t *testing.T) {
	handler, _, peopleAsked, _ := peopleRoutes(t)
	if code := call(handler, http.MethodPost, PeopleSignInPath, ""); code != http.StatusOK {
		t.Fatalf("POST %s with no token = %d, want 200", PeopleSignInPath, code)
	}
	if *peopleAsked {
		t.Fatal("the sign-in asked for a token")
	}
	if code := call(handler, http.MethodGet, "/api/v1/people/me", ""); code != http.StatusUnauthorized {
		t.Fatalf("a People route with no token = %d, want 401", code)
	}
}

func TestWithoutPeopleRulesEveryPeopleRouteButTheSignInIsRefused(t *testing.T) {
	platform := strategyFunc(func(context.Context, string) (any, error) {
		return nil, errors.New("not asked")
	})
	handler := NewMandatoryHTTPAuthInterceptor(platform, nil).
		Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	if code := call(handler, http.MethodGet, "/api/v1/people/me", aPeopleToken); code != http.StatusUnauthorized {
		t.Fatalf("People route without People rules = %d, want 401", code)
	}
	if code := call(handler, http.MethodPost, PeopleSignInPath, ""); code != http.StatusOK {
		t.Fatalf("the sign-in without People rules = %d, want 200", code)
	}
}

func TestThePersonChainAdmitsOnlyAPerson(t *testing.T) {
	ok := func(context.Context, any) (any, error) { return "ok", nil }
	guarded := NewRequirePerson().Intercept(ok)

	user := context.WithValue(t.Context(), pkgauth.UserContextKey, entities.User{Username: "admin"})
	if _, err := guarded(user, nil); !errors.Is(err, pkgauth.ErrUnauthorized) {
		t.Fatalf("a User through the person chain: %v, want ErrUnauthorized", err)
	}
	person := context.WithValue(t.Context(), pkgauth.PersonContextKey, entities.Person{Username: "ada"})
	if _, err := guarded(person, nil); err != nil {
		t.Fatalf("a person through the person chain: %v", err)
	}
}
