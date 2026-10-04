package impl

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// The visit key is the identity of the work a command acts on: derived, so a
// retry computes the same one, and changed by exactly what makes it different
// work — a task created, completed or withdrawn on the step, a token moved,
// another version, another kind. A claim changes none of those.
func TestDeviationVisitKey(t *testing.T) {
	t.Parallel()
	step := &entities.Node{ID: "opsApprove"}
	instance := entities.ProcessInstance{
		ID:         uuid.Must(uuid.NewV7()),
		Definition: &entities.ProcessDefinition{ID: uuid.Must(uuid.NewV7())},
		Tokens:     []entities.Token{{ID: uuid.Must(uuid.NewV7()), Node: step}, {ID: uuid.Must(uuid.NewV7()), Node: &entities.Node{ID: "other"}}},
	}
	a, b := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	key := deviationVisitKey(instance, entities.DeviationWaive, "opsApprove", []uuid.UUID{a, b})

	if !strings.HasPrefix(key, "dv1-") || len(key) != 36 {
		t.Fatalf("key %q: want dv1- and 32 characters", key)
	}
	if again := deviationVisitKey(instance, entities.DeviationWaive, "opsApprove", []uuid.UUID{b, a}); again != key {
		t.Error("the order the tasks were listed in changed the key")
	}
	if other := deviationVisitKey(instance, entities.DeviationHold, "opsApprove", []uuid.UUID{a, b}); other == key {
		t.Error("a hold and a waive of the same work share a key")
	}
	if other := deviationVisitKey(instance, entities.DeviationWaive, "opsApprove", []uuid.UUID{a}); other == key {
		t.Error("a task leaving the step did not change the key")
	}
	moved := instance
	moved.Tokens = []entities.Token{{ID: uuid.Must(uuid.NewV7()), Node: step}}
	if other := deviationVisitKey(moved, entities.DeviationWaive, "opsApprove", []uuid.UUID{a, b}); other == key {
		t.Error("a new token on the step did not change the key")
	}
	elsewhere := instance
	elsewhere.Tokens = append([]entities.Token{}, instance.Tokens[0], entities.Token{ID: uuid.Must(uuid.NewV7()), Node: &entities.Node{ID: "other"}})
	if other := deviationVisitKey(elsewhere, entities.DeviationWaive, "opsApprove", []uuid.UUID{a, b}); other != key {
		t.Error("a token moving somewhere else changed the key of work at this step")
	}

	// A cancel that names no step is about the whole instance, which is how an
	// instance holding nothing is closed: any token, on any step, and any open
	// task makes it different work.
	nothing := instance
	nothing.Tokens = nil
	closing := deviationVisitKey(nothing, entities.DeviationCancel, "", nil)
	if !strings.HasPrefix(closing, "dv1-") || len(closing) != 36 {
		t.Fatalf("key %q for an instance that holds nothing: want dv1- and 32 characters", closing)
	}
	if arrived := deviationVisitKey(instance, entities.DeviationCancel, "", nil); arrived == closing {
		t.Error("a token arriving on an instance that held none did not change the key of a cancel that names no step")
	}
	if reopened := deviationVisitKey(nothing, entities.DeviationCancel, "", []uuid.UUID{a}); reopened == closing {
		t.Error("an open task appearing did not change the key of a cancel that names no step")
	}
}

// What a key is made of besides the tasks and tokens of the step: which
// instance, which version of its process, which step. And what it is not made
// of: anything about the instance a claim, a comment or a variable changes.
func TestDeviationVisitKeyCoversTheInstanceItsVersionAndTheStep(t *testing.T) {
	t.Parallel()
	step := &entities.Node{ID: "opsApprove"}
	first, second := entities.Token{ID: uuid.Must(uuid.NewV7()), Node: step}, entities.Token{ID: uuid.Must(uuid.NewV7()), Node: step}
	instance := entities.ProcessInstance{
		ID:         uuid.Must(uuid.NewV7()),
		Definition: &entities.ProcessDefinition{ID: uuid.Must(uuid.NewV7())},
		Status:     entities.ProcessActive,
		Tokens:     []entities.Token{first, second},
	}
	task := uuid.Must(uuid.NewV7())
	key := deviationVisitKey(instance, entities.DeviationWaive, "opsApprove", []uuid.UUID{task})

	another := instance
	another.ID = uuid.Must(uuid.NewV7())
	migrated := instance
	migrated.Definition = &entities.ProcessDefinition{ID: uuid.Must(uuid.NewV7())}
	versionless := instance
	versionless.Definition = nil
	differs := map[string]string{
		"another instance":                  deviationVisitKey(another, entities.DeviationWaive, "opsApprove", []uuid.UUID{task}),
		"another version of the process":    deviationVisitKey(migrated, entities.DeviationWaive, "opsApprove", []uuid.UUID{task}),
		"an instance that names no version": deviationVisitKey(versionless, entities.DeviationWaive, "opsApprove", []uuid.UUID{task}),
		"another step":                      deviationVisitKey(instance, entities.DeviationWaive, "salesApprove", []uuid.UUID{task}),
		"a cancel at the same step":         deviationVisitKey(instance, entities.DeviationCancel, "opsApprove", []uuid.UUID{task}),
	}
	for what, other := range differs {
		if other == key {
			t.Errorf("%s has the key of the work it was compared with", what)
		}
	}

	same := instance
	same.Tokens = []entities.Token{second, first}
	same.Status = entities.ProcessSuspended
	same.Variables = map[string]any{"note": "left by somebody else"}
	same.CompletedNodes = []*entities.Node{{ID: "start"}}
	if again := deviationVisitKey(same, entities.DeviationWaive, "opsApprove", []uuid.UUID{task}); again != key {
		t.Error("the order of the tokens, the instance's variables or its status changed the key; none of them is the work at the step")
	}
}

// The key is a hash over fields that say how long they are, so what a
// definition's author calls a step cannot be chosen to read as another
// command's tasks and tokens.
func TestDeviationVisitKeyIsNotSteeredByWhatAStepIsCalled(t *testing.T) {
	t.Parallel()
	task, token := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	instance := entities.ProcessInstance{ID: uuid.Must(uuid.NewV7()), Definition: &entities.ProcessDefinition{ID: uuid.Must(uuid.NewV7())}}

	plain := instance
	plain.Tokens = []entities.Token{{ID: token, Node: &entities.Node{ID: "x"}}}
	honest := deviationVisitKey(plain, entities.DeviationWaive, "x", []uuid.UUID{task})

	// Step ids that spell out, after "x", what the honest command's task and
	// token would be written as under separators.
	for _, spelled := range []string{
		"x\x00" + task.String() + "\x00" + token.String(),
		"x," + task.String() + "," + token.String(),
		"x" + string(task[:]) + string(token[:]),
		"x\x00" + string(task[:]) + "\x00" + string(token[:]),
	} {
		if forged := deviationVisitKey(instance, entities.DeviationWaive, spelled, nil); forged == honest {
			t.Errorf("a step called %q has the key of step x with its task and token", spelled)
		}
	}
	// And a task is not mistaken for a token.
	swapped := instance
	swapped.Tokens = []entities.Token{{ID: task, Node: &entities.Node{ID: "x"}}}
	if deviationVisitKey(swapped, entities.DeviationWaive, "x", []uuid.UUID{token}) == honest {
		t.Error("a task's id read as a token's, and the token's as a task's, gave the same key")
	}
}
