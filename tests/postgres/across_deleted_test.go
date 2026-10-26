package postgres_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// The three reads that count deleted rows. Every other read leaves them out,
// so each of these is a place where leaving them out would be the bug.

// The unique key on (project, key, version) covers deleted rows, so a version
// handed out once is never handed out again, even after its row is deleted.
func TestADeletedProcessVersionIsNotReissued(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 4)
	repo, _, projID, ctx := newPostgresEngine(t, db)
	defSvc := serviceimpl.NewDefinitionService(repo)

	if next, err := repo.Definition().NextVersion(ctx, projID, "reissue"); err != nil || next != 1 {
		t.Fatalf("a key never deployed: next version %d (err %v), want 1", next, err)
	}
	if _, err := defSvc.CreateDefinition(ctx, paymentDefinition(projID, "reissue")); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	second, err := defSvc.CreateDefinition(ctx, paymentDefinition(projID, "reissue"))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	if err := repo.Definition().Delete(ctx, second); err != nil {
		t.Fatalf("delete v2: %v", err)
	}
	if next, err := repo.Definition().NextVersion(ctx, projID, "reissue"); err != nil || next != 3 {
		t.Errorf("after deleting v2: next version %d (err %v), want 3", next, err)
	}
	if _, err := defSvc.CreateDefinition(ctx, paymentDefinition(projID, "reissue")); err != nil {
		t.Errorf("deploying again after a delete: %v", err)
	}
}

func TestADeletedDecisionVersionIsNotReissued(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 4)
	repo, _, projID, ctx := newPostgresEngine(t, db)
	decisionSvc := serviceimpl.NewDecisionService(repo, serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))
	deploy := func() (uuid.UUID, error) {
		return decisionSvc.CreateDecision(ctx, entities.DecisionDefinition{
			Project:   &entities.Project{ID: projID},
			Key:       "reissue",
			Name:      "Reissue",
			HitPolicy: entities.HitPolicyFirst,
			Inputs:    []entities.DecisionInput{{ID: "in1", Expression: "amount", Type: "number"}},
			Outputs:   []entities.DecisionOutput{{ID: "out1", Name: "band", Type: "string"}},
			Rules:     []entities.DecisionRule{{Inputs: []string{"> 10"}, Outputs: []any{"HIGH"}}},
		})
	}

	if next, err := repo.Decision().NextVersion(ctx, projID, "reissue"); err != nil || next != 1 {
		t.Fatalf("a key never deployed: next version %d (err %v), want 1", next, err)
	}
	if _, err := deploy(); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	second, err := deploy()
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	if err := repo.Decision().Delete(ctx, second); err != nil {
		t.Fatalf("delete v2: %v", err)
	}
	if next, err := repo.Decision().NextVersion(ctx, projID, "reissue"); err != nil || next != 3 {
		t.Errorf("after deleting v2: next version %d (err %v), want 3", next, err)
	}
	if _, err := deploy(); err != nil {
		t.Errorf("deploying again after a delete: %v", err)
	}
}

// A database whose every account was deleted has still been used, and setup
// must not hand it to the next visitor as new.
func TestADatabaseWhoseAccountsWereAllDeletedIsStillAnInstallation(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 4)
	repo, _, _, ctx := newPostgresEngine(t, db)
	users := repo.User()

	if held, err := users.HasAccounts(ctx); err != nil || held {
		t.Fatalf("a database with no accounts: held %v (err %v), want false", held, err)
	}
	id := uuid.New()
	if err := users.Create(ctx, models.UserModel{Base: models.Base{ID: models.UUID(id)}, Username: "gone"}, "x"); err != nil {
		t.Fatalf("create an account: %v", err)
	}
	if err := users.Delete(ctx, id); err != nil {
		t.Fatalf("delete the account: %v", err)
	}
	if held, err := users.HasAccounts(ctx); err != nil || !held {
		t.Errorf("every account deleted: held %v (err %v), want true", held, err)
	}
}
