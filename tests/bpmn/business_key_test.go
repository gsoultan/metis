package bpmn_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// An instance started for a case carries the caller's name for it, a
// sub-process its call activity starts carries the same one, and a project's
// instances are found by it: every instance of the case, and nothing else.
func TestABusinessKeyFollowsTheCaseIntoItsSubProcessesAndFindsIt(t *testing.T) {
	h := newServiceTaskHarness(t)
	createChildProcess(t, h, "supplier-check")
	if _, err := h.defSvc.CreateDefinition(h.ctx, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projectID},
		Key:     "onboarding",
		Name:    "Onboard a supplier",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "check", Type: entities.CallActivity, Name: "Check the supplier", Properties: map[string]any{
				"called_process_key": "supplier-check",
			}},
			{ID: "end", Type: entities.EndEvent, Name: "Done"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "check"},
			{ID: "f2", SourceRef: "check", TargetRef: "end"},
		},
	}); err != nil {
		t.Fatalf("create the parent process: %v", err)
	}
	start := func(key string) entities.InstanceStart {
		return entities.InstanceStart{ProjectID: h.projectID, DefinitionKey: "onboarding", BusinessKey: key}
	}
	parentID, err := h.engine.StartInstance(h.ctx, start("SUP-4411"))
	if err != nil {
		t.Fatalf("start the case: %v", err)
	}
	if _, err := h.engine.StartInstance(h.ctx, start("SUP-9000")); err != nil {
		t.Fatalf("start another case: %v", err)
	}
	if _, err := h.engine.StartProcess(h.ctx, h.projectID, "onboarding", nil); err != nil {
		t.Fatalf("start a case with no key: %v", err)
	}

	children := h.subProcessesOf(t, parentID)
	if len(children) != 1 {
		t.Fatalf("the call activity started %d sub-processes, want one", len(children))
	}
	child, err := h.engine.GetInstance(h.ctx, children[0].ID)
	if err != nil {
		t.Fatalf("read the sub-process: %v", err)
	}
	if child.BusinessKey != "SUP-4411" {
		t.Errorf("the sub-process carries business key %q, want its caller's SUP-4411", child.BusinessKey)
	}

	found, err := h.engine.ListInstancesPaged(h.ctx, h.projectID,
		repocontracts.InstanceFilter{BusinessKey: "SUP-4411"}, repocontracts.Pagination{})
	if err != nil {
		t.Fatalf("list by business key: %v", err)
	}
	ids := map[string]bool{}
	for _, instance := range found.Items {
		ids[instance.ID.String()] = true
		if instance.BusinessKey != "SUP-4411" {
			t.Errorf("listing SUP-4411 returned an instance of %q", instance.BusinessKey)
		}
	}
	if found.Total != 2 || !ids[parentID.String()] || !ids[child.ID.String()] {
		t.Errorf("listing SUP-4411 found %d instances (%v), want the case and its sub-process", found.Total, ids)
	}
}

// A business key the column cannot hold, or one that would print as
// something else, is refused before anything starts.
func TestABusinessKeyThatCannotBeKeptAsWrittenIsRefused(t *testing.T) {
	h := newServiceTaskHarness(t)
	createChildProcess(t, h, "supplier-check")
	for name, key := range map[string]string{
		"longer than the column": strings.Repeat("k", entities.MaxBusinessKeyLength+1),
		"with a line break":      "SUP-1\nSUP-2",
		"with a tab":             "SUP\t1",
	} {
		if _, err := h.engine.StartInstance(h.ctx, entities.InstanceStart{
			ProjectID: h.projectID, DefinitionKey: "supplier-check", BusinessKey: key,
		}); err == nil {
			t.Errorf("a business key %s was accepted", name)
		}
	}
	page, err := h.engine.ListInstancesPaged(h.ctx, h.projectID, repocontracts.InstanceFilter{}, repocontracts.Pagination{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 0 {
		t.Errorf("%d instances started under refused business keys", page.Total)
	}
	if _, err := h.engine.StartInstance(h.ctx, entities.InstanceStart{
		ProjectID: h.projectID, DefinitionKey: "supplier-check", BusinessKey: strings.Repeat("ü", entities.MaxBusinessKeyLength),
	}); err != nil {
		t.Errorf("a business key of %d characters, the most the column holds, was refused: %v", entities.MaxBusinessKeyLength, err)
	}
}
