package impl

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

func auditEntry(instance uuid.UUID, kind, nodeID, nodeName string, at time.Time, data map[string]any) entities.AuditEntry {
	e := entities.AuditEntry{
		ID:        uuid.New(),
		Instance:  &entities.ProcessInstance{ID: instance},
		Type:      kind,
		Message:   kind,
		Timestamp: at,
		Data:      data,
	}
	if nodeID != "" {
		e.Node = &entities.Node{ID: nodeID, Name: nodeName}
	}
	return e
}

// TestOCELLogIsMineable is the point of the export: a discovered model has to
// have the process's own activities in it.
//
// The obvious mapping — event type from the audit entry's Type — produces a log
// whose every event is "node_reached", and a miner handed that discovers a
// model with four boxes regardless of how large the process is. The activity is
// the node's name; the audit type is the lifecycle transition and travels as an
// attribute.
func TestOCELLogIsMineable(t *testing.T) {
	instance := uuid.New()
	at := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)

	log := buildOCELLog([]entities.AuditEntry{
		auditEntry(instance, "process_started", "", "", at, nil),
		auditEntry(instance, "node_reached", "review", "Review claim", at.Add(time.Minute), nil),
		auditEntry(instance, "task_completed", "review", "Review claim", at.Add(2*time.Minute), nil),
		auditEntry(instance, "node_reached", "pay", "Pay out", at.Add(3*time.Minute), nil),
	}, nil, entities.OCELOptions{})

	if len(log.Events) != 4 {
		t.Fatalf("events: got %d, want 4", len(log.Events))
	}

	types := map[string]int{}
	for _, e := range log.Events {
		types[e.Type]++
	}
	if types["Review claim"] != 2 {
		t.Errorf("the review activity appears %d times, want 2 — the node name is the activity", types["Review claim"])
	}
	if types["Pay out"] != 1 {
		t.Errorf("the pay activity appears %d times, want 1", types["Pay out"])
	}
	if types["node_reached"] != 0 {
		t.Errorf("%d events are typed by their audit kind; a model mined from that has no business activities in it", types["node_reached"])
	}
	// A process-level entry has no node, so there its type is the activity.
	if types["process_started"] != 1 {
		t.Errorf("process_started appears %d times, want 1", types["process_started"])
	}

	// Every event has to name its case or it cannot be grouped into one.
	for _, e := range log.Events {
		if len(e.Relationships) == 0 || e.Relationships[0].ObjectID != instance.String() {
			t.Fatalf("event %s is not related to its instance", e.Type)
		}
		if e.Relationships[0].Qualifier != entities.OCELQualifierInstance {
			t.Errorf("relationship qualifier is %q, want %q", e.Relationships[0].Qualifier, entities.OCELQualifierInstance)
		}
	}

	// The declared types are what a reader validates against.
	declared := map[string]bool{}
	for _, et := range log.EventTypes {
		declared[et.Name] = true
	}
	for _, e := range log.Events {
		if !declared[e.Type] {
			t.Errorf("event type %q is used but never declared", e.Type)
		}
	}
}

// TestOCELExportDoesNotLeakVariablesByDefault is the one that matters.
//
// An audit entry's data map is the instance's process variables — an amount, an
// applicant's name, an approval decision. Mining a control-flow model needs the
// activity, the case and the time and none of those are in there, so including
// them by default would be handing out every business fact in a project for no
// benefit to the thing the endpoint is for.
func TestOCELExportDoesNotLeakVariablesByDefault(t *testing.T) {
	instance := uuid.New()
	secret := map[string]any{"applicant": "Ada Lovelace", "amount": 25000}
	entries := []entities.AuditEntry{
		auditEntry(instance, "node_reached", "review", "Review claim", time.Now(), secret),
	}

	quiet, err := json.Marshal(buildOCELLog(entries, nil, entities.OCELOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"Ada Lovelace", "25000", "applicant"} {
		if strings.Contains(string(quiet), leaked) {
			t.Errorf("the default export contains %q from the process variables\n%s", leaked, quiet)
		}
	}

	loud, err := json.Marshal(buildOCELLog(entries, nil, entities.OCELOptions{IncludeVariables: true}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(loud), "Ada Lovelace") {
		t.Error("opting in did not include the variables, so the flag does nothing")
	}
}

// TestOCELSeparatesDefinitionVersions guards against merging two control flows
// into one. Two versions of a process are two different models; a log that
// relates their cases to the same definition object discovers a model that is
// neither of them.
func TestOCELSeparatesDefinitionVersions(t *testing.T) {
	oldInstance, newInstance := uuid.New(), uuid.New()
	known := map[uuid.UUID]entities.ProcessInstance{
		oldInstance: {
			ID:         oldInstance,
			Status:     entities.ProcessCompleted,
			Definition: &entities.ProcessDefinition{Key: "claim", Version: 1},
		},
		newInstance: {
			ID:         newInstance,
			Status:     entities.ProcessActive,
			Definition: &entities.ProcessDefinition{Key: "claim", Version: 2},
		},
	}

	log := buildOCELLog([]entities.AuditEntry{
		auditEntry(oldInstance, "node_reached", "a", "A", time.Now(), nil),
		auditEntry(newInstance, "node_reached", "a", "A", time.Now(), nil),
	}, known, entities.OCELOptions{})

	definitions := map[string]bool{}
	instances := 0
	for _, o := range log.Objects {
		switch o.Type {
		case entities.OCELObjectDefinition:
			definitions[o.ID] = true
		case entities.OCELObjectProcessInstance:
			instances++
			if len(o.Relationships) != 1 {
				t.Errorf("instance %s is not related to a definition", o.ID)
			}
		}
	}
	if instances != 2 {
		t.Errorf("instance objects: got %d, want 2", instances)
	}
	if !definitions["claim:1"] || !definitions["claim:2"] {
		t.Errorf("definition objects are %v, want claim:1 and claim:2 as separate objects", definitions)
	}
}

// TestOCELDropsEventsWithNoCase covers entries that describe the project rather
// than an instance. OCEL has no event outside every object, so emitting one
// with an invented case puts a phantom trace in every discovered model.
func TestOCELDropsEventsWithNoCase(t *testing.T) {
	orphan := entities.AuditEntry{ID: uuid.New(), Type: "project_created", Timestamp: time.Now()}
	log := buildOCELLog([]entities.AuditEntry{orphan}, nil, entities.OCELOptions{})

	if len(log.Events) != 0 {
		t.Errorf("events: got %d, want 0 — an entry with no instance is not a case", len(log.Events))
	}
	if len(log.Objects) != 0 {
		t.Errorf("objects: got %d, want 0", len(log.Objects))
	}
}

// TestOCELNamesNoVersionItCouldNotRead covers a case whose definition is known
// only by its id — deleted since, or out of the reader's scope. Version 0 is
// not a version and an empty key is not a process: saying either would file the
// case under a definition that never existed. It says nothing about them.
func TestOCELNamesNoVersionItCouldNotRead(t *testing.T) {
	instance := uuid.New()
	known := map[uuid.UUID]entities.ProcessInstance{
		instance: {
			ID:         instance,
			Status:     entities.ProcessCompleted,
			Definition: &entities.ProcessDefinition{ID: uuid.New()},
		},
	}
	log := buildOCELLog([]entities.AuditEntry{
		auditEntry(instance, "node_reached", "a", "A", time.Now(), nil),
	}, known, entities.OCELOptions{})

	for _, o := range log.Objects {
		if o.Type == entities.OCELObjectDefinition {
			t.Errorf("a definition object %q was invented for a definition nobody could read", o.ID)
		}
		for _, attr := range o.Attributes {
			if attr.Name == "definition_key" || attr.Name == "definition_version" {
				t.Errorf("the case says %s %q about a definition nobody could read", attr.Name, attr.Value)
			}
		}
	}
}
