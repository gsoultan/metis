package bpmn_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// instancesAhead is as many instances as one read of the generated store
// returns: storm starts every query with a limit of 1000.
const instancesAhead = 1000

// startWaiting deploys a process that stops at a task and starts one instance.
func (h engineHarness) startWaiting(t *testing.T, key string) uuid.UUID {
	t.Helper()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "end"},
		},
	})
	id, err := h.svc.StartProcess(h.Ctx(), h.projID, key, nil)
	if err != nil {
		t.Fatalf("start %s: %v", key, err)
	}
	return id
}

// seedInstances records n more instances of like's version in one statement,
// started after it; parent names the instance that called them, if any.
func (h engineHarness) seedInstances(t *testing.T, like uuid.UUID, parent *uuid.UUID, n int) {
	t.Helper()
	h.seedInstancesLike(h.Ctx(), t, like, parent, "active", n)
}

// Every sub-process an instance started is listed under it.
//
// The list read the children newest first through a query the store caps at a
// thousand rows. A step that starts one sub-process per item of a large order
// had the oldest of them missing from the instance's view.
func TestEverySubProcessOfAnInstanceIsListed(t *testing.T) {
	h := newEngineHarness(t, "Many Children Project")
	parent := h.startWaiting(t, "parent")
	h.seedInstances(t, parent, &parent, instancesAhead+1)

	children, err := h.engine.ListSubProcesses(h.Ctx(), parent)
	if err != nil {
		t.Fatalf("list sub-processes: %v", err)
	}
	if len(children) != instancesAhead+1 {
		t.Fatalf("the instance lists %d of the %d sub-processes it started", len(children), instancesAhead+1)
	}
}

// The OCEL export describes every case whose events it carries.
//
// It read the project's instances newest first through the same capped query,
// so a case started before a thousand others came out with its events and with
// no attributes at all: no status, nothing a mining tool can filter it by.
func TestTheOCELExportDescribesACaseOlderThanAThousandOthers(t *testing.T) {
	h := newEngineHarness(t, "Old Case Project")
	old := h.startWaiting(t, "claims")
	h.seedInstances(t, old, nil, instancesAhead)

	log, err := h.engine.ExportOCEL(h.Ctx(), h.projID, entities.OCELOptions{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	for _, object := range log.Objects {
		if object.ID != old.String() {
			continue
		}
		for _, attribute := range object.Attributes {
			if attribute.Name == "status" && attribute.Value == string(entities.ProcessActive) {
				return
			}
		}
		t.Fatalf("the case started before %d others is exported with attributes %v; it is active",
			instancesAhead, object.Attributes)
	}
	t.Fatal("the case is missing from the export's objects")
}
