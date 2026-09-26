package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// A repository method nothing calls is how the retention sweeps went missing,
// so this holds the running server to calling ReofferStranded: a task an older
// release left at zero retries, with no incident, is offered again.
func TestARunningServerOffersAgainAnExternalTaskNobodyWasToldAbout(t *testing.T) {
	gormDB := testutils.SetupTestDB(t)
	conn := testutils.StormConn(gormDB)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	a := &App{db: gormDB, storm: conn, repo: repo, sse: sse,
		svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "reoffer-test", nil, nil, nil, func(*gorm.DB) {})}

	task, now := uuid.New(), time.Now()
	if err := gormDB.WithContext(t.Context()).Exec(`
		INSERT INTO external_tasks (id, created_at, updated_at, project_id, instance_id, definition_id,
			node_id, topic, retries, retry_timeout)
		VALUES (?, ?, ?, ?, ?, ?, 'ship', 'shipping', 0, 0)`,
		task, now, now, uuid.New(), uuid.New(), uuid.New()).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a.startBackgroundWork(ctx)

	var retries int
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if err := gormDB.WithContext(t.Context()).Raw(
			`SELECT retries FROM external_tasks WHERE id = ?`, task).Scan(&retries).Error; err != nil {
			t.Fatalf("read the task: %v", err)
		}
		if retries == 1 {
			return
		}
	}
	t.Fatalf("ten seconds into a running server, the stranded task still has %d retries and is off offer", retries)
}
