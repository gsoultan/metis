package postgres_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/tests/testutils"
)

// Which instances still have a row: a removed one does, an id never written
// does not.
func TestExistingSeesRemovedInstancesToo(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 4)
	repo, engine, projID, ctx := newPostgresEngine(t, db)
	if _, err := serviceimpl.NewDefinitionService(repo).CreateDefinition(ctx, paymentDefinition(projID, "order-payment-existing")); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	live, err := engine.StartProcess(ctx, projID, "order-payment-existing", map[string]any{"orderId": "order-1"})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	removed, err := engine.StartProcess(ctx, projID, "order-payment-existing", map[string]any{"orderId": "order-2"})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	if err := processinstance.Delete(ctx, testutils.Executor(db), removed); err != nil {
		t.Fatalf("remove: %v", err)
	}
	never := uuid.New()

	if _, err := repo.Process().Existing(ctx, []uuid.UUID{live}); err == nil {
		t.Error("a tenant asked which instances exist in every tenant and was answered")
	}
	found, err := repo.Process().Existing(entities.WithSystemContext(t.Context()), []uuid.UUID{live, removed, never})
	if err != nil {
		t.Fatalf("existing: %v", err)
	}
	if !found[live] || !found[removed] || found[never] || len(found) != 2 {
		t.Errorf("found %v; want the live and the removed instance, not the one never written", found)
	}
}
