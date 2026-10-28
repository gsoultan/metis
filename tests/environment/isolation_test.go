package environment_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// TestARequestOnAnEnvironmentPortReadsThatEnvironmentsDatabase is the property
// the whole design exists for: what one runtime holds is absent from another
// rather than filtered out of it.
//
// It exercises the binding the way a request does — through a handler wrapped
// the way an environment listener wraps it — because the binding being applied
// by the listener rather than read from the request is the security property,
// and a test that set the context itself would prove nothing about that.
func TestARequestOnAnEnvironmentPortReadsThatEnvironmentsDatabase(t *testing.T) {
	mainConn := testutils.SetupTestDB(t)
	stagingDB := testutils.SetupTestDB(t)

	environmentID := uuid.New()
	if pool := stagingDB; pool != nil {
		mainConn.RegisterEnvironment(environmentID, pool.Main())
	}

	repo := repositories.NewRepository(mainConn)
	system := entities.WithSystemContext(context.Background())

	// One definition in each database, with different keys, so which database
	// answered is visible in the result rather than inferred from a count.
	writeDefinition(t, mainConn, "lives-in-main")
	writeDefinition(t, stagingDB, "lives-in-staging")

	var seen []string
	handler := requestScoped(environmentID, func(r *http.Request) {
		defs, err := repo.Definition().List(entities.WithSystemContext(r.Context()))
		if err != nil {
			t.Errorf("list definitions on the environment port: %v", err)
			return
		}
		for _, def := range defs {
			seen = append(seen, def.Key)
		}
	})

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/definitions", nil))

	if len(seen) != 1 || seen[0] != "lives-in-staging" {
		t.Fatalf("a request on the staging port saw %v; it must see only staging's own definitions", seen)
	}

	// And the main port still sees only its own.
	fromMain, err := repo.Definition().List(system)
	if err != nil {
		t.Fatalf("list definitions on the main port: %v", err)
	}
	if len(fromMain) != 1 || fromMain[0].Key != "lives-in-main" {
		t.Fatalf("the main port saw %d definitions, first %q; it must see only its own", len(fromMain), keyOf(fromMain))
	}
}

// TestIdentityStillResolvesAgainstTheMainDatabase covers the half that is easy
// to get wrong in the other direction.
//
// An environment's schema has the identity tables too — one migration list
// rather than two — and they are empty. A request on an environment port that
// authenticated against them would be refused with a valid token, because the
// account is real and the database being asked is the wrong one.
func TestIdentityStillResolvesAgainstTheMainDatabase(t *testing.T) {
	mainConn := testutils.SetupTestDB(t)
	stagingDB := testutils.SetupTestDB(t)

	environmentID := uuid.New()
	if pool := stagingDB; pool != nil {
		mainConn.RegisterEnvironment(environmentID, pool.Main())
	}

	repo := repositories.NewRepository(mainConn)
	account := models.UserModel{
		Base:     models.Base{ID: models.FromUUID(uuid.New())},
		Username: "ada",
		FullName: "Ada Lovelace",
		Roles:    []string{entities.RoleAdmin},
	}
	if err := repo.User().Create(entities.WithSystemContext(t.Context()), account, ""); err != nil {
		t.Fatalf("seed the account in the main database: %v", err)
	}

	var found string
	var lookupErr error
	handler := requestScoped(environmentID, func(r *http.Request) {
		user, err := repo.User().GetByUsername(entities.WithSystemContext(r.Context()), "ada")
		lookupErr = err
		found = user.Username
	})
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/tasks", nil))

	if lookupErr != nil {
		t.Fatalf("an account that exists could not be resolved on an environment port: %v", lookupErr)
	}
	if found != "ada" {
		t.Fatalf("resolved %q; accounts are installation-wide and must resolve on every port", found)
	}
}

// TestAnEnvironmentWithNoConnectionIsRefusedNotServedFromMain is the fail-closed
// case. A fallback here would succeed, silently, and write one runtime's rows
// into another's store.
func TestAnEnvironmentWithNoConnectionIsRefusedNotServedFromMain(t *testing.T) {
	mainConn := testutils.SetupTestDB(t)

	repo := repositories.NewRepository(mainConn)
	writeDefinition(t, mainConn, "lives-in-main")

	// Registered nowhere: this is an environment whose database would not open.
	unopened := uuid.New()

	var err error
	handler := requestScoped(unopened, func(r *http.Request) {
		_, err = repo.Definition().List(entities.WithSystemContext(r.Context()))
	})
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/definitions", nil))

	if err == nil {
		t.Fatal("a request bound to an environment with no database succeeded; it must be refused, not answered from the main database")
	}
}

// requestScoped wraps a body the way an environment listener wraps the
// application handler.
func requestScoped(environmentID uuid.UUID, body func(*http.Request)) http.Handler {
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { body(r) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(db.Bind(r.Context(), environmentID)))
	})
}

func writeDefinition(t *testing.T, db *db.Conn, key string) {
	t.Helper()
	testutils.SeedDefinition(t, db, testutils.SeedProject(t, db), key)
}

func keyOf(defs []models.ProcessDefinitionModel) string {
	if len(defs) == 0 {
		return ""
	}
	return defs[0].Key
}
