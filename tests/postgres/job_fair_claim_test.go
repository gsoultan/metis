package postgres_test

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// queueJobs makes n jobs due for one organization, the first due at oldest and
// each a second after the one before. A nil organization is a job made before
// jobs carried one.
// queued is the case each test's queued jobs belong to: one, since what a job
// is claimed by is its organization, which is the job's own column.
var queued sync.Map

func queueCase(t *testing.T, repo repositories.Repository) (uuid.UUID, uuid.UUID) {
	t.Helper()
	if c, ok := queued.Load(t); ok {
		pair := c.([2]uuid.UUID)
		return pair[0], pair[1]
	}
	definition, instance := aCase(t, repo)
	queued.Store(t, [2]uuid.UUID{definition, instance})
	t.Cleanup(func() { queued.Delete(t) })
	return definition, instance
}

func queueJobs(t *testing.T, repo repositories.Repository, organization *uuid.UUID, oldest time.Time, n int) map[uuid.UUID]bool {
	t.Helper()
	ctx := entities.WithSystemContext(t.Context())
	made := map[uuid.UUID]bool{}
	definition, instance := queueCase(t, repo)
	for i := range n {
		j := models.JobModel{
			Base:       models.Base{ID: models.UUID(uuid.New())},
			InstanceID: models.UUID(instance), DefinitionID: models.UUID(definition),
			Type:      "timer",
			Status:    models.JobPending,
			NextRunAt: oldest.Add(time.Duration(i) * time.Second),
		}
		if organization != nil {
			id := models.UUID(*organization)
			j.OrganizationID = &id
		}
		id, err := repo.Job().Create(ctx, j)
		if err != nil {
			t.Fatalf("queue a job: %v", err)
		}
		made[id] = true
	}
	return made
}

// One organization that queued a flood of timers used to have every worker
// until the flood had run: the claim took the oldest due jobs whoever they
// were. Now each organization's oldest is taken in turn, so another's work,
// queued after the flood, is in the first round.
func TestTheClaimTakesEachOrganizationsOldestInTurn(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	now := time.Now().UTC()
	flooding, quiet := uuid.New(), uuid.New()

	flood := queueJobs(t, repo, &flooding, now.Add(-time.Hour), 40)
	waiting := queueJobs(t, repo, &quiet, now.Add(-time.Minute), 3)
	unowned := queueJobs(t, repo, nil, now.Add(-30*time.Minute), 2)

	offered, err := repo.Job().GetPending(t.Context(), 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(offered) != 10 {
		t.Fatalf("offered %d jobs, want a full round of 10", len(offered))
	}
	counts := map[string]int{}
	for i, j := range offered {
		id := uuid.UUID(j.ID)
		switch {
		case flood[id]:
			counts["flooding"]++
		case waiting[id]:
			counts["quiet"]++
		case unowned[id]:
			counts["unowned"]++
		}
		// The first turn is one of each, oldest first.
		if i < 3 && counts["flooding"] > 1 {
			t.Errorf("the first turn gave the flooding organization a second job before the others had one: %v", counts)
		}
	}
	if counts["quiet"] != 3 || counts["unowned"] != 2 || counts["flooding"] != 5 {
		t.Errorf("a round of 10 gave %v; want the quiet organization's 3, the 2 with no organization and 5 of the flood", counts)
	}
	for i := 1; i < len(offered); i++ {
		if offered[i].OrganizationID != nil && offered[i-1].OrganizationID != nil &&
			*offered[i].OrganizationID == *offered[i-1].OrganizationID && *offered[i].OrganizationID == models.UUID(quiet) {
			t.Errorf("the quiet organization's jobs came one after another, not in turns: positions %d and %d", i-1, i)
		}
	}
}

// With nobody else's work due, the claim is what it was: one organization's
// oldest first, a full round of them.
func TestOneOrganizationAloneGetsAFullRoundOldestFirst(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	now := time.Now().UTC()
	alone := uuid.New()
	queueJobs(t, repo, &alone, now.Add(-time.Hour), 12)

	offered, err := repo.Job().GetPending(t.Context(), 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(offered) != 10 {
		t.Fatalf("offered %d jobs, want 10", len(offered))
	}
	for i := 1; i < len(offered); i++ {
		if offered[i].NextRunAt.Before(offered[i-1].NextRunAt) {
			t.Errorf("job %d is older than job %d; one organization's are offered oldest first", i, i-1)
		}
	}
}

// A job whose worker died is offered again in its organization's turn, and one
// still held, or not yet due, is not offered at all.
func TestAnAbandonedJobIsOfferedInItsOrganizationsTurn(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	ctx := entities.WithSystemContext(t.Context())
	now := time.Now().UTC()
	organization := models.UUID(uuid.New())
	expired, live := now.Add(-time.Minute), now.Add(time.Minute)
	definition, instance := aCase(t, repo)

	park := func(status models.JobStatus, due time.Time, lease *time.Time) uuid.UUID {
		t.Helper()
		id, err := repo.Job().Create(ctx, models.JobModel{
			Base: models.Base{ID: models.UUID(uuid.New())}, Type: "timer", Status: status,
			InstanceID: models.UUID(instance), DefinitionID: models.UUID(definition),
			NextRunAt: due, LockExpires: lease, OrganizationID: &organization,
		})
		if err != nil {
			t.Fatalf("make a job: %v", err)
		}
		return id
	}
	abandoned := park(models.JobRunning, now.Add(-time.Hour), &expired)
	held := park(models.JobRunning, now.Add(-time.Hour), &live)
	later := park(models.JobPending, now.Add(time.Hour), nil)

	offered, err := repo.Job().GetPending(t.Context(), 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	ids := map[uuid.UUID]bool{}
	for _, j := range offered {
		ids[uuid.UUID(j.ID)] = true
	}
	if !ids[abandoned] {
		t.Error("a job whose lease ran out was not offered again")
	}
	if ids[held] || ids[later] {
		t.Errorf("offered a job still held or not yet due: %v", offered)
	}
}

// A job made for an instance says whose work it is, from the instance's
// project, without the caller naming it.
func TestAJobTakesItsOrganizationFromItsInstance(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx, organization, project := testutils.ScopedProject(t, repo)
	definition := testutils.SeedDefinition(t, db, project, "remind")
	instance := uuid.Must(uuid.NewV7())
	if _, err := repo.Process().Create(ctx, models.ProcessInstanceModel{
		Base: models.Base{ID: models.FromUUID(instance)}, ProjectID: models.FromUUID(project),
		DefinitionID: models.FromUUID(definition), Status: models.ProcessActive,
	}); err != nil {
		t.Fatalf("seed the instance: %v", err)
	}

	system := entities.WithSystemContext(t.Context())
	id, err := repo.Job().Create(system, models.JobModel{
		Base: models.Base{ID: models.UUID(uuid.New())}, InstanceID: models.FromUUID(instance),
		DefinitionID: models.FromUUID(definition), Type: "timer", Status: models.JobPending, NextRunAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("make the job: %v", err)
	}
	stored, err := repo.Job().Get(system, id)
	if err != nil {
		t.Fatalf("read the job: %v", err)
	}
	if stored.OrganizationID == nil || uuid.UUID(*stored.OrganizationID) != organization {
		t.Errorf("the job's organization is %v, want its instance's, %s", stored.OrganizationID, organization)
	}
}

// A round looks at a bounded window of organizations, so with more of them
// than that it must still be filled, oldest first, and leave nobody out.
func TestARoundIsFilledWhenThereAreMoreOrganizationsThanItLooksAt(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	now := time.Now().UTC()
	queued := map[uuid.UUID]bool{}
	for i := range 1001 {
		organization := uuid.New()
		for id := range queueJobs(t, repo, &organization, now.Add(-time.Hour+time.Duration(i)*time.Millisecond), 1) {
			queued[id] = true
		}
	}

	offered, err := repo.Job().GetPending(t.Context(), 2000)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	seen := map[uuid.UUID]bool{}
	for _, j := range offered {
		seen[uuid.UUID(j.ID)] = true
	}
	if len(offered) != len(queued) || len(seen) != len(queued) {
		t.Errorf("offered %d jobs (%d distinct) of the %d due, one for each organization", len(offered), len(seen), len(queued))
	}
}
