package bpmn_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	handlersimpl "github.com/gsoultan/metis/server/domains/handlers/impl"
	observerimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// What a service task actually does had no test.
//
// Every process test substituted testutils.SynchronousJobService, which marks
// the node complete and moves on without touching the code that calls anything.
// So the suite stayed green while: connector settings were dropped in transit,
// instances were stored belonging to no project, and a task whose connector had
// gone missing quietly did nothing and reported success.
//
// These run the real job service against a real HTTP server, because the point
// is whether the call happens and what comes back.

// The service task in docs/data-flow.md, executed: variables out under the
// names the endpoint expects, response fields back under the names the process
// uses, and everything unmapped left behind.
func TestServiceTask_MapsVariablesOutAndResponseFieldsBack(t *testing.T) {
	var gotBody map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"registration_id": "09876543",
			"credit_score":    42,
			"status":          "active",
			"incorporated":    "2011-04-02",
		})
	}))
	defer api.Close()

	h := newServiceTaskHarness(t)
	instance := h.run(t, entities.Node{
		ID:   "lookup",
		Name: "Look up the company",
		Type: entities.ServiceTask,
		Properties: map[string]any{
			"http_url":            api.URL,
			"http_method":         "POST",
			"input_companyNumber": "registration_id",
			"output_credit_score": "creditScore",
			"output_status":       "companyStatus",
		},
	}, map[string]any{"companyNumber": "09876543", "supplierName": "Northwind Ltd"})

	// Out: the process's companyNumber, under the endpoint's name for it.
	if got := gotBody["registration_id"]; got != "09876543" {
		t.Errorf("the endpoint received registration_id=%#v, want \"09876543\" — the input mapping did not rename it", got)
	}
	if _, leaked := gotBody["companyNumber"]; leaked {
		t.Error("the endpoint received companyNumber; the mapping renames rather than adds")
	}

	// Back: only the fields with an output mapping, under the process's names.
	if got := instance.Variables["creditScore"]; got != float64(42) {
		t.Errorf("creditScore = %#v, want 42 — the output mapping did not bring it back", got)
	}
	if got := instance.Variables["companyStatus"]; got != "active" {
		t.Errorf("companyStatus = %#v, want \"active\"", got)
	}
	if _, kept := instance.Variables["incorporated"]; kept {
		t.Error("incorporated was stored; a response field with no output mapping is not kept")
	}
	if _, kept := instance.Variables["credit_score"]; kept {
		t.Error("credit_score was stored under the endpoint's name as well as the mapped one")
	}
}

// The failure that reported success: a task naming a connector instance that no
// longer exists. It has no http_url — nothing configured to post to Slack does
// — so resolution used to fall through to the HTTP runner, which treats that as
// a no-op, and the process completed having notified nobody.
func TestServiceTask_WithAMissingConnectorRaisesRatherThanCompleting(t *testing.T) {
	h := newServiceTaskHarness(t)
	instance := h.run(t, entities.Node{
		ID:   "notify",
		Name: "Send the notification",
		Type: entities.ServiceTask,
		Properties: map[string]any{
			"connector_instance_id": uuid.Must(uuid.NewV7()).String(),
		},
	}, map[string]any{"message": "deploy finished"})

	if instance.Status == entities.ProcessCompleted {
		t.Fatal("the process completed even though the notification was never sent")
	}

	job := h.lastJob(t, instance.ID)
	if job.LastError == "" {
		t.Fatal("nothing was recorded against the job that failed")
	}
	if !strings.Contains(job.LastError, "notify") || !strings.Contains(job.LastError, "connector") {
		t.Errorf("the recorded error does not identify the node or what it could not reach: %q", job.LastError)
	}
}

// An endpoint that answers 500 is a failure, not a result to carry on with.
func TestServiceTask_TreatsAnErrorResponseAsAFailure(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"upstream exploded"}`, http.StatusInternalServerError)
	}))
	defer api.Close()

	h := newServiceTaskHarness(t)
	instance := h.run(t, entities.Node{
		ID:         "call",
		Name:       "Call the thing",
		Type:       entities.ServiceTask,
		Properties: map[string]any{"http_url": api.URL, "http_method": "POST"},
	}, map[string]any{"x": 1})

	if instance.Status == entities.ProcessCompleted {
		t.Fatal("the process completed despite the endpoint returning 500")
	}
	if job := h.lastJob(t, instance.ID); job.LastError == "" {
		t.Error("a 500 response left nothing recorded against the job")
	}
}

// ─── harness ─────────────────────────────────────────────────────────────────

type serviceTaskHarness struct {
	repo        repositories.Repository
	engine      contracts.ExecutionEngine
	jobSvc      contracts.JobService
	defSvc      contracts.DefinitionService
	taskSvc     contracts.TaskService
	decisionSvc contracts.DecisionService
	projectID   uuid.UUID
}

// subProcessesOf returns the instances started by this one.
func (h *serviceTaskHarness) subProcessesOf(t *testing.T, parentID uuid.UUID) []entities.ProcessInstance {
	t.Helper()
	children, err := h.engine.ListSubProcesses(t.Context(), parentID)
	if err != nil {
		t.Fatalf("list sub-processes: %v", err)
	}
	return children
}

// tasksFor returns the tasks an instance is waiting on.
func (h *serviceTaskHarness) tasksFor(t *testing.T, instanceID uuid.UUID) []entities.Task {
	t.Helper()
	all, err := h.taskSvc.ListTasks(t.Context(), h.projectID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	var mine []entities.Task
	for _, task := range all {
		if task.Instance != nil && task.Instance.ID == instanceID {
			mine = append(mine, task)
		}
	}
	return mine
}

// newServiceTaskHarness wires the real job service rather than the synchronous
// double, because the double is exactly what these tests exist to stop
// standing in for.
func newServiceTaskHarness(t *testing.T) *serviceTaskHarness {
	t.Helper()
	ctx := t.Context()

	// A service task URL comes from a process definition, which is user-authored,
	// so the HTTP client refuses loopback and private addresses unless told
	// otherwise — that is what stops an author pointing a task at
	// 169.254.169.254 and reading cloud credentials. The stub endpoints below
	// are on 127.0.0.1, so the tests opt in for themselves rather than the guard
	// being relaxed anywhere it matters. It is read per dial, so setting it here
	// takes effect.
	t.Setenv("GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	repo := repositories.NewRepository(testutils.SetupTestDB(t))

	engine := serviceimpl.NewExecutionEngine(repo, observerimpl.NewEventDispatcher())
	connectorSvc := serviceimpl.NewConnectorService(repo)
	taskSvc := serviceimpl.NewTaskService(repo, engine, serviceimpl.NewAuditWriter(repo.Audit()))
	externalTaskSvc := serviceimpl.NewExternalTaskService(repo, engine)
	decisionSvc := serviceimpl.NewDecisionService(repo, serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))
	jobSvc := serviceimpl.NewJobService(repo, engine, connectorSvc,
		serviceimpl.NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())

	engine.Apply(
		serviceimpl.WithHandlerFactory(handlersimpl.NewNodeHandlerFactory(
			engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc,
			repo.Subscription(), serviceimpl.NewAuditWriter(repo.Audit()),
		)),
		serviceimpl.WithJobService(jobSvc),
	)

	org, err := serviceimpl.NewOrganizationService(repo).CreateOrganization(ctx, "Acme", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	project, err := serviceimpl.NewProjectService(repo).CreateProject(ctx, org.ID, "Demo", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	return &serviceTaskHarness{
		repo:        repo,
		engine:      engine,
		jobSvc:      jobSvc,
		defSvc:      serviceimpl.NewDefinitionService(repo),
		taskSvc:     taskSvc,
		decisionSvc: decisionSvc,
		projectID:   project.ID,
	}
}

// run puts the node in a start → node → end process, starts it, and drains the
// job queue so the service task has actually been attempted before returning.
func (h *serviceTaskHarness) run(t *testing.T, node entities.Node, variables map[string]any) entities.ProcessInstance {
	t.Helper()

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projectID},
		Key:     "service-task-" + node.ID,
		Name:    "Service task under test",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			&node,
			{ID: "end", Type: entities.EndEvent, Name: "End"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: node.ID},
			{ID: "f2", SourceRef: node.ID, TargetRef: "end"},
		},
	}
	return h.runDefinition(t, &def, variables)
}

// runDefinition starts a process of whatever shape the test needs and drains
// the job queue.
func (h *serviceTaskHarness) runDefinition(t *testing.T, def *entities.ProcessDefinition, variables map[string]any) entities.ProcessInstance {
	t.Helper()
	ctx := t.Context()

	if def.Project == nil {
		def.Project = &entities.Project{ID: h.projectID}
	}
	if _, err := h.defSvc.CreateDefinition(ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.engine.StartProcess(ctx, h.projectID, def.Key, variables)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// The service task is queued rather than run inline, so nothing has been
	// called yet at this point.
	//
	// Drained repeatedly because running a job can queue the next one — a
	// sequential multi-instance node starts each iteration when the one before
	// it finishes. The real worker gets there on its next tick; a test should
	// not have to sleep for one. The loop ends when a round does no work, so a
	// node that genuinely stalls still shows up as a stalled instance.
	for range 20 {
		before := h.pendingJobs(t)
		if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
			t.Fatalf("process pending jobs: %v", err)
		}
		if before == 0 {
			break
		}
	}

	updated, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	return updated
}

// pendingJobs counts the work waiting to be picked up.
func (h *serviceTaskHarness) pendingJobs(t *testing.T) int {
	t.Helper()
	jobs, err := h.repo.Job().GetPending(t.Context(), 50)
	if err != nil {
		t.Fatalf("count pending jobs: %v", err)
	}
	return len(jobs)
}

// lastJob returns the instance's most recent job, for the error it recorded.
func (h *serviceTaskHarness) lastJob(t *testing.T, instanceID uuid.UUID) entities.Job {
	t.Helper()
	models, err := h.repo.Job().ListByInstance(t.Context(), instanceID)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("no job was created for the service task")
	}
	return adapters.JobEntityAdapter{Model: models[len(models)-1]}.ToEntity()
}
