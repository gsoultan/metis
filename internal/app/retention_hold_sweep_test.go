package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// heldSweep is an app with the finished-instance sweep on, over a project of
// finished instances a test can hold.
type heldSweep struct {
	t          *testing.T
	app        *App
	db         *gorm.DB
	repo       repositories.Repository
	project    uuid.UUID
	definition uuid.UUID
	tenant     context.Context
	keep       optInRetention
}

func newHeldSweep(t *testing.T) heldSweep {
	t.Helper()
	gormDB := testutils.SetupTestDB(t)
	conn := testutils.StormConn(gormDB)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	a := &App{db: gormDB, storm: conn, repo: repo, sse: sse,
		svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "retention-hold-test", nil, nil, nil)}
	tenant, _, projectID := testutils.ScopedProject(t, repo)
	definitionID := uuid.Must(uuid.NewV7())
	if err := repo.Definition().Create(tenant, models.ProcessDefinitionModel{
		Base: models.Base{ID: models.FromUUID(definitionID)}, ProjectID: models.FromUUID(projectID),
		Key: "claims", Name: "Claims", Version: 1,
	}); err != nil {
		t.Fatalf("seed the definition: %v", err)
	}
	t.Setenv(envRetainFinishedInstancesDays, "30")
	return heldSweep{t: t, app: a, db: gormDB, repo: repo, project: projectID, definition: definitionID,
		tenant: tenant, keep: resolveOptInRetention()}
}

// finished seeds an instance that completed long ago, called from parent when
// parent is not uuid.Nil.
func (h heldSweep) finished(parent uuid.UUID) uuid.UUID {
	h.t.Helper()
	id := uuid.Must(uuid.NewV7())
	if _, err := h.repo.Process().Create(h.tenant, models.ProcessInstanceModel{
		Base: models.Base{ID: models.FromUUID(id)}, ProjectID: models.FromUUID(h.project),
		DefinitionID: models.FromUUID(h.definition), Status: models.ProcessCompleted,
	}); err != nil {
		h.t.Fatalf("seed an instance: %v", err)
	}
	h.exec(`UPDATE process_instances SET updated_at = ? WHERE id = ?`, time.Now().Add(-longAgo), id)
	if parent != uuid.Nil {
		h.exec(`UPDATE process_instances SET parent_instance_id = ? WHERE id = ?`, parent, id)
	}
	return id
}

func (h heldSweep) exec(stmt string, args ...any) {
	h.t.Helper()
	if err := h.db.WithContext(h.t.Context()).Exec(stmt, args...).Error; err != nil {
		h.t.Fatalf("%s: %v", stmt, err)
	}
}

func (h heldSweep) hold(ctx context.Context, instance uuid.UUID) error {
	_, err := h.repo.RetentionHold().Place(ctx, entities.RetentionHold{
		Project: &entities.Project{ID: h.project}, Instance: &entities.ProcessInstance{ID: instance},
		Reason: "litigation", PlacedBy: "ada", PlacedByID: uuid.Must(uuid.NewV7()),
	})
	return err
}

func (h heldSweep) sweep() {
	h.app.sweepRetention(entities.WithSystemContext(h.t.Context()), time.Now(), h.keep)
}

func (h heldSweep) left(id uuid.UUID) bool {
	h.t.Helper()
	var n int64
	if err := h.db.WithContext(h.t.Context()).Raw(`SELECT count(*) FROM process_instances WHERE id = ?`, id).Scan(&n).Error; err != nil {
		h.t.Fatalf("count: %v", err)
	}
	return n == 1
}

// A hold on one instance keeps the whole case it is part of: the instance
// that started the chain of calls, and every instance started from it at any
// depth, siblings of the held one included. Released, the case is swept like
// any other.
func TestAHoldKeepsTheWholeCaseItIsPartOfUntilItIsReleased(t *testing.T) {
	h := newHeldSweep(t)
	root := h.finished(uuid.Nil)
	held := h.finished(root)
	sibling := h.finished(root)
	grandchild := h.finished(held)
	otherRoot := h.finished(uuid.Nil)
	otherChild := h.finished(otherRoot)
	alone := h.finished(uuid.Nil)

	system := entities.WithSystemContext(t.Context())
	if err := h.repo.UnitOfWork().Do(system, func(ctx context.Context) error { return h.hold(ctx, held) }); err != nil {
		t.Fatalf("hold: %v", err)
	}
	h.sweep()

	for name, id := range map[string]uuid.UUID{"root": root, "held": held, "sibling": sibling, "grandchild": grandchild} {
		if !h.left(id) {
			t.Errorf("the sweep removed the %s instance of a held case", name)
		}
	}
	for name, id := range map[string]uuid.UUID{"other root": otherRoot, "other child": otherChild, "unrelated": alone} {
		if h.left(id) {
			t.Errorf("the sweep kept the %s instance, which no hold names", name)
		}
	}

	if err := h.repo.UnitOfWork().Do(system, func(ctx context.Context) error {
		_, err := h.repo.RetentionHold().Release(ctx, held)
		return err
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	h.sweep()
	for name, id := range map[string]uuid.UUID{"root": root, "held": held, "sibling": sibling, "grandchild": grandchild} {
		if h.left(id) {
			t.Errorf("the %s instance was kept after its hold was released", name)
		}
	}
}

// A hold being placed when a sweep batch starts is waited for. The batch
// reads the holds once, as it starts; without the wait it would not see a
// hold that commits while it runs, and would remove the instance the hold was
// placed to keep. The hold does not lock the instance's row, so nothing else
// would stop it.
func TestASweepWaitsForAHoldBeingPlacedAndKeepsWhatItHolds(t *testing.T) {
	h := newHeldSweep(t)
	instance := h.finished(uuid.Nil)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	placed, commit, held := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	// Let the hold commit however the test ends, so a failure below does not
	// leave a transaction open that the database's cleanup waits on.
	release := sync.OnceFunc(func() { close(commit) })
	defer release()
	go func() {
		held <- h.repo.UnitOfWork().Do(entities.WithSystemContext(ctx), func(ctx context.Context) error {
			if err := h.hold(ctx, instance); err != nil {
				return err
			}
			close(placed)
			<-commit
			return nil
		})
	}()
	select {
	case <-placed:
	case err := <-held:
		t.Fatalf("hold: %v", err)
	}

	swept := make(chan struct{})
	go func() {
		h.app.sweepRetention(entities.WithSystemContext(ctx), time.Now(), h.keep)
		close(swept)
	}()
	select {
	case <-swept:
		t.Fatal("the sweep ran while a hold was being placed instead of waiting for it")
	case <-time.After(300 * time.Millisecond):
	}
	release()
	if err := <-held; err != nil {
		t.Fatalf("hold: %v", err)
	}
	select {
	case <-swept:
	case <-ctx.Done():
		t.Fatal("the sweep did not finish once the hold was placed")
	}
	if !h.left(instance) {
		t.Fatal("the sweep removed an instance a hold placed while it waited was keeping")
	}
}
