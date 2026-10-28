package postgres_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// A topic is counted once per organization however many of its projects have
// it on offer, and a removed project's tasks still count against the
// organization that owned it.
func TestAnUnservedTopicIsCountedOncePerOrganization(t *testing.T) {
	repo := repositories.NewRepository(testutils.StormConn(testutils.SetupTestDB(t)))
	ctx, organization, first := testutils.ScopedProject(t, repo)
	second := uuid.Must(uuid.NewV7())
	if err := repo.Project().Create(entities.WithSystemContext(ctx), models.ProjectModel{
		Base:           models.Base{ID: models.UUID(second)},
		OrganizationID: models.UUID(organization),
		Name:           "Second Project",
	}); err != nil {
		t.Fatalf("seed a second project: %v", err)
	}
	removedCtx, _, removed := testutils.ScopedProject(t, repo)
	for _, project := range []uuid.UUID{first, second} {
		waitingOn(t, repo, project, "shipping")
		waitingOn(t, repo, project, "billing")
	}
	waitingOn(t, repo, removed, "shipping")
	if err := repo.Project().Delete(entities.WithSystemContext(removedCtx), removed); err != nil {
		t.Fatalf("remove a project: %v", err)
	}

	unserved, err := repo.ExternalTaskWorker().CountUnserved(entities.WithSystemContext(t.Context()), time.Now())
	if err != nil {
		t.Fatalf("count the unserved topics: %v", err)
	}
	if unserved != 3 {
		t.Errorf("%d topics are unserved, want three: two of the organization with two projects, one of the removed project's", unserved)
	}
}

// The count is not a page: more topics than one read returns are all counted.
func TestEveryUnservedTopicIsCounted(t *testing.T) {
	repo := repositories.NewRepository(testutils.StormConn(testutils.SetupTestDB(t)))
	_, _, project := testutils.ScopedProject(t, repo)
	const topics = 1001
	for i := range topics {
		waitingOn(t, repo, project, fmt.Sprintf("topic-%04d", i))
	}

	unserved, err := repo.ExternalTaskWorker().CountUnserved(entities.WithSystemContext(t.Context()), time.Now())
	if err != nil {
		t.Fatalf("count the unserved topics: %v", err)
	}
	if unserved != topics {
		t.Errorf("%d topics are unserved, want %d", unserved, topics)
	}
}
