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

// declaredFieldIDs returns what a task's form declares: the id of each field
// of the form the task carries, together with each field of the stored form
// its form key names. A key naming no stored form adds nothing.
//
// The stored form is read whenever the task names one. This is the reader for
// a caller that asks what a step may set before anybody has set anything. A
// completion has its variables in hand and reads the same two forms through
// taskService.undeclaredVariables, which is spared the stored one whenever the
// inline one already covers what the completion carries.
func declaredFieldIDs(ctx context.Context, forms repocontracts.FormRepository, task models.TaskModel) (map[string]struct{}, error) {
	declared := make(map[string]struct{})
	addFieldIDs(declared, inlineForm(task.FormDefinition))
	if task.FormKey == "" {
		return declared, nil
	}
	if err := addStoredFormFieldIDs(ctx, forms, declared, task); err != nil {
		return nil, err
	}
	return declared, nil
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
