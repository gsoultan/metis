package impl

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/models"
)

const (
	// formFieldRequiredKey marks a field the person must fill in.
	formFieldRequiredKey = "required"
	// formFieldLogicKey holds a field's conditions, and formFieldHiddenIfKey
	// the one that hides it.
	formFieldLogicKey    = "logic"
	formFieldHiddenIfKey = "hiddenIf"
)

// refuseMissingRequired refuses a completion that leaves out a field the
// task's form requires.
//
// A completion that carried no variables finished an approval without anybody
// saying yes or no: the step was done, the instance moved on, and the
// decision it was waiting for was never made. The inbox checks a required
// field before it submits; this is the same rule for every other caller.
//
// The rule is the inbox's (ui/src/hooks/useTaskForm.ts): a value is missing
// when it is absent, null or the empty string, and false and 0 are answers. A
// field with a condition that can hide it is not asked for, because whether it
// is shown depends on values only the form evaluates.
//
// Called before anything is written, so a refused completion leaves the task
// open and the instance as it was.
func (s *taskService) refuseMissingRequired(ctx context.Context, task models.TaskModel, vars map[string]any) error {
	fields := fieldsOf(inlineForm(task.FormDefinition))
	if task.FormKey != "" {
		form, err := s.repo.Form().GetByKey(ctx, uuid.UUID(task.ProjectID), task.FormKey)
		switch {
		case errors.Is(err, apierr.ErrNotFound):
			// A form kept outside Metis: its fields cannot be read, so none
			// of them can be asked for.
		case err != nil:
			return fmt.Errorf("could not read the form %q that task %s names: %w", task.FormKey, uuid.UUID(task.ID), err)
		default:
			fields = append(fields, fieldsOf(form.Schema)...)
		}
	}

	missing := missingRequiredFields(fields, vars)
	if len(missing) == 0 {
		return nil
	}
	noun := "field"
	if len(missing) > 1 {
		noun = "fields"
	}
	return apierr.Invalidf("this task's form needs the %s %s before it can be completed", noun, listedNames(missing))
}

// missingRequiredFields returns the ids of the required, always-shown fields
// that vars leaves without a value, sorted and without repeats.
func missingRequiredFields(fields []any, vars map[string]any) []string {
	var missing []string
	for _, field := range fields {
		described, ok := field.(map[string]any)
		if !ok {
			continue
		}
		id, _ := described[formFieldIDKey].(string)
		if id == "" || described[formFieldRequiredKey] != true || mayBeHidden(described) {
			continue
		}
		if value, ok := vars[id]; ok && value != nil && value != "" {
			continue
		}
		if !slices.Contains(missing, id) {
			missing = append(missing, id)
		}
	}
	slices.Sort(missing)
	return missing
}

// mayBeHidden reports whether a field carries a condition that can hide it.
func mayBeHidden(field map[string]any) bool {
	logic, ok := field[formFieldLogicKey].(map[string]any)
	if !ok {
		return false
	}
	condition, _ := logic[formFieldHiddenIfKey].(string)
	return strings.TrimSpace(condition) != ""
}
