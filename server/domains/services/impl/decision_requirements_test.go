package impl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// countingTableEvaluator counts how often each decision's table is run.
type countingTableEvaluator struct {
	next contracts.DecisionTableEvaluator

	mu   sync.Mutex
	runs map[string]int
}

func (c *countingTableEvaluator) EvaluateTable(ctx context.Context, def entities.DecisionDefinition, variables map[string]any) (entities.DecisionResult, error) {
	c.mu.Lock()
	c.runs[def.Key]++
	c.mu.Unlock()
	return c.next.EvaluateTable(ctx, def, variables)
}

// constantDecision answers 1 under its own key, whatever it is asked.
func constantDecision(projectID uuid.UUID, key string, requires ...string) entities.DecisionDefinition {
	return entities.DecisionDefinition{
		Project:           &entities.Project{ID: projectID},
		Key:               key,
		RequiredDecisions: requires,
		Inputs:            []entities.DecisionInput{{ID: "in", Expression: "1", Type: "number"}},
		Outputs:           []entities.DecisionOutput{{ID: "out", Name: "value", Type: "number"}},
		Rules:             []entities.DecisionRule{{Inputs: []string{"-"}, Outputs: []any{1}}},
	}
}

// TestADecisionRequiredByManyIsEvaluatedOnce builds requirements four layers
// deep and two wide, each decision in a layer requiring both in the next.
// Without reusing answers the bottom layer ran 2^4 times each; now every
// decision runs once per evaluation.
func TestADecisionRequiredByManyIsEvaluatedOnce(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	counter := &countingTableEvaluator{next: NewDecisionTableEvaluator(NewFEELEvaluator()), runs: map[string]int{}}
	svc := NewDecisionService(repo, counter)
	projectID, ctx := seedProject(t, repo)

	const layers = 4
	name := func(layer int, side string) string { return fmt.Sprintf("layer%d-%s", layer, side) }
	for layer := layers; layer >= 1; layer-- {
		for _, side := range []string{"a", "b"} {
			var requires []string
			if layer < layers {
				requires = []string{name(layer+1, "a"), name(layer+1, "b")}
			}
			mustCreateDecision(t, svc, ctx, constantDecision(projectID, name(layer, side), requires...))
		}
	}
	mustCreateDecision(t, svc, ctx, constantDecision(projectID, "top", name(1, "a"), name(1, "b")))

	if _, err := svc.Evaluate(ctx, projectID, "top", 0, nil); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	for key, n := range counter.runs {
		if n != 1 {
			t.Errorf("%s was evaluated %d times in one evaluation, want once", key, n)
		}
	}
	if got, want := len(counter.runs), 2*layers+1; got != want {
		t.Errorf("%d decisions were evaluated, want %d", got, want)
	}
}

// TestADecisionThatRequiresTheSameDecisionTwiceIsRefused: a requirement is
// read by its key, so listing one twice says nothing more and only doubled
// the work under it.
func TestADecisionThatRequiresTheSameDecisionTwiceIsRefused(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := NewDecisionService(repo, NewDecisionTableEvaluator(NewFEELEvaluator()))
	projectID, ctx := seedProject(t, repo)
	mustCreateDecision(t, svc, ctx, constantDecision(projectID, "base"))

	_, err := svc.CreateDecision(ctx, constantDecision(projectID, "twice", "base", "base"))
	if err == nil {
		t.Fatal("a decision requiring base twice was saved")
	}
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "base") {
		t.Fatalf("the refusal is not the caller's mistake naming the repeated key: %v", err)
	}
}

// TestAStoredDecisionRequiringTheSameDecisionTwiceEvaluatesItOnce covers a
// version saved before the refusal existed: it still runs, and the repeated
// requirement is evaluated once.
func TestAStoredDecisionRequiringTheSameDecisionTwiceEvaluatesItOnce(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	counter := &countingTableEvaluator{next: NewDecisionTableEvaluator(NewFEELEvaluator()), runs: map[string]int{}}
	svc := NewDecisionService(repo, counter)
	projectID, ctx := seedProject(t, repo)
	mustCreateDecision(t, svc, ctx, constantDecision(projectID, "base"))

	old := constantDecision(projectID, "twice", "base", "base")
	old.ID = uuid.New()
	old.Version = 1
	if err := repo.Decision().Create(ctx, adapters.DecisionModelAdapter{Decision: old}.ToModel()); err != nil {
		t.Fatalf("store the version as an older save would have: %v", err)
	}
	if err := repo.Decision().MakeLive(ctx, projectID, old.Key, 1); err != nil {
		t.Fatalf("make it live: %v", err)
	}

	if _, err := svc.Evaluate(ctx, projectID, "twice", 0, nil); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if n := counter.runs["base"]; n != 1 {
		t.Errorf("base was evaluated %d times, want once", n)
	}
}
