package deviation_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// deploy deploys def under a key of its own and answers the key.
func (h *deviationHarness) deploy(t *testing.T, def *entities.ProcessDefinition) string {
	t.Helper()
	h.deployed++
	def.Project = &entities.Project{ID: h.projID}
	def.Key = fmt.Sprintf("%s-%d", def.Key, h.deployed)
	if _, err := h.svc.CreateDefinition(h.tenantContext(), def); err != nil {
		t.Fatalf("deploy %s: %v", def.Key, err)
	}
	return def.Key
}

// start deploys def under a key of its own and starts one instance of it.
func (h *deviationHarness) start(t *testing.T, def *entities.ProcessDefinition) uuid.UUID {
	t.Helper()
	key := h.deploy(t, def)
	id, err := h.svc.StartProcess(h.tenantContext(), h.projID, key, nil)
	if err != nil {
		t.Fatalf("start %s: %v", key, err)
	}
	return id
}

// twoSteps is start → Approve (alice's) → Sign → end: an instance of it is
// still running once its first step is done.
func twoSteps() *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Key: "deviation-two-steps", Name: "Two steps",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "step", Type: entities.UserTask, Name: "Approve", Assignee: "alice"},
			{ID: "second", Type: entities.UserTask, Name: "Sign"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "step"},
			{ID: "f2", SourceRef: "step", TargetRef: "second"},
			{ID: "f3", SourceRef: "second", TargetRef: "end"},
		},
	}
}

// orderBySize is start → Check the order (declares amount) → Large order? →
// "Second approval" when amount > 100, "Book it" when amount is 0 to 100.
// The gateway has no default: an amount below nothing fits no way out.
func orderBySize() *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Key: "deviation-order-by-size", Name: "Order by size",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "step", Type: entities.UserTask, Name: "Check the order", Assignee: "alice", Properties: testutils.FormDeclaring("amount")},
			{ID: "size", Type: entities.ExclusiveGateway, Name: "Large order?"},
			{ID: "large", Type: entities.UserTask, Name: "Second approval"},
			{ID: "small", Type: entities.UserTask, Name: "Book it"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "step"},
			{ID: "f2", SourceRef: "step", TargetRef: "size"},
			{ID: "over", SourceRef: "size", TargetRef: "large", Condition: "amount > 100"},
			{ID: "upto", SourceRef: "size", TargetRef: "small", Condition: "amount >= 0 and amount <= 100"},
			{ID: "f3", SourceRef: "large", TargetRef: "end"},
			{ID: "f4", SourceRef: "small", TargetRef: "end"},
		},
	}
}

// completeStep does the instance's open task on "step" as its holder would.
func (h *deviationHarness) completeStep(t *testing.T, instanceID uuid.UUID) {
	t.Helper()
	tasks, err := h.svc.ListTasks(h.tenantContext(), h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == "step" {
			if err := h.svc.CompleteTask(h.tenantContext(), task.ID, "alice", nil); err != nil {
				t.Fatalf("complete the step: %v", err)
			}
			return
		}
	}
	t.Fatal("the instance has no task on the step to complete")
}

// Rulings addendum §9. What no plan could be made for is a 400 that says
// which field and why — sent as a preview or as an apply, and before anything
// of the instance is read.
func TestAMalformedDeviationRequestIsA400ThatSaysWhatToFix(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice",
		Properties: testutils.FormDeclaring("approved", "amount")})
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	_, planned := h.previewed(t, admin, instanceID, map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason})
	before := h.everyRow(t)

	fiftyOne := map[string]any{}
	for i := range 51 {
		fiftyOne[fmt.Sprintf("field%02d", i)] = i
	}
	longName := strings.Repeat("é", 256)
	const sayWhichStep = "say which step: node_id is required for a waive and a hold"
	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{"a kind there is not", map[string]any{"kind": "skip", "node_id": "step", "reason": routeReason}, "kind must be waive, cancel or hold"},
		{"no kind", map[string]any{"node_id": "step", "reason": routeReason}, "kind must be waive, cancel or hold"},
		{"a waive naming no step", map[string]any{"kind": "waive", "reason": routeReason}, sayWhichStep},
		{"a hold naming no step", map[string]any{"kind": "hold", "reason": routeReason}, sayWhichStep},
		{"a hold naming spaces", map[string]any{"kind": "hold", "node_id": "   ", "reason": routeReason}, sayWhichStep},
		{"outputs on a cancel", map[string]any{"kind": "cancel", "node_id": "step", "reason": routeReason, "outputs": map[string]any{"approved": true}},
			"outputs are what a waived step counts as; a cancel sets none"},
		{"outputs on a hold", map[string]any{"kind": "hold", "node_id": "step", "reason": routeReason, "outputs": map[string]any{"approved": true}},
			"outputs are what a waived step counts as; a hold sets none"},
		{"an output given as null", map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason, "outputs": map[string]any{"approved": nil}},
			"output approved is null: say what the waiver counts as, or leave it out"},
		{"two outputs given as null", map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason, "outputs": map[string]any{"approved": nil, "amount": nil}},
			"outputs amount, approved are null: say what the waiver counts as, or leave them out"},
		{"an output with no name", map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason, "outputs": map[string]any{"": true}},
			"an output needs the name of the field it sets"},
		{"an output named in 256 characters", map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason, "outputs": map[string]any{longName: true}},
			"“" + strings.Repeat("é", 64) + "…” is too long a name for a waive to set: a field's name is at most 255 characters"},
		{"51 outputs", map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason, "outputs": fiftyOne},
			"a waive sets at most 50 values, and this one names 51"},
		{"outputs heavier than 64 KiB", map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason,
			"outputs": map[string]any{"approved": strings.Repeat("y", 64<<10)}},
			"the outputs are larger than 64 KiB, which is more than the record of a waive keeps"},
	}
	for _, c := range cases {
		for _, mode := range []string{"a preview", "an apply"} {
			body := map[string]any{}
			for name, value := range c.body {
				body[name] = value
			}
			if mode == "an apply" {
				body["visit_key"], body["dry_run"] = planned.Plan.VisitKey, false
			}
			if status, _, raw := h.deviate(t, admin, instanceID, body); status != http.StatusBadRequest || !sameJSON(t, raw, invalid(c.want)) {
				t.Errorf("%s, sent as %s: %d (%.300s), want 400 %s", c.name, mode, status, raw, invalid(c.want))
			}
		}
	}

	// An apply names the plan it previewed.
	const previewFirst = "preview first: an apply names the visit_key of the plan it previewed"
	for name, key := range map[string]any{"no visit key": nil, "a visit key of spaces": "   ", "an empty visit key": ""} {
		body := map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason, "dry_run": false}
		if key != nil {
			body["visit_key"] = key
		}
		if status, _, raw := h.deviate(t, admin, instanceID, body); status != http.StatusBadRequest || !sameJSON(t, raw, invalid(previewFirst)) {
			t.Errorf("an apply with %s: %d (%s), want 400 %s", name, status, raw, invalid(previewFirst))
		}
	}

	// The instance is named in the address, and an address that names none is
	// the caller's to fix.
	notAnID := invalid(`instance id "not-an-id" is not a valid identifier`)
	if status, raw := h.send(t, admin, "/api/v1/instances/not-an-id/deviations", `{"kind":"cancel","reason":"x"}`); status != http.StatusBadRequest || !sameJSON(t, raw, notAnID) {
		t.Errorf("an id that is not one: %d (%s), want 400 %s", status, raw, notAnID)
	}

	// A name of exactly 255 characters is a request like any other: a plan.
	atTheLimit := map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason, "outputs": map[string]any{strings.Repeat("é", 255): true}}
	if status, _, raw := h.deviate(t, admin, instanceID, atTheLimit); status != http.StatusOK {
		t.Errorf("an output named in 255 characters: %d (%.300s), want a 200 plan", status, raw)
	}
	h.requireUnchanged(t, before, "malformed requests")
}

// Rulings addendum §9. A refusal a preview can show is a 200 carrying the
// refusal, and a 400 carrying the same sentences, joined, when it is applied.
func TestARefusedPlanIsA200ToPreviewAndA400ToApply(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	before := h.everyRow(t)
	const sayWhy = "Say why: a reason is required, and it is kept with the record."

	// No reason: the preview says so, and the apply is refused in its words.
	noReason := map[string]any{"kind": "waive", "node_id": "step"}
	status, planned, raw := h.deviate(t, admin, instanceID, noReason)
	if status != http.StatusOK || planned.Plan.Applicable || len(planned.Plan.Refusals) != 1 || planned.Plan.Refusals[0] != sayWhy {
		t.Fatalf("a preview with no reason: %d (%s), want a 200 plan refusing for the reason alone", status, raw)
	}
	noReason["visit_key"], noReason["dry_run"] = planned.Plan.VisitKey, false
	if status, _, raw := h.deviate(t, admin, instanceID, noReason); status != http.StatusBadRequest || !sameJSON(t, raw, invalid(sayWhy)) {
		t.Fatalf("an apply with no reason: %d (%s), want 400 %s", status, raw, invalid(sayWhy))
	}

	// Several refusals: each is listed by the preview, and the apply is
	// answered with all of them, in order, a space between.
	nowhere := map[string]any{"kind": "hold", "node_id": "archive", "reason": strings.Repeat("y", 2001)}
	status, planned, raw = h.deviate(t, admin, instanceID, nowhere)
	wantRefusals := []string{`This process has no step "archive".`, "The reason is longer than 2000 characters; say it more briefly."}
	if status != http.StatusOK || planned.Plan.Applicable || strings.Join(planned.Plan.Refusals, "|") != strings.Join(wantRefusals, "|") {
		t.Fatalf("a preview of a hold at a step there is not, for too long a reason: %d (%.400s), want a 200 plan refusing %q", status, raw, wantRefusals)
	}
	nowhere["visit_key"], nowhere["dry_run"] = planned.Plan.VisitKey, false
	joined := invalid(strings.Join(wantRefusals, " "))
	if status, _, raw := h.deviate(t, admin, instanceID, nowhere); status != http.StatusBadRequest || !sameJSON(t, raw, joined) {
		t.Fatalf("the same, applied: %d (%s), want 400 %s", status, raw, joined)
	}
	h.requireUnchanged(t, before, "plans that refuse")
}

// Rulings addendum §9; plan Ruling 15. A request that conflicts with what has
// been done since its preview — or with what was done for the same visit — is
// a 400 that says so in words: the server has no other class for it. The same
// request sent again is not a conflict: it is answered with its record.
func TestAnApplyThatComesTooLateIsA400ThatSaysWhatHappened(t *testing.T) {
	h := newDeviationRouteHarness(t)
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	oneStep := func() uuid.UUID {
		return h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	}
	refused := func(what string, instanceID uuid.UUID, body map[string]any, sentence string) {
		t.Helper()
		before := h.everyRow(t)
		if status, _, raw := h.deviate(t, admin, instanceID, body); status != http.StatusBadRequest || !sameJSON(t, raw, invalid(sentence)) {
			t.Fatalf("%s: %d (%s), want 400 %s", what, status, raw, invalid(sentence))
		}
		h.requireUnchanged(t, before, what)
	}
	appliedOnce := func(what string, instanceID uuid.UUID, body map[string]any) {
		t.Helper()
		if status, first, raw := h.deviate(t, admin, instanceID, body); status != http.StatusOK || !first.Applied || first.Replayed {
			t.Fatalf("%s: %d (%s), want it applied", what, status, raw)
		}
		before := h.everyRow(t)
		status, again, raw := h.deviate(t, admin, instanceID, body)
		if status != http.StatusOK || !again.Applied || !again.Replayed || again.Deviation == nil {
			t.Fatalf("%s, sent again: %d (%s), want a 200 that says it was replayed", what, status, raw)
		}
		h.requireUnchanged(t, before, what+", sent again")
	}
	withAnotherReason := func(body map[string]any) map[string]any {
		other := map[string]any{}
		for name, value := range body {
			other[name] = value
		}
		other["reason"] = "a different reason, sent later"
		return other
	}

	// The visit already has its act: the same request is replayed, another is
	// told who acted.
	held := oneStep()
	hold, _ := h.previewed(t, admin, held, map[string]any{"kind": "hold", "node_id": "step", "reason": routeReason})
	waiveOfTheHeld, _ := h.previewed(t, admin, held, map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason})
	appliedOnce("a hold", held, hold)
	refused("another hold of the visit already held", held, withAnotherReason(hold), "this instance was already held by boss")

	cancel, _ := h.previewed(t, admin, held, map[string]any{"kind": "cancel", "node_id": "step", "reason": routeReason})
	appliedOnce("a cancel", held, cancel)
	refused("another cancel of the visit already cancelled", held, withAnotherReason(cancel), "this instance was already cancelled by boss")
	if rows := h.rowCount(t, held); rows != 2 {
		t.Fatalf("the ledger holds %d rows, want the hold and the cancel", rows)
	}

	waived := oneStep()
	waive, _ := h.previewed(t, admin, waived, map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason})
	appliedOnce("a waive", waived, waive)
	refused("another waive of the visit already waived", waived, withAnotherReason(waive), "this step was already waived by boss")

	// The instance ended after the preview.
	refused("a waive applied after the instance was cancelled", held, waiveOfTheHeld,
		"this instance is cancelled, so it can no longer be waived; preview again")
	done := oneStep()
	holdOfTheDone, _ := h.previewed(t, admin, done, map[string]any{"kind": "hold", "node_id": "step", "reason": routeReason})
	cancelOfTheDone, _ := h.previewed(t, admin, done, map[string]any{"kind": "cancel", "node_id": "step", "reason": routeReason})
	h.completeStep(t, done)
	refused("a hold applied after the instance finished", done, holdOfTheDone,
		"this instance is completed, so it can no longer be held; preview again")
	refused("a cancel applied after the instance finished", done, cancelOfTheDone,
		"this instance is completed, so it can no longer be cancelled; preview again")

	// The instance is still running, and no longer where the preview found it.
	moved := h.start(t, twoSteps())
	const movedOn = "this instance has moved since you previewed it; preview again"
	stale := map[string]map[string]any{}
	for _, kind := range []string{"waive", "cancel", "hold"} {
		stale[kind], _ = h.previewed(t, admin, moved, map[string]any{"kind": kind, "node_id": "step", "reason": routeReason})
	}
	h.completeStep(t, moved)
	for kind, body := range stale {
		refused("a "+kind+" applied after the step was done by its holder", moved, body, movedOn)
	}
}

// A suspended instance has not ended, so it is not said that it can no longer
// be acted on; and nothing in the product resumes one, so it is not told to
// resume it either (final-wave ruling FW-3). It is refused as what it is, in
// one sentence for all three kinds. Nothing in the product suspends an
// instance today, so the row is written through the repository, as the
// service's own test of this does.
func TestAnApplyOnASuspendedInstanceIsA400ThatSaysItIsSuspended(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	applies := map[string]map[string]any{}
	for _, kind := range []string{"waive", "cancel", "hold"} {
		applies[kind], _ = h.previewed(t, admin, instanceID, map[string]any{"kind": kind, "node_id": "step", "reason": routeReason})
	}
	row, err := h.repo.Process().Get(h.tenantContext(), instanceID)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	row.Status = models.ProcessSuspended
	if err := h.repo.Process().Update(h.tenantContext(), row); err != nil {
		t.Fatalf("suspend the instance: %v", err)
	}
	before := h.everyRow(t)

	for kind, body := range applies {
		want := invalid("this instance is suspended, and a suspended instance is not waived, cancelled or held in place")
		if status, _, raw := h.deviate(t, admin, instanceID, body); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
			t.Errorf("a %s of a suspended instance: %d (%s), want 400 %s", kind, status, raw, want)
		}
	}
	h.requireUnchanged(t, before, "applies on a suspended instance")
}

// Task 6 F6-2. What fails for reasons that are nobody's request is the
// server's, and is answered as that: a 500. Here the step after the waived one
// consults a decision nobody stored; reading it answers "not found", and an
// instance that exists, asked about by somebody who may ask, must not be
// answered as not there — nor as a request they could put right. The status
// is the error's class, never its words: the words may say "not found".
func TestAFailureThatIsTheServersIsA500WhateverItsWordsSay(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.start(t, &entities.ProcessDefinition{
		Key: "deviation-advance-failure", Name: "Refund",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "step", Type: entities.UserTask, Name: "Review the claim", Assignee: "alice", Properties: testutils.FormDeclaring("approved")},
			{ID: "policy", Type: entities.BusinessRuleTask, Name: "Apply the refund policy", Properties: map[string]any{"decision_key": "no-such-decision"}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "step"},
			{ID: "f2", SourceRef: "step", TargetRef: "policy"},
			{ID: "f3", SourceRef: "policy", TargetRef: "end"},
		},
	})
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	apply, planned := h.previewed(t, admin, instanceID, map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason,
		"outputs": map[string]any{"approved": true}})
	if !planned.Plan.Applicable {
		t.Fatalf("the plan refuses, so the advance is never tried and this proves nothing: %q", planned.Plan.Refusals)
	}
	before := h.everyRow(t)

	status, _, raw := h.deviate(t, admin, instanceID, apply)
	if status != http.StatusInternalServerError {
		t.Fatalf("a waive whose next step could not run: %d (%s), want 500", status, raw)
	}
	if !strings.Contains(raw, `"error":"waiving “Review the claim”: `) || !strings.Contains(raw, "no-such-decision") {
		t.Errorf("the 500 does not say what was being done and what failed: %s", raw)
	}
	if strings.Contains(raw, `"plan"`) || strings.Contains(raw, `"applied"`) {
		t.Errorf("the 500 carries a reply as well as the failure: %s", raw)
	}
	h.requireUnchanged(t, before, "a waive whose advance failed")
	if !h.stepIsOpen(t, instanceID) {
		t.Fatal("the step is not open again after the waive was undone")
	}
}
