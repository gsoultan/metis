package handlers_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/domains/validation"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// A request with no definition must be rejected, not crash the handler.
//
// CreateDefinitionRequest carries `Definition *entities.ProcessDefinition`, so
// any request whose body omits or misspells the "definition" key decodes to a
// nil pointer. CreateDefinition passed that straight to def.Accept, whose
// visitor dereferences pd.Key — a nil dereference on a path reachable by any
// authenticated caller sending `{}`.
//
// Found by importing the examples in docs/examples/ with the wrong envelope:
// the request never returned and no line was written to the request log,
// because the panic unwound before the logging interceptor could record the
// call. There is no recovery middleware in the chain, so nothing turned it into
// a response either.
func TestCreateDefinition_RejectsANilDefinitionInsteadOfPanicking(t *testing.T) {
	db := testutils.SetupTestDB(t)
	svc := serviceimpl.NewDefinitionService(repositories.NewRepository(db))

	// Fails the test rather than the process: without the recover the panic
	// takes the whole run down and reports nothing useful about where it came
	// from.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a request with no definition panicked instead of returning an error: %v", r)
		}
	}()

	_, err := svc.CreateDefinition(t.Context(), nil)
	if err == nil {
		t.Fatal("a nil definition was accepted")
	}
	if !strings.Contains(err.Error(), "definition") {
		t.Fatalf("error does not say what was wrong with the request: %v", err)
	}
}

// The visitor is the piece that dereferences, so it is pinned separately: a
// nil-check in the service alone would leave every other Accept caller exposed.
func TestVisitDefinition_ToleratesANilDefinition(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("validating a nil definition panicked: %v", r)
		}
	}()

	var pd *entities.ProcessDefinition
	pd.Accept(validation.NewVisitor())
}
