package impl

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
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

// A cancel ends the whole instance and withdraws everything open on it,
// whichever step it names. So its key is of the whole instance: a token
// moving on another branch, or a task opening there, is work the preview did
// not show, and makes it a different visit. A waive and a hold of the same
// step act on that step alone, and their keys do not move.
func TestDeviationVisitKeyOfACancelCoversTheWholeInstance(t *testing.T) {
	t.Parallel()
	here := entities.Token{ID: uuid.Must(uuid.NewV7()), Node: &entities.Node{ID: "stock"}}
	there := entities.Token{ID: uuid.Must(uuid.NewV7()), Node: &entities.Node{ID: "credit"}}
	instance := entities.ProcessInstance{
		ID:         uuid.Must(uuid.NewV7()),
		Definition: &entities.ProcessDefinition{ID: uuid.Must(uuid.NewV7())},
		Tokens:     []entities.Token{here, there},
	}
	stockTask, creditTask := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	everyTask := []uuid.UUID{stockTask, creditTask}
	cancel := deviationVisitKey(instance, entities.DeviationCancel, "stock", everyTask)

	// The other branch moves on: its token is a new one, on another step.
	moved := instance
	moved.Tokens = []entities.Token{here, {ID: uuid.Must(uuid.NewV7()), Node: &entities.Node{ID: "creditAgain"}}}
	if deviationVisitKey(moved, entities.DeviationCancel, "stock", everyTask) == cancel {
		t.Error("a token moving on another branch did not change the key of a cancel, which ends that branch too")
	}
	// A task opens on the other branch.
	if deviationVisitKey(instance, entities.DeviationCancel, "stock", append(slices.Clone(everyTask), uuid.Must(uuid.NewV7()))) == cancel {
		t.Error("a task opening on another branch did not change the key of a cancel, which withdraws it too")
	}
	// The step it names is where the record says the instance stood.
	if deviationVisitKey(instance, entities.DeviationCancel, "credit", everyTask) == cancel {
		t.Error("a cancel at one step and a cancel at another share a key")
	}
	if deviationVisitKey(instance, entities.DeviationCancel, "", everyTask) == cancel {
		t.Error("a cancel that names a step and one that names none share a key")
	}

	for _, kind := range []entities.DeviationKind{entities.DeviationWaive, entities.DeviationHold} {
		atStock := deviationVisitKey(instance, kind, "stock", []uuid.UUID{stockTask})
		if deviationVisitKey(moved, kind, "stock", []uuid.UUID{stockTask}) != atStock {
			t.Errorf("a token moving on another branch changed the key of a %s of this step", kind)
		}
	}
}

// A hold's key covers the incidents on the step it holds: which there are,
// and whether each is open. A hold raises one, or uses the one that is open,
// so a step with an incident open, one whose incident was resolved and one
// that never had any are three different things to hold — and a preview of one
// must not be applied to another. A waive and a cancel do nothing with a
// step's incidents, and their keys do not move.
func TestDeviationVisitKeyOfAHoldCoversTheIncidentsOnItsStep(t *testing.T) {
	t.Parallel()
	instance := entities.ProcessInstance{
		ID:         uuid.Must(uuid.NewV7()),
		Definition: &entities.ProcessDefinition{ID: uuid.Must(uuid.NewV7())},
		Tokens:     []entities.Token{{ID: uuid.Must(uuid.NewV7()), Node: &entities.Node{ID: "approve"}}},
	}
	task := []uuid.UUID{uuid.Must(uuid.NewV7())}
	incident := func(status models.IncidentStatus) models.IncidentModel {
		made := models.IncidentModel{NodeID: "approve", Status: status}
		made.ID = models.UUID(uuid.Must(uuid.NewV7()))
		return made
	}
	first, second := incident(models.IncidentOpen), incident(models.IncidentOpen)
	resolved := first
	resolved.Status = models.IncidentResolved

	never := deviationVisitKey(instance, entities.DeviationHold, "approve", task)
	held := deviationVisitKey(instance, entities.DeviationHold, "approve", task, first)
	decided := deviationVisitKey(instance, entities.DeviationHold, "approve", task, resolved)
	heldAgain := deviationVisitKey(instance, entities.DeviationHold, "approve", task, resolved, second)
	keys := map[string]string{
		"a step never held": never, "a step with an incident open": held,
		"a step whose incident was resolved": decided, "a step held again after that": heldAgain,
	}
	seen := map[string]string{}
	for what, key := range keys {
		if other, taken := seen[key]; taken {
			t.Errorf("%s and %s have the same key", what, other)
		}
		seen[key] = what
	}
	if deviationVisitKey(instance, entities.DeviationHold, "approve", task, second, resolved) != heldAgain {
		t.Error("the order the incidents were listed in changed the key")
	}
	if deviationVisitKey(instance, entities.DeviationHold, "approve", task) != never {
		t.Error("asking twice gave two keys")
	}

	for _, kind := range []entities.DeviationKind{entities.DeviationWaive, entities.DeviationCancel} {
		if deviationVisitKey(instance, kind, "approve", task, first) != deviationVisitKey(instance, kind, "approve", task) {
			t.Errorf("an incident on the step changed the key of a %s, which does nothing with it", kind)
		}
	}

	// An incident's id is not read as a task's or a token's, nor its status as
	// part of the next incident.
	asTask := deviationVisitKey(instance, entities.DeviationHold, "approve", append(slices.Clone(task), uuid.UUID(first.ID)))
	if asTask == held {
		t.Error("an incident on the step and a task with its id gave the same key")
	}
}
