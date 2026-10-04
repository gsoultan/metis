package impl

import (
	"context"
	"fmt"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

const (
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
// open and the instance as it was. With EnvAllowUndeclaredTaskVariables on, the
// completion is let through as before and the step named in the log instead;
// definitionKey is how that line names it.
func (s *taskService) admitVariables(ctx context.Context, task models.TaskModel, definitionKey string, vars map[string]any) error {
	if len(vars) == 0 {
		return nil
	}
	undeclared, err := s.undeclaredVariables(ctx, task, vars)
	if err != nil || len(undeclared) == 0 {
		return err
	}
	if AllowUndeclaredTaskVariables() {
		s.reportUndeclared(task, definitionKey, undeclared)
		return nil
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
	covered := func() bool { return declaresAll(declared, vars) }
	if err := addDeclaredFieldIDs(ctx, s.storedForms, declared, task, covered); err != nil {
		return nil, err
	}
	return namesOutside(vars, declared), nil
}

// storedForms is where a task's stored form is read from.
func (s *taskService) storedForms() repocontracts.FormRepository {
	return s.repo.Form()
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
