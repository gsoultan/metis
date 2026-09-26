package decision_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/tests/testutils"
)

// A decision is a business policy. An instance that evaluated version 3 was
// decided under what version 3 said, and its timeline names version 3 — so
// version 3 has to go on saying it. Saving an edit rewrote the stored version
// in place, and after it nobody could read what the instances decided under
// it had been told.

// bandTable is a one-line credit band table answering band for any score over
// ten, in project.
func bandTable(project uuid.UUID, band string) entities.DecisionDefinition {
	return entities.DecisionDefinition{
		Project:   &entities.Project{ID: project},
		Key:       "credit-band",
		Name:      "Credit band",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "in", Label: "Score", Expression: "score", Type: "number"}},
		Outputs:   []entities.DecisionOutput{{ID: "out", Label: "Band", Name: "band", Type: "string"}},
		Rules:     []entities.DecisionRule{{ID: "r1", Inputs: []string{"> 10"}, Outputs: []any{band}}},
	}
}

func TestSavingAnEditedDecisionAddsAVersionAndKeepsThePreviousOne(t *testing.T) {
	w := newListWorld(t)
	id, err := w.svc.CreateDecision(w.ctx, bandTable(w.project, "HIGH"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	saved, err := w.svc.UpdateDecision(w.ctx, id, bandTable(w.project, "VERY HIGH"))
	if err != nil {
		t.Fatalf("save the edit: %v", err)
	}

	// Version 1, read by the id an instance recorded, still says what it said.
	first, err := w.svc.GetDecision(w.ctx, id)
	if err != nil {
		t.Fatalf("read version 1: %v", err)
	}
	if first.Version != 1 || first.Rules[0].Outputs[0] != "HIGH" {
		t.Errorf("version 1 now reads v%d answering %v; saving an edit rewrote the version instances already decided under",
			first.Version, first.Rules[0].Outputs[0])
	}

	// The edit is version 2, and the save says so.
	second, err := w.repo.Decision().GetByKeyAndVersion(w.ctx, w.project, "credit-band", 2)
	if err != nil {
		t.Fatalf("the edit was not stored as version 2: %v", err)
	}
	if second.Rules[0].Outputs[0] != "VERY HIGH" {
		t.Errorf("version 2 answers %v, want the edit's VERY HIGH", second.Rules[0].Outputs[0])
	}
	if !saved.NewVersion || saved.Version != 2 || saved.ID != uuid.UUID(second.ID) {
		t.Errorf("the save reported %+v, want the new version 2 (%v)", saved, uuid.UUID(second.ID))
	}

	// And an evaluation pinned to either version answers as that version did.
	for version, want := range map[int]string{1: "HIGH", 2: "VERY HIGH"} {
		result, err := w.svc.Evaluate(w.ctx, w.project, "credit-band", version, map[string]any{"score": 20})
		if err != nil {
			t.Fatalf("evaluate version %d: %v", version, err)
		}
		if result.Values["band"] != want || result.DecisionVersion != version {
			t.Errorf("version %d answered %v as v%d, want %s", version, result.Values["band"], result.DecisionVersion, want)
		}
	}
}

// Saving the table exactly as it was loaded is not a new policy. Minting a
// version for it would fill the history with copies, and make "which version
// changed the threshold" a question with several identical answers.
func TestSavingAnUnchangedDecisionAddsNoVersion(t *testing.T) {
	w := newListWorld(t)
	id, err := w.svc.CreateDecision(w.ctx, bandTable(w.project, "HIGH"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	loaded, err := w.svc.GetDecision(w.ctx, id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// What an editor sends back for an untouched table: the same content, with
	// the empty lists spelled out rather than left out.
	loaded.RequiredDecisions = []string{}
	loaded.Tests = []entities.DecisionTest{}

	saved, err := w.svc.UpdateDecision(w.ctx, id, loaded)
	if err != nil {
		t.Fatalf("save the untouched table: %v", err)
	}
	if saved.NewVersion || saved.ID != id || saved.Version != 1 {
		t.Errorf("an unchanged save reported %+v, want version 1 (%v) and no new version", saved, id)
	}
	if _, err := w.repo.Decision().GetByKeyAndVersion(w.ctx, w.project, "credit-band", 2); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("an unchanged save minted version 2 (lookup: %v)", err)
	}
}

// A save names the version it edits, and the new version belongs where that
// one does. A key or project in the body cannot move it: the key is how
// processes name the decision, and a save that filed the edit under another
// key would leave every process consulting the old one unchanged.
func TestASavedVersionStaysUnderTheKeyItWasSavedFrom(t *testing.T) {
	w := newListWorld(t)
	id, err := w.svc.CreateDecision(w.ctx, bandTable(w.project, "HIGH"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	edited := bandTable(w.project, "VERY HIGH")
	edited.Key = "somewhere-else"
	edited.Project = nil

	if _, err := w.svc.UpdateDecision(w.ctx, id, edited); err != nil {
		t.Fatalf("save the edit: %v", err)
	}
	if _, err := w.repo.Decision().GetByKeyAndVersion(w.ctx, w.project, "credit-band", 2); err != nil {
		t.Errorf("the edit is not version 2 of credit-band: %v", err)
	}
	if _, err := w.repo.Decision().GetByKeyAndVersion(w.ctx, w.project, "somewhere-else", 1); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("the edit was filed under the key its body named (lookup: %v)", err)
	}
}

// The version a save edits is read in the caller's organization, so another
// organization's id names a decision that is not there. This was a repository
// test while saving rewrote a row; saving is a read and an insert now, and the
// read is where the refusal has to happen — before anything is stored.
func TestSavingAnotherOrganizationsDecisionIsRefused(t *testing.T) {
	w := newListWorld(t)
	theirCtx, _, theirProject := testutils.ScopedProject(t, w.repo)
	theirs, err := w.svc.CreateDecision(theirCtx, bandTable(theirProject, "HIGH"))
	if err != nil {
		t.Fatalf("create their decision: %v", err)
	}

	if _, err := w.svc.UpdateDecision(w.ctx, theirs, bandTable(w.project, "STOLEN")); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("saving another organization's decision: got %v, want not found", err)
	}
	if _, err := w.repo.Decision().GetByKeyAndVersion(theirCtx, theirProject, "credit-band", 2); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("a version was added to their decision (lookup: %v)", err)
	}
	if _, err := w.repo.Decision().GetByKeyAndVersion(w.ctx, w.project, "credit-band", 1); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("their table was copied into the caller's project (lookup: %v)", err)
	}
}
