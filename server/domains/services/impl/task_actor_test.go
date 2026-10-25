package impl

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

func TestAPersonReachesOnlyTheirOwnProjectsTasks(t *testing.T) {
	project := uuid.New()
	person := taskActor{name: "ada", project: project}
	if err := person.reaches(entities.Task{Project: &entities.Project{ID: project}}); err != nil {
		t.Fatalf("a task of ada's project: %v", err)
	}
	for name, task := range map[string]entities.Task{
		"another project": {Project: &entities.Project{ID: uuid.New()}},
		"no project":      {},
	} {
		if err := person.reaches(task); !errors.Is(err, apierr.ErrNotFound) {
			t.Errorf("%s: %v, want not found", name, err)
		}
	}
	// A User's reach is the tenant scope's, already applied to the read.
	if err := (taskActor{name: "dana"}).reaches(entities.Task{Project: &entities.Project{ID: uuid.New()}}); err != nil {
		t.Fatalf("a User reaching a task: %v", err)
	}
}

func TestAGroupIsMatchedByNameOrByID(t *testing.T) {
	id := uuid.New()
	task := entities.Task{CandidateGroups: []*entities.Group{nil, {ID: id, Name: "approvers"}}}
	if !inCandidateGroupKeys(task, []string{"approvers"}) {
		t.Error("the group's name did not match")
	}
	if !inCandidateGroupKeys(task, []string{id.String()}) {
		t.Error("the group's id did not match")
	}
	if inCandidateGroupKeys(task, []string{"reviewers", uuid.NewString()}) {
		t.Error("another group matched")
	}
}
