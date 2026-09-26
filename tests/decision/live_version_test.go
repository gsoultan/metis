package decision_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/models"
)

// An evaluation that names no version — a business rule task with no version
// binding, the evaluate API called without one, a decision another decision
// requires — reads the key's live version. It read the newest, so a version
// saved to be reviewed was in force the moment it was saved, and staging one
// was not possible at all.

// stageVersion stores version of key, answering band with answer for any
// score, without making it live: the state a staged save leaves, and the one
// an old installation's rows are in.
func stageVersion(t *testing.T, h *businessRuleHarness, key string, version int, answer string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if err := h.repo.Decision().Create(h.ctx, models.DecisionDefinitionModel{
		Base:      models.Base{ID: models.UUID(id)},
		ProjectID: models.UUID(h.projectID),
		Key:       key,
		Name:      "Band",
		Version:   version,
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []models.DecisionInput{{ID: "in", Label: "Score", Expression: "score", Type: "number"}},
		Outputs:   []models.DecisionOutput{{ID: "out", Label: "Band", Name: "band", Type: "string"}},
		Rules:     []models.DecisionRule{{ID: "r1", Inputs: []string{"-"}, Outputs: []any{answer}}},
	}); err != nil {
		t.Fatalf("stage v%d of %s: %v", version, key, err)
	}
	return id
}

// bandDecision answers band with answer for any score.
func bandDecision(key, answer string) entities.DecisionDefinition {
	return entities.DecisionDefinition{
		Key:       key,
		Name:      "Band",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "in", Label: "Score", Expression: "score", Type: "number"}},
		Outputs:   []entities.DecisionOutput{{ID: "out", Label: "Band", Name: "band", Type: "string"}},
		Rules:     []entities.DecisionRule{{ID: "r1", Inputs: []string{"-"}, Outputs: []any{answer}}},
	}
}

// consult deploys a process whose one step consults key with no version, runs
// it, and returns the finished instance.
func consult(t *testing.T, h *businessRuleHarness, key string) entities.ProcessInstance {
	t.Helper()
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projectID},
		Key:     "consult-" + key,
		Name:    "Consult",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "decide", Type: entities.BusinessRuleTask, Name: "Decide", Properties: map[string]any{"decision_key": key}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "decide"},
			{ID: "f2", SourceRef: "decide", TargetRef: "end"},
		},
	}
	if _, err := serviceimpl.NewDefinitionService(h.repo).CreateDefinition(h.ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	instanceID, err := h.engine.StartProcess(h.ctx, h.projectID, def.Key, map[string]any{"score": 50})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	instance, err := h.engine.GetInstance(h.ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	return instance
}

func TestABusinessRuleTaskWithNoVersionEvaluatesTheLiveVersionWhileANewerOneIsStaged(t *testing.T) {
	h := newBusinessRuleHarness(t)
	live := bandDecision("band", "HIGH")
	live.Project = &entities.Project{ID: h.projectID}
	if _, err := h.decisions.CreateDecision(h.ctx, live); err != nil {
		t.Fatalf("create v1: %v", err)
	}
	stageVersion(t, h, "band", 2, "LOW")

	instance := consult(t, h, "band")

	if got := instance.Variables["band"]; got != "HIGH" {
		t.Errorf("the step decided %v; v1 is live and answers HIGH, and the staged v2 answers LOW", got)
	}
	entry := h.decisionEntry(t, instance.ID)
	if got := entry.Data["decision_version"]; got != float64(1) && got != 1 {
		t.Errorf("the timeline says v%v decided; want the live v1", got)
	}
}

// A decision another decision requires is evaluated the way a step with no
// version is: at its live version. Evaluating it at its newest would put a
// staged table into force through the back door, from any decision that
// depends on it.
func TestARequiredDecisionIsEvaluatedAtItsLiveVersion(t *testing.T) {
	h := newBusinessRuleHarness(t)
	risk := bandDecision("risk", "LOW")
	risk.Project = &entities.Project{ID: h.projectID}
	if _, err := h.decisions.CreateDecision(h.ctx, risk); err != nil {
		t.Fatalf("create risk v1: %v", err)
	}
	stageVersion(t, h, "risk", 2, "HIGH")

	price := entities.DecisionDefinition{
		Project:           &entities.Project{ID: h.projectID},
		Key:               "price",
		Name:              "Price",
		HitPolicy:         entities.HitPolicyFirst,
		RequiredDecisions: []string{"risk"},
		Inputs:            []entities.DecisionInput{{ID: "in", Label: "Risk", Expression: "risk", Type: "string"}},
		Outputs:           []entities.DecisionOutput{{ID: "out", Label: "Price", Name: "price", Type: "number"}},
		Rules: []entities.DecisionRule{
			{ID: "cheap", Inputs: []string{"LOW"}, Outputs: []any{100}},
			{ID: "dear", Inputs: []string{"HIGH"}, Outputs: []any{500}},
		},
	}
	if _, err := h.decisions.CreateDecision(h.ctx, price); err != nil {
		t.Fatalf("create price: %v", err)
	}

	result, err := h.decisions.Evaluate(h.ctx, h.projectID, "price", 0, map[string]any{"score": 50})
	if err != nil {
		t.Fatalf("evaluate price: %v", err)
	}
	if got := result.Values["price"]; got != float64(100) && got != 100 {
		t.Errorf("price = %v; the live risk v1 answers LOW (100), the staged v2 HIGH (500)", got)
	}
}

// A key nobody has made live has no answer for an evaluation that names no
// version, and it says so rather than evaluating the newest: guessing would put
// into force a version nobody chose. Every save records a live version and
// migration 26 recorded one for everything saved before it, so this is a table
// written around the service — and a pinned evaluation still reads it.
func TestAnUnpinnedEvaluationOfAKeyWithNoLiveVersionIsRefused(t *testing.T) {
	h := newBusinessRuleHarness(t)
	stageVersion(t, h, "band", 1, "HIGH")

	_, err := h.decisions.Evaluate(h.ctx, h.projectID, "band", 0, map[string]any{"score": 50})
	if err == nil || !strings.Contains(err.Error(), "no live version") {
		t.Fatalf("an unpinned evaluation of a key nobody made live: got %v, want a refusal naming the missing live version", err)
	}
	pinned, err := h.decisions.Evaluate(h.ctx, h.projectID, "band", 1, map[string]any{"score": 50})
	if err != nil || pinned.Values["band"] != "HIGH" {
		t.Fatalf("a pinned evaluation of v1: got %v (%v), want HIGH", pinned.Values["band"], err)
	}
}

// Saving can put the new version into force or stage it beside the live one,
// as deploying a process can. A staged version is stored, can be tried pinned,
// and changes nothing an unpinned evaluation reads until somebody makes it
// live.
func TestAStagedSaveLeavesTheLiveVersionInForce(t *testing.T) {
	w := newListWorld(t)
	id, err := w.svc.CreateDecision(w.ctx, bandTable(w.project, "HIGH"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	staged, err := w.svc.UpdateDecision(w.ctx, id, bandTable(w.project, "VERY HIGH"), false)
	if err != nil {
		t.Fatalf("stage the edit: %v", err)
	}
	if !staged.NewVersion || staged.Version != 2 || staged.Live {
		t.Errorf("a staged save reported %+v, want a new v2 that is not live", staged)
	}
	assertAnswers(t, w, 0, "HIGH", 1)
	assertAnswers(t, w, 2, "VERY HIGH", 2)

	// Saving the next edit live puts it into force over both.
	promoted, err := w.svc.UpdateDecision(w.ctx, staged.ID, bandTable(w.project, "TOP"), true)
	if err != nil {
		t.Fatalf("save the next edit live: %v", err)
	}
	if !promoted.Live || promoted.Version != 3 {
		t.Errorf("a live save reported %+v, want v3, live", promoted)
	}
	assertAnswers(t, w, 0, "TOP", 3)
}

// Staging keeps the live version in force, so it needs one to keep. A key with
// none — a table written around the service — would otherwise answer nothing
// to a step with no version even after a save, and the save is what fixes it.
func TestAStagedSaveOfAKeyWithNoLiveVersionMakesItLive(t *testing.T) {
	h := newBusinessRuleHarness(t)
	id := stageVersion(t, h, "band", 1, "HIGH")

	saved, err := h.decisions.UpdateDecision(h.ctx, id, bandDecision("band", "LOW"), false)
	if err != nil {
		t.Fatalf("stage the edit: %v", err)
	}
	if !saved.Live || saved.Version != 2 {
		t.Errorf("a staged save of a key with nothing live reported %+v, want v2, live", saved)
	}
	result, err := h.decisions.Evaluate(h.ctx, h.projectID, "band", 0, map[string]any{"score": 50})
	if err != nil || result.Values["band"] != "LOW" {
		t.Fatalf("unpinned after the save: got %v (%v), want LOW from v2", result.Values["band"], err)
	}
}

// assertAnswers evaluates credit-band at version (0 for unpinned) and checks
// the answer and the version that gave it.
func assertAnswers(t *testing.T, w listWorld, version int, band string, from int) {
	t.Helper()
	result, err := w.svc.Evaluate(w.ctx, w.project, "credit-band", version, map[string]any{"score": 20})
	if err != nil {
		t.Fatalf("evaluate at %d: %v", version, err)
	}
	if result.Values["band"] != band || result.DecisionVersion != from {
		t.Errorf("evaluating at %d answered %v from v%d, want %s from v%d",
			version, result.Values["band"], result.DecisionVersion, band, from)
	}
}
