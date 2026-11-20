package app

import (
	"context"
	"os"
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

func TestARoleIsReadAsWrittenAndAnythingElseIsRefused(t *testing.T) {
	for value, want := range map[string]Role{"": RoleAll, "all": RoleAll, " Web ": RoleWeb, "WORKER": RoleWorker} {
		got, err := ParseRole(value)
		if err != nil || got != want {
			t.Errorf("ParseRole(%q) = %q, %v; want %q", value, got, err, want)
		}
	}
	for _, value := range []string{"workers", "api", "both"} {
		if _, err := ParseRole(value); err == nil {
			t.Errorf("ParseRole(%q) was accepted; a typo must stop the engine", value)
		}
	}
	if RoleWeb.RunsWork() || !RoleAll.RunsWork() || !RoleWorker.RunsWork() {
		t.Error("only the web role may leave the work to other processes")
	}
}

// The role a joined machine was given is written to config.yaml, and read from
// there unless the environment says otherwise.
func TestTheRoleComesFromTheEnvironmentOrElseConfigYAML(t *testing.T) {
	t.Chdir(t.TempDir())
	if role, err := ConfiguredRole(); err != nil || role != RoleAll {
		t.Fatalf("with neither: %q, %v; want all", role, err)
	}
	if err := os.WriteFile("config.yaml", []byte("server:\n  role: worker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if role, err := ConfiguredRole(); err != nil || role != RoleWorker {
		t.Fatalf("from config.yaml: %q, %v; want worker", role, err)
	}
	t.Setenv(envRole, "web")
	if role, err := ConfiguredRole(); err != nil || role != RoleWeb {
		t.Fatalf("the environment did not win over config.yaml: %q, %v", role, err)
	}
	t.Setenv(envRole, "")
	if role, err := ConfiguredRole(); err != nil || role != RoleAll {
		t.Fatalf("an empty METIS_ROLE is the default, whatever the file says: %q, %v", role, err)
	}
	if err := os.Unsetenv(envRole); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config.yaml", []byte("server:\n  role: wroker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfiguredRole(); err == nil {
		t.Fatal("a misspelt role in config.yaml was accepted")
	}
}

// A web process serves and leaves the work to the workers: what the running
// server in reoffer_test.go offers again within seconds, a web process leaves
// alone. And it stops at once, with nothing of its own to drain.
func TestAWebProcessRunsNoWork(t *testing.T) {
	conn := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	a := &App{role: RoleWeb, storm: conn, repo: repo, sse: sse,
		svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "role-test", nil, nil, nil)}

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
	a.startBackgroundWork(ctx)
	time.Sleep(3 * time.Second)
	read, _, err := externaltask.New().IDEq(row.ID).One(t.Context(), ex)
	if err != nil {
		t.Fatalf("read the task: %v", err)
	}
	if read.Retries != 0 {
		t.Fatalf("a web process offered the stranded task again (retries %d); that is a worker's to do", read.Retries)
	}

	cancel()
	drain, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	start := time.Now()
	a.stopBackgroundWork(drain)
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("a web process took %s to stop with no work of its own", took)
	}
}
