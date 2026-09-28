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
	"github.com/gsoultan/metis/server/repositories/models"
)

const (
	// formFieldsKey is where a form kept as an object holds its list of
	// fields: a stored form always, an inline one when it is not a bare list.
	formFieldsKey = "fields"
	// formFieldIDKey names a field, and so the variable the field sets.
	formFieldIDKey = "id"

	// maxNamesShown bounds how many refused names are listed, and maxNameShown
	// how much of each. The names are the caller's: a completion carrying ten
	// thousand of them, or one a megabyte long, would otherwise be echoed back
	// whole.
	maxNamesShown = 10
	maxNameShown  = 64
)

// admitVariables refuses a completion that would set a variable the task's
// form does not declare.
//
// A completion wrote whatever variables it carried into the instance, so
// whoever completed an approval could also rewrite the amount being approved,
// or name somebody else as the approver: business data beyond their step. A
// task declares what it sets through its form — the id of each of its fields,
// hidden ones included, because the inbox submits every field. A task with no
// form declares nothing, so it sets nothing: absent constraint means deny.
//
// Called before anything is written, so a refused completion leaves the task
// open and the instance as it was.
func (s *taskService) admitVariables(ctx context.Context, task models.TaskModel, vars map[string]any) error {
	if len(vars) == 0 {
		return nil
	}
	undeclared, err := s.undeclaredVariables(ctx, task, vars)
	if err != nil || len(undeclared) == 0 {
		return err
	}
	return refuseUndeclared(task, undeclared)
}

// undeclaredVariables returns the names in vars that the task's form has no
// field for, sorted.
//
// The stored form is read only when the inline one leaves something
// undeclared: most tasks carry their form inline, and a completion that form
// covers costs no read.
func (s *taskService) undeclaredVariables(ctx context.Context, task models.TaskModel, vars map[string]any) ([]string, error) {
	declared := make(map[string]struct{})
	addFieldIDs(declared, inlineForm(task.FormDefinition))
	undeclared := namesOutside(vars, declared)
	if len(undeclared) == 0 || task.FormKey == "" {
		return undeclared, nil
	}
	if err := s.addStoredFormFieldIDs(ctx, declared, task); err != nil {
		return nil, err
	}
	return namesOutside(vars, declared), nil
}

// addStoredFormFieldIDs adds the fields of the stored form a task's form key
// names.
//
// A key naming no form stored in the task's project adds nothing. It may name
// a form kept outside Metis — an imported process's embedded form — whose
// fields Metis cannot read, and cannot therefore say are declared.
func (s *taskService) addStoredFormFieldIDs(ctx context.Context, declared map[string]struct{}, task models.TaskModel) error {
	form, err := s.repo.Form().GetByKey(ctx, uuid.UUID(task.ProjectID), task.FormKey)
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

// refuseUndeclared says which variables the task may not set, and why.
func refuseUndeclared(task models.TaskModel, undeclared []string) error {
	names := listedNames(undeclared)
	if strings.TrimSpace(task.FormDefinition) == "" && task.FormKey == "" {
		return apierr.Invalidf("this task has no form to declare %s; a task can set only the variables its form declares", names)
	}
	noun := "field"
	if len(undeclared) > 1 {
		noun = "fields"
	}
	return apierr.Invalidf("this task's form has no %s named %s; a task can set only the variables its form declares", noun, names)
}

// shownNames is names as a person is shown them: at most maxNamesShown, each
// cut to maxNameShown characters, and how many were left out.
func shownNames(names []string) (shown []string, more int) {
	count := min(len(names), maxNamesShown)
	shown = make([]string, count)
	for i, name := range names[:count] {
		shown[i] = shortened(name)
	}
	return shown, len(names) - count
}

// listedNames words shownNames for a sentence.
func listedNames(names []string) string {
	shown, more := shownNames(names)
	listed := strings.Join(shown, ", ")
	if more > 0 {
		listed = fmt.Sprintf("%s and %d more", listed, more)
	}
	return listed
}

// shortened cuts a name to maxNameShown characters.
func shortened(name string) string {
	count := 0
	for offset := range name {
		if count == maxNameShown {
			return name[:offset] + "…"
		}
		count++
	}
	return name
}
