package impl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

const (
	// formFieldsKey is where a form kept as an object holds its list of
	// fields: a stored form always, an inline one when it is not a bare list.
	formFieldsKey = "fields"
	// formFieldIDKey names a field, and so the variable the field sets.
	formFieldIDKey = "id"
)

// declaredFieldIDs returns what a task's form declares, and so what the step
// may set: the id of each field of the form the task carries, together with
// each field of the stored form its form key names.
//
// It denies by default. On success the set is never nil, and an empty set
// means the step may set nothing: a task with no form, a form with no named
// field, a key naming no form stored in the task's project. Beside an error
// the set is nil, so a caller cannot go on with the inline half alone.
//
// The stored form is read whenever the task names one, so a stored form that
// cannot be read is always an error here: it fails safe, with no answer
// rather than half of one. A completion differs in that one respect
// (taskService.undeclaredVariables): when the inline form already declares
// everything it carries, it never reads the stored form and so never meets
// that error. Both are filled by addDeclaredFieldIDs, so they cannot differ in
// which forms count.
func declaredFieldIDs(ctx context.Context, forms repocontracts.FormRepository, task models.TaskModel) (map[string]struct{}, error) {
	declared := make(map[string]struct{})
	held := func() repocontracts.FormRepository { return forms }
	if err := addDeclaredFieldIDs(ctx, held, declared, task, nil); err != nil {
		return nil, err
	}
	return declared, nil
}

// addDeclaredFieldIDs adds to declared what a task's form declares. It is the
// one list of where a task's form is kept — the form the task carries, then
// the stored form its form key names — for every caller that asks.
//
// needed, when not nil, is what the caller is about to set, by name. Once the
// inline form is in, a caller whose every name is declared has all it needs,
// and the stored form is not read. forms is called only when the stored form
// is read, so a caller stopped this way needs no repository at all. A caller
// with nothing in hand passes nil and is given the whole of what is declared.
//
// Stopping early is sound only because every source adds to declared and none
// takes a name away: what the inline form declares is declared whatever the
// stored form says, so a caller satisfied by the first half would be satisfied
// by the whole. A source that could remove a name — a stored form that
// overrode the inline one — would make this stop on a set that is not the
// final one, and the early exit would have to go.
//
// The question is asked here, of the set being filled, and not by a function
// of the caller's: a function handed in could only be right by reading this
// same set, which nothing made it do — and a set handed to a function the
// compiler cannot see is allocated for every completion, where this one stays
// on the stack. declared is the caller's, and is filled in place rather than
// returned, for the same reason.
func addDeclaredFieldIDs(
	ctx context.Context,
	forms func() repocontracts.FormRepository,
	declared map[string]struct{},
	task models.TaskModel,
	needed map[string]any,
) error {
	addFieldIDs(declared, inlineForm(task.FormDefinition))
	if task.FormKey == "" || (needed != nil && declaresAll(declared, needed)) {
		return nil
	}
	return addStoredFormFieldIDs(ctx, forms(), declared, task)
}

// addStoredFormFieldIDs adds the fields of the stored form a task's form key
// names.
//
// A key naming no form stored in the task's project adds nothing. It may name
// a form kept outside Metis — an imported process's embedded form — whose
// fields Metis cannot read, and cannot therefore say are declared.
func addStoredFormFieldIDs(ctx context.Context, forms repocontracts.FormRepository, declared map[string]struct{}, task models.TaskModel) error {
	form, err := forms.GetByKey(ctx, uuid.UUID(task.ProjectID), task.FormKey)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("could not read the form %q that task %s names: %w", task.FormKey, uuid.UUID(task.ID), err)
	}
	addFieldIDs(declared, form.Schema)
	return nil
}

// inlineForm reads the form a task carries. Text that is not JSON is no form
// the inbox can show either, and declares nothing.
func inlineForm(definition string) any {
	if strings.TrimSpace(definition) == "" {
		return nil
	}
	var form any
	if json.Unmarshal([]byte(definition), &form) != nil {
		return nil
	}
	return form
}

// addFieldIDs adds the id of each field of a form, read as the inbox reads one
// (ui/src/domain/formDefinition.ts): a list of fields, or an object holding the
// list under "fields". A field with no id names no variable.
func addFieldIDs(declared map[string]struct{}, form any) {
	for _, field := range fieldsOf(form) {
		described, ok := field.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := described[formFieldIDKey].(string); ok && id != "" {
			declared[id] = struct{}{}
		}
	}
}

// fieldsOf returns a form's list of fields, or nil for anything that is not a
// form.
func fieldsOf(form any) []any {
	switch shaped := form.(type) {
	case []any:
		return shaped
	case map[string]any:
		if fields, ok := shaped[formFieldsKey].([]any); ok {
			return fields
		}
	}
	return nil
}

// declaresAll reports whether declared holds every name in vars: whether
// namesOutside would list nothing, without building the list.
func declaresAll(declared map[string]struct{}, vars map[string]any) bool {
	for name := range vars {
		if _, ok := declared[name]; !ok {
			return false
		}
	}
	return true
}

// namesOutside returns the names in vars that declared does not hold, sorted.
func namesOutside(vars map[string]any, declared map[string]struct{}) []string {
	var outside []string
	for name := range vars {
		if _, ok := declared[name]; !ok {
			outside = append(outside, name)
		}
	}
	slices.Sort(outside)
	return outside
}
