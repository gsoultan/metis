package app

import (
	"context"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/externaltask"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm/runtime"
)

// A repository method nothing calls is how the retention sweeps went missing,
// so this holds the running server to calling ReofferStranded: a task an older
// release left at zero retries, with no incident, is offered again.
func TestARunningServerOffersAgainAnExternalTaskNobodyWasToldAbout(t *testing.T) {
	conn := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	a := &App{storm: conn, repo: repo, sse: sse,
		svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "reoffer-test", nil, nil, nil)}

	ex := testutils.Executor(conn)
	_, _, project := testutils.ScopedProject(t, repo)
	definition := testutils.SeedDefinition(t, conn, project, "shipping")
	stranded := externaltask.Create()
	stranded.SetProjectID(project)
	stranded.SetInstanceID(testutils.SeedInstance(t, conn, project, definition, models.ProcessActive))
	stranded.SetDefinitionID(definition)
	stranded.SetNodeID("ship")
	stranded.SetTopic("shipping")
	stranded.SetRetries(0)
	stranded.SetRetryTimeout(0)
	stranded.SetVariables(runtime.JSON("{}"))
	row, err := stranded.Insert(t.Context(), ex)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a.startBackgroundWork(ctx)

	var retries int64
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		read, _, err := externaltask.New().IDEq(row.ID).One(t.Context(), ex)
		if err != nil {
			t.Fatalf("read the task: %v", err)
		}
		if retries = read.Retries; retries == 1 {
			return
		}
	}
	t.Fatalf("ten seconds into a running server, the stranded task still has %d retries and is off offer", retries)
}
