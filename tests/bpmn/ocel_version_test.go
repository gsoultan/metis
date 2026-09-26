package bpmn_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A case in the OCEL export names the process version it ran.
//
// The export has always declared definition_key and definition_version on a
// case and related each case to a definition object, and never filled any of
// them in: the instance it reads carries only its definition's id. Every case
// said key "" and version 0, whatever it ran, so a case of v2 and one of v3
// were the same to a mining tool — and two versions of a process are two
// control flows, which a miner that merges them discovers as neither.
func TestEachExportedCaseNamesItsOwnProcessVersion(t *testing.T) {
	e := newAuditedEngine(t, "ocel-version-test")
	deploy := func() {
		t.Helper()
		if _, err := e.svc.CreateDefinition(e.ctx, passThroughAfterDraft(e.projectID)); err != nil {
			t.Fatalf("deploy: %v", err)
		}
	}
	start := func() uuid.UUID {
		t.Helper()
		id, err := e.svc.StartProcess(e.ctx, e.projectID, "claim", nil)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		return id
	}
	deploy() // v1
	deploy() // v2, live
	onV2 := start()
	deploy() // v3, live
	onV3 := start()

	log, err := e.svc.ExportOCEL(e.ctx, e.projectID, entities.OCELOptions{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	definitions := map[string]bool{}
	for _, object := range log.Objects {
		if object.Type == entities.OCELObjectDefinition {
			definitions[object.ID] = true
		}
	}
	for instanceID, version := range map[uuid.UUID]string{onV2: "2", onV3: "3"} {
		object, ok := caseObject(log, instanceID)
		if !ok {
			t.Fatalf("the export has no case for the instance of v%s", version)
		}
		attributes := map[string]string{}
		for _, attr := range object.Attributes {
			attributes[attr.Name] = attr.Value
		}
		if got := attributes["definition_version"]; got != version {
			t.Errorf("the case of v%s says definition_version %q", version, got)
		}
		if got := attributes["definition_key"]; got != "claim" {
			t.Errorf("the case of v%s says definition_key %q, want claim", version, got)
		}
		wantDefinition := "claim:" + version
		if len(object.Relationships) != 1 || object.Relationships[0].ObjectID != wantDefinition ||
			object.Relationships[0].Qualifier != entities.OCELQualifierDefines {
			t.Errorf("the case of v%s relates to %v, want %s as its %s",
				version, object.Relationships, wantDefinition, entities.OCELQualifierDefines)
		}
		if !definitions[wantDefinition] {
			t.Errorf("the export has no %s definition object for the case to relate to", wantDefinition)
		}
	}
}

// caseObject finds one instance's object in an export.
func caseObject(log entities.OCELLog, instanceID uuid.UUID) (entities.OCELObject, bool) {
	for _, object := range log.Objects {
		if object.Type == entities.OCELObjectProcessInstance && object.ID == instanceID.String() {
			return object, true
		}
	}
	return entities.OCELObject{}, false
}
