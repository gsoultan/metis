package impl

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// formsKept is the forms one project keeps, and how often they were read.
type formsKept struct {
	repocontracts.FormRepository
	project uuid.UUID
	byKey   map[string]map[string]any
	fails   error
	reads   int
}

func (f *formsKept) GetByKey(_ context.Context, projectID uuid.UUID, key string) (models.FormModel, error) {
	f.reads++
	if f.fails != nil {
		return models.FormModel{}, f.fails
	}
	schema, ok := f.byKey[key]
	if !ok || projectID != f.project {
		return models.FormModel{}, fmt.Errorf("form %q: %w", key, apierr.ErrNotFound)
	}
	return models.FormModel{ProjectID: models.UUID(projectID), Key: key, Schema: schema}, nil
}

// approvalForms is a project that keeps two forms: "approval", with the fields
// approved and reason, and "unnamed", whose fields have no id and so name no
// variable.
func approvalForms(project uuid.UUID) *formsKept {
	return &formsKept{
		project: project,
		byKey: map[string]map[string]any{
			"approval": {"fields": []any{
				map[string]any{"id": "approved"},
				map[string]any{"id": "reason"},
			}},
			"unnamed": {"fields": []any{
				map[string]any{"label": "Approved"},
				map[string]any{"id": ""},
				map[string]any{"id": 7},
			}},
		},
	}
}

// formsOnly is a store that keeps forms and nothing else, and counts how often
// it was asked for them.
type formsOnly struct {
	repositories.Repository
	forms *formsKept
	asked int
}

func (r *formsOnly) Form() repocontracts.FormRepository {
	r.asked++
	return r.forms
}

// A completion reads the stored form only when the inline one leaves one of
// its variables undeclared: most tasks carry their form inline, and a
// completion that form covers costs no read, and does not so much as ask the
// store for its forms.
func TestACompletionReadsTheStoredFormOnlyWhenTheInlineOneLeavesSomethingUndeclared(t *testing.T) {
	project := uuid.Must(uuid.NewV7())
	inline := `[{"id":"amount"}]`
	for _, tc := range []struct {
		name       string
		task       models.TaskModel
		vars       map[string]any
		undeclared []string
		reads      int
	}{
		{"the inline form covers the completion", models.TaskModel{FormDefinition: inline, FormKey: "approval"}, map[string]any{"amount": 10}, nil, 0},
		{"the stored form declares what the inline one leaves out", models.TaskModel{FormDefinition: inline, FormKey: "approval"}, map[string]any{"amount": 10, "approved": true}, nil, 1},
		{"a stored form alone", models.TaskModel{FormKey: "approval"}, map[string]any{"reason": "late"}, nil, 1},
		{"a variable neither form declares", models.TaskModel{FormDefinition: inline, FormKey: "approval"}, map[string]any{"reason": "late", "approver": "someone else", "amount_due": 1}, []string{"amount_due", "approver"}, 1},
		{"a key naming no stored form declares nothing", models.TaskModel{FormDefinition: inline, FormKey: "kept-elsewhere"}, map[string]any{"approved": true}, []string{"approved"}, 1},
		{"no stored form is named", models.TaskModel{FormDefinition: inline}, map[string]any{"approved": true}, []string{"approved"}, 0},
		{"no form at all", models.TaskModel{}, map[string]any{"approved": true}, []string{"approved"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &formsOnly{forms: approvalForms(project)}
			s := &taskService{repo: store}
			tc.task.ProjectID = models.UUID(project)

			undeclared, err := s.undeclaredVariables(context.Background(), tc.task, tc.vars)
			if err != nil {
				t.Fatalf("undeclaredVariables: %v", err)
			}
			if !slices.Equal(undeclared, tc.undeclared) {
				t.Fatalf("undeclared = %v, want %v", undeclared, tc.undeclared)
			}
			if store.asked != tc.reads || store.forms.reads != tc.reads {
				t.Fatalf("the store was asked for its forms %d times and a form was read %d times, want %d of each",
					store.asked, store.forms.reads, tc.reads)
			}
		})
	}
}

// The completion the inline form covers needs no store at all, which is what
// BenchmarkAdmittingVariablesAnInlineFormDeclares runs it with.
func TestACompletionTheInlineFormCoversNeedsNoStore(t *testing.T) {
	task := models.TaskModel{FormDefinition: `[{"id":"amount"}]`, FormKey: "approval"}

	undeclared, err := (&taskService{}).undeclaredVariables(context.Background(), task, map[string]any{"amount": 10})
	if err != nil || len(undeclared) != 0 {
		t.Fatalf("undeclared = %v, err = %v, want neither", undeclared, err)
	}
}

// The path the design exists for: a task that names a stored form and carries
// an inline one that covers the completion. It stops at the inline form, so
// it costs no more than reading that form and listing what is outside it,
// written out by hand here — no read of the store, no second set, no list of
// names. Compared with that and not with a number, so it holds whatever a
// decoder allocates. The comparison is made only without the race detector:
// under it the two counts move by a few allocations from run to run, in
// either direction, so there the test only runs both paths.
func TestAKeyedCompletionTheInlineFormCoversCostsNoMoreThanReadingThatForm(t *testing.T) {
	task := models.TaskModel{FormKey: "approval", FormDefinition: `[` +
		`{"id":"approved","label":"Approved","type":"boolean","required":true},` +
		`{"id":"reason","label":"Why not","type":"textarea","logic":{"hiddenIf":"data.approved == true"}},` +
		`{"id":"amount_checked","label":"Amount checked","type":"boolean"},` +
		`{"id":"notes","label":"Notes","type":"textarea"}]`}
	vars := map[string]any{"approved": true, "reason": "", "amount_checked": true, "notes": ""}
	// No store: a completion that asked for one would panic, not allocate.
	s := &taskService{}
	ctx := context.Background()

	covered := testing.AllocsPerRun(50, func() {
		if undeclared, err := s.undeclaredVariables(ctx, task, vars); err != nil || len(undeclared) != 0 {
			t.Fatalf("undeclared = %v, err = %v, want neither", undeclared, err)
		}
	})
	byHand := testing.AllocsPerRun(50, func() {
		declared := make(map[string]struct{})
		addFieldIDs(declared, inlineForm(task.FormDefinition))
		if undeclared := namesOutside(vars, declared); len(undeclared) != 0 {
			t.Fatalf("undeclared = %v, want none", undeclared)
		}
	})
	if raceDetector {
		t.Logf("built with the race detector: %.0f and %.0f allocations, not compared", covered, byHand)
		return
	}
	if covered > byHand {
		t.Errorf("a covered completion of a task that names a stored form allocated %.0f times; reading its inline form by hand allocates %.0f",
			covered, byHand)
	}
}

// A stored form that cannot be read fails the completion: "the form declares
// nothing" would refuse what the form allows, and the caller could not tell.
func TestACompletionWhoseStoredFormCannotBeReadIsAnError(t *testing.T) {
	down := errors.New("connection refused")
	task := unreadableFormTask()
	s := &taskService{repo: &formsOnly{forms: &formsKept{fails: down}}}

	undeclared, err := s.undeclaredVariables(context.Background(), task, map[string]any{"approved": true})
	if !errors.Is(err, down) {
		t.Fatalf("err = %v, want it to wrap %v", err, down)
	}
	if want := unreadableFormPrefix(task); !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("err = %q, want it to start %q", err, want)
	}
	if undeclared != nil {
		t.Fatalf("undeclared = %v beside an error, want nothing", undeclared)
	}
}

// unreadableFormTask is a task that carries one form and names another.
func unreadableFormTask() models.TaskModel {
	return models.TaskModel{
		Base:           models.Base{ID: models.UUID(uuid.Must(uuid.NewV7()))},
		FormDefinition: `[{"id":"amount"}]`,
		FormKey:        "approval",
	}
}

// unreadableFormPrefix is how the error for a stored form that could not be
// read begins: the form's key and the task that names it, then the cause.
func unreadableFormPrefix(task models.TaskModel) string {
	return fmt.Sprintf("could not read the form %q that task %s names: ", task.FormKey, uuid.UUID(task.ID))
}

// --- declaredFieldIDs ---

func sortedIDs(declared map[string]struct{}) []string {
	ids := make([]string, 0, len(declared))
	for id := range declared {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// A completion and whoever asks beforehand what a step may set agree, name by
// name: a completion may set a variable exactly when declaredFieldIDs holds its
// name. A form source that only one of the two read would let a waive set what
// a completion is refused, or refuse what a completion sets.
func TestACompletionMaySetExactlyWhatTheTasksFormIsSaidToDeclare(t *testing.T) {
	project, elsewhere := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	inline := `[{"id":"amount"},{"id":"reason"}]`
	candidates := []string{"amount", "approved", "reason", "approver", "fields", "id", "label", ""}
	for _, tc := range []struct {
		name    string
		task    models.TaskModel
		project uuid.UUID
	}{
		{"an inline form only", models.TaskModel{FormDefinition: inline}, project},
		{"a stored form only", models.TaskModel{FormKey: "approval"}, project},
		{"both", models.TaskModel{FormDefinition: inline, FormKey: "approval"}, project},
		{"a key that names no stored form", models.TaskModel{FormDefinition: inline, FormKey: "kept-elsewhere"}, project},
		{"no form at all", models.TaskModel{}, project},
		{"a stored form in another project", models.TaskModel{FormKey: "approval"}, elsewhere},
		{"an inline form and a stored form in another project", models.TaskModel{FormDefinition: inline, FormKey: "approval"}, elsewhere},
		{"a stored form whose fields have no id", models.TaskModel{FormKey: "unnamed"}, project},
		{"an inline form kept as an object", models.TaskModel{FormDefinition: `{"fields":[{"id":"amount"},{"id":"reason"}]}`}, project},
		{"an inline object and a stored form", models.TaskModel{FormDefinition: `{"fields":[{"id":"amount"}]}`, FormKey: "approval"}, project},
		{"an inline object with no list of fields", models.TaskModel{FormDefinition: `{"amount":{"type":"number"},"id":"id","fields":"fields"}`}, project},
		{"inline text that is not JSON", models.TaskModel{FormDefinition: `[{"id":"amount"`}, project},
		{"inline text that is not JSON, and a stored form", models.TaskModel{FormDefinition: `[{"id":"amount"`, FormKey: "approval"}, project},
		{"an inline list that holds what are not fields", models.TaskModel{FormDefinition: `["amount",7,null,{"id":7},{"id":""},{"label":"Reason"}]`, FormKey: "approval"}, project},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.task.ProjectID = models.UUID(tc.project)
			declared, err := declaredFieldIDs(context.Background(), approvalForms(project), tc.task)
			if err != nil {
				t.Fatalf("declaredFieldIDs: %v", err)
			}
			// Every candidate, and every name the form is said to declare:
			// the second half is what catches a source only declaredFieldIDs
			// reads, whatever it happens to name.
			names := slices.Concat(candidates, sortedIDs(declared))
			slices.Sort(names)
			for _, name := range slices.Compact(names) {
				s := &taskService{repo: &formsOnly{forms: approvalForms(project)}}
				undeclared, err := s.undeclaredVariables(context.Background(), tc.task, map[string]any{name: true})
				if err != nil {
					t.Fatalf("undeclaredVariables(%q): %v", name, err)
				}
				_, said := declared[name]
				if maySet := len(undeclared) == 0; maySet != said {
					t.Errorf("a completion may set %q: %t, but the form is said to declare it: %t", name, maySet, said)
				}
			}
		})
	}
}

// What a task's form declares is the fields of the form it carries together
// with the fields of the stored form it names. The stored form is read whenever
// the task names one: a caller asking what a step may set has no values in hand
// that could make the read unnecessary.
func TestATaskDeclaresTheFieldsOfItsInlineFormAndOfTheStoredFormItNames(t *testing.T) {
	project := uuid.Must(uuid.NewV7())
	for _, tc := range []struct {
		name  string
		task  models.TaskModel
		want  []string
		reads int
	}{
		{"no form declares nothing", models.TaskModel{}, []string{}, 0},
		{"an inline form alone", models.TaskModel{FormDefinition: `[{"id":"amount"}]`}, []string{"amount"}, 0},
		{"a stored form alone", models.TaskModel{FormKey: "approval"}, []string{"approved", "reason"}, 1},
		{"both, joined", models.TaskModel{FormDefinition: `[{"id":"amount"},{"id":"reason"}]`, FormKey: "approval"}, []string{"amount", "approved", "reason"}, 1},
		{"a key naming no stored form adds nothing", models.TaskModel{FormDefinition: `[{"id":"amount"}]`, FormKey: "kept-elsewhere"}, []string{"amount"}, 1},
		{"a stored form whose fields have no id declares nothing", models.TaskModel{FormKey: "unnamed"}, []string{}, 1},
		{"an inline form kept as an object", models.TaskModel{FormDefinition: `{"fields":[{"id":"amount"}]}`}, []string{"amount"}, 0},
		{"an inline object with no list of fields declares nothing", models.TaskModel{FormDefinition: `{"amount":{"type":"number"}}`}, []string{}, 0},
		{"inline text that is not JSON declares nothing", models.TaskModel{FormDefinition: `[{"id":"amount"`}, []string{}, 0},
		{"inline text that is not JSON leaves what the stored form declares", models.TaskModel{FormDefinition: `[{"id":"amount"`, FormKey: "approval"}, []string{"approved", "reason"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forms := approvalForms(project)
			tc.task.ProjectID = models.UUID(project)

			declared, err := declaredFieldIDs(context.Background(), forms, tc.task)
			if err != nil {
				t.Fatalf("declaredFieldIDs: %v", err)
			}
			if declared == nil {
				t.Fatal("declaredFieldIDs answered a nil set; a caller reads from it and may add to it")
			}
			if got := sortedIDs(declared); !slices.Equal(got, tc.want) {
				t.Fatalf("the task declares %v, want %v", got, tc.want)
			}
			if forms.reads != tc.reads {
				t.Fatalf("the stored form was read %d times, want %d", forms.reads, tc.reads)
			}
		})
	}
}

// A stored form belongs to a project: a task declares nothing through a key
// that names a form of another project.
func TestATaskDeclaresNothingThroughAnotherProjectsForm(t *testing.T) {
	forms := approvalForms(uuid.Must(uuid.NewV7()))
	task := models.TaskModel{ProjectID: models.UUID(uuid.Must(uuid.NewV7())), FormKey: "approval"}

	declared, err := declaredFieldIDs(context.Background(), forms, task)
	if err != nil {
		t.Fatalf("declaredFieldIDs: %v", err)
	}
	if len(declared) != 0 {
		t.Fatalf("the task declares %v through a form of another project, want nothing", sortedIDs(declared))
	}
}

// A stored form that cannot be read is an error, never "declares nothing" and
// never "declares only the inline fields": either would decide what a step may
// set from half an answer.
func TestAStoredFormThatCannotBeReadIsAnError(t *testing.T) {
	down := errors.New("connection refused")
	task := unreadableFormTask()

	declared, err := declaredFieldIDs(context.Background(), &formsKept{fails: down}, task)
	if !errors.Is(err, down) {
		t.Fatalf("err = %v, want it to wrap %v", err, down)
	}
	if want := unreadableFormPrefix(task); !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("err = %q, want it to start %q", err, want)
	}
	if declared != nil {
		t.Fatalf("declared = %v beside an error, want nothing", sortedIDs(declared))
	}
}
