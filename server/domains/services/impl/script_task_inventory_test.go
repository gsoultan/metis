package impl

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// TestCollectScriptTasks pins what the inventory reports.
//
// The point of this list is to size a risk that cannot currently be bounded —
// the script sandbox has no heap limit because goja offers none — so the
// counting rule matters. It keys on the node *type*, because that is what
// decides whether the engine hands a body to goja: a script task with an empty
// body still counts (somebody will fill it in), and a non-script node carrying
// a Script field does not (the engine would never run it).
//
// An inventory that undercounts is worse than none: it turns "we looked" into
// evidence for a decision the data does not support.
func TestCollectScriptTasks(t *testing.T) {
	def := &entities.ProcessDefinition{
		ID:      uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		Key:     "invoice-intake",
		Name:    "Invoice intake",
		Version: 7,
		Nodes: []*entities.Node{
			{
				ID: "normalise", Name: "Normalise the totals", Type: entities.ScriptTask,
				Script: "setVar('total', amount * 1.2)", ScriptFormat: "javascript",
			},
			// Declared but not yet written. Still a script task.
			{ID: "placeholder", Name: "To be written", Type: entities.ScriptTask},
			// A Script field on a node the engine never sends to goja.
			{ID: "call-out", Name: "Call the partner", Type: entities.ServiceTask, Script: "not run"},
			{ID: "approve", Name: "Approve", Type: entities.UserTask},
			{
				ID: "sub", Name: "Subprocess", Type: entities.SubProcess,
				Nodes: []*entities.Node{
					{ID: "inner", Name: "Inner script", Type: entities.ScriptTask, Script: "x = 1"},
				},
			},
		},
	}

	usages := collectScriptTasks(def)

	ids := make([]string, 0, len(usages))
	for _, u := range usages {
		ids = append(ids, u.NodeID)
	}

	want := map[string]bool{"normalise": true, "placeholder": true, "inner": true}
	if len(usages) != len(want) {
		t.Fatalf("reported %d script tasks %v, want %d (%v)", len(usages), ids, len(want), want)
	}
	for _, id := range ids {
		if !want[id] {
			t.Errorf("reported %q, which is not a script task", id)
		}
	}
}

func TestCollectScriptTasksCarriesWhatDecidesTheFix(t *testing.T) {
	body := "setVar('total', amount * 1.2)"
	def := &entities.ProcessDefinition{
		ID:      uuid.MustParse("00000000-0000-0000-0000-000000000003"),
		Key:     "invoice-intake",
		Name:    "Invoice intake",
		Version: 7,
		Nodes: []*entities.Node{
			{ID: "normalise", Name: "Normalise", Type: entities.ScriptTask, Script: body, ScriptFormat: "javascript"},
		},
	}

	usages := collectScriptTasks(def)
	if len(usages) != 1 {
		t.Fatalf("got %d usages, want 1", len(usages))
	}
	u := usages[0]

	// Choosing between a FEEL replacement and out-of-process execution means
	// reading the scripts, so the body travels — and its length beside it, so a
	// caller can rank by size without measuring every string itself.
	if u.Script != body {
		t.Errorf("Script = %q, want %q", u.Script, body)
	}
	if u.ScriptLength != len(body) {
		t.Errorf("ScriptLength = %d, want %d", u.ScriptLength, len(body))
	}
	if u.ScriptFormat != "javascript" {
		t.Errorf("ScriptFormat = %q, want %q", u.ScriptFormat, "javascript")
	}
	// Without the definition it names, a line of this inventory is unactionable.
	if u.DefinitionKey != "invoice-intake" || u.Version != 7 || u.NodeName != "Normalise" {
		t.Errorf("usage does not locate itself: %+v", u)
	}
}

// An empty format still means JavaScript — the handler sends every script task
// to goja whatever the model declared — so it must not be filtered out.
func TestCollectScriptTasksCountsAnUndeclaredFormat(t *testing.T) {
	def := &entities.ProcessDefinition{
		ID:    uuid.MustParse("00000000-0000-0000-0000-000000000004"),
		Key:   "k",
		Nodes: []*entities.Node{{ID: "s", Type: entities.ScriptTask, Script: "x = 1"}},
	}

	if got := len(collectScriptTasks(def)); got != 1 {
		t.Errorf("reported %d script tasks for an undeclared format, want 1", got)
	}
}
