package bpmn_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/tests/testutils"
)

// What a repeating step does when it is not an approval.
//
// Counting the runs of a repeating step strictly — one completion retires one
// run's token, a completion with nothing to retire is refused, a step that
// ends early withdraws what it left open — is the rule for a user task or a
// manual task that repeats, and for nothing else. The runs of a repeating
// sub-process share the tokens of the steps inside it, so the same rule there
// refuses work that is owed an advance; and a repeating external task, call
// activity or service task was never counted that way.
//
// So every other repeating shape, and every step that runs once, does exactly
// what it did before the rule existed (BPMN 2.0.2 §10.3.8 Multi-Instance,
// §13.3.7 loop characteristics execution — including the places where that is
// looser than the standard asks). Each test here drives one such shape and
// compares what the instance looks like after every step with what the
// release before the rule left behind: the pins in
// repeating_shapes_unchanged_pins_test.go were recorded by running this file,
// unchanged, against that release.
//
// A shape that hangs there hangs here, and is pinned as hanging: making it
// finish is the follow-up that gives each run of a repeating sub-process its
// own tokens, not something to happen by accident.

// shapeRun is one instance being driven through a shape.
type shapeRun struct {
	t     *testing.T
	h     engineHarness
	id    uuid.UUID
	calls *atomic.Int32
}

var instanceIDPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// plain is an outcome with the ids taken out, so it reads the same on every
// run.
func plain(err error) string {
	if err == nil {
		return "ok"
	}
	return instanceIDPattern.ReplaceAllString(err.Error(), "<id>")
}

func startShape(t *testing.T, h engineHarness, def *entities.ProcessDefinition, vars map[string]any) *shapeRun {
	t.Helper()
	h.deploy(t, def)
	id, err := h.svc.StartProcess(h.Ctx(), h.projID, def.Key, vars)
	if err != nil {
		t.Fatalf("start %s: %v", def.Key, err)
	}
	return &shapeRun{t: t, h: h, id: id}
}

type shapeCount struct {
	NodeID string
	Kind   string
	Status string
	N      int
}

func (r *shapeRun) counts(query string) string {
	r.t.Helper()
	var rows []shapeCount
	if err := r.h.db.Raw(query, r.id).Scan(&rows).Error; err != nil {
		r.t.Fatalf("count: %v", err)
	}
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		label := row.NodeID
		if row.Kind != "" {
			label += ":" + row.Kind
		}
		if row.Status != "" {
			label += ":" + row.Status
		}
		parts = append(parts, fmt.Sprintf("%s×%d", strings.TrimPrefix(label, ":"), row.N))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// state is everything about the instance that a different engine rule could
// change: where its tokens are, what it is counting, the tasks, parked work,
// jobs, incidents and called processes it has, and how many calls it made.
func (r *shapeRun) state() string {
	r.t.Helper()
	instance, err := r.h.engine.GetInstance(r.h.Ctx(), r.id)
	if err != nil {
		r.t.Fatalf("reload instance: %v", err)
	}
	tokens := make([]string, 0, len(instance.Tokens))
	for _, token := range instance.Tokens {
		at := "?"
		if token.Node != nil {
			at = token.Node.ID
		}
		if token.IterationID != "" {
			at += "#" + token.IterationID
		}
		tokens = append(tokens, at)
	}
	sort.Strings(tokens)
	counting := make([]string, 0, len(instance.MultiInstance))
	for nodeID, progress := range instance.MultiInstance {
		counting = append(counting, fmt.Sprintf("%s:%d/%d", nodeID, progress.Completed, progress.Total))
	}
	sort.Strings(counting)

	state := fmt.Sprintf("status=%s tokens=%v counting=%v tasks=%s parked=%s jobs=%s incidents=%s called=%s",
		instance.Status, tokens, counting,
		r.counts(`SELECT node_id, status, count(*) AS n FROM tasks
			WHERE instance_id = ? AND deleted_at IS NULL GROUP BY 1, 2 ORDER BY 1, 2`),
		r.counts(`SELECT node_id, count(*) AS n FROM external_tasks
			WHERE instance_id = ? AND deleted_at IS NULL GROUP BY 1 ORDER BY 1`),
		r.counts(`SELECT node_id, type AS kind, status, count(*) AS n FROM jobs
			WHERE instance_id = ? AND deleted_at IS NULL GROUP BY 1, 2, 3 ORDER BY 1, 2, 3`),
		r.counts(`SELECT status, count(*) AS n FROM incidents
			WHERE instance_id = ? AND deleted_at IS NULL GROUP BY 1 ORDER BY 1`),
		r.counts(`SELECT status, count(*) AS n FROM process_instances
			WHERE parent_instance_id = ? AND deleted_at IS NULL GROUP BY 1 ORDER BY 1`))
	if r.calls != nil {
		state += fmt.Sprintf(" calls=%d", r.calls.Load())
	}
	return state
}

// pin compares the instance, and the outcome of what was just done to it,
// with what the release before the rule left at the same step.
func (r *shapeRun) pin(step string, outcome ...string) {
	r.t.Helper()
	got := r.state()
	if len(outcome) > 0 {
		got = "outcome=" + strings.Join(outcome, "; ") + " " + got
	}
	key := r.t.Name() + " | " + step
	want, pinned := unchangedShapes[key]
	if !pinned {
		r.t.Errorf("PIN\t%s\t%s", key, got)
		return
	}
	if got != want {
		r.t.Errorf("%s:\n  before the rule: %s\n  now:             %s", step, want, got)
	}
}

// completeOn completes one open task of instanceID on nodeID through the
// inbox, as a person would, and says how that went.
func (r *shapeRun) completeOn(instanceID uuid.UUID, nodeID string) string {
	r.t.Helper()
	tasks, err := r.h.svc.ListTasks(r.h.Ctx(), r.h.projID)
	if err != nil {
		r.t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == nodeID && taskIsOpen(task.Status) {
			return plain(r.h.svc.CompleteTask(testutils.AsOperator(r.h.Ctx(), "carol"), task.ID, "carol", nil))
		}
	}
	return "no open task"
}

func (r *shapeRun) complete(nodeID string) string {
	r.t.Helper()
	return r.completeOn(r.id, nodeID)
}

// fetch locks every piece of work waiting on topic for one worker, oldest
// first.
func (r *shapeRun) fetch(topic string) []*entities.ExternalTask {
	r.t.Helper()
	fetched, err := r.h.svc.FetchAndLock(r.h.Ctx(), topic, "worker-1", 10, 60_000)
	if err != nil {
		r.t.Fatalf("fetch %s: %v", topic, err)
	}
	slices.SortFunc(fetched, func(a, b *entities.ExternalTask) int { return strings.Compare(a.ID.String(), b.ID.String()) })
	return fetched
}

func (r *shapeRun) report(task *entities.ExternalTask) string {
	return plain(r.h.svc.Complete(r.h.Ctx(), task.ID, "worker-1", nil))
}

// giveUp reports a failure with no tries left.
func (r *shapeRun) giveUp(task *entities.ExternalTask) string {
	return plain(r.h.svc.HandleFailure(r.h.Ctx(), task.ID, "worker-1", "the supplier's system is down", "", 0, 0))
}

// runJob runs one pending job on nodeID — the lowest run first — and holds
// every other job of the instance back, so jobs that the worker would run side
// by side happen here in an order that is the same every time.
func (r *shapeRun) runJob(nodeID string) string {
	r.t.Helper()
	ctx := r.h.Ctx()
	jobs, err := r.h.repo.Job().ListByInstance(ctx, r.id)
	if err != nil {
		r.t.Fatalf("list jobs: %v", err)
	}
	chosen := -1
	for i, job := range jobs {
		if job.Status != "pending" {
			continue
		}
		if job.NodeID == nodeID && (chosen < 0 ||
			job.IterationID < jobs[chosen].IterationID ||
			(job.IterationID == jobs[chosen].IterationID && uuid.UUID(job.ID).String() < uuid.UUID(jobs[chosen].ID).String())) {
			chosen = i
		}
	}
	if chosen < 0 {
		return "no pending job"
	}
	for i, job := range jobs {
		if job.Status != "pending" {
			continue
		}
		job.NextRunAt = time.Now().Add(time.Hour)
		if i == chosen {
			job.NextRunAt = time.Now().Add(-time.Minute)
		}
		if err := r.h.repo.Job().Update(ctx, job); err != nil {
			r.t.Fatalf("schedule job: %v", err)
		}
	}
	return plain(r.h.jobSvc.ProcessPendingJobs(ctx))
}

// called lists the processes the instance has called, oldest first.
func (r *shapeRun) called() []uuid.UUID {
	r.t.Helper()
	var ids []string
	if err := r.h.db.Raw(`SELECT id::text FROM process_instances
		WHERE parent_instance_id = ? AND deleted_at IS NULL ORDER BY created_at, id`, r.id).Scan(&ids).Error; err != nil {
		r.t.Fatalf("list called processes: %v", err)
	}
	children := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		children = append(children, uuid.MustParse(id))
	}
	return children
}

// stageAs writes the instance's tokens and counts as given: the state an
// instance was left in by the release before the rule, whatever the release
// under test would have left on its way there.
func (r *shapeRun) stageAs(def *entities.ProcessDefinition, tokens []string, counting map[string]entities.MultiInstanceState) {
	r.t.Helper()
	ctx := r.h.Ctx()
	instance, err := r.h.engine.GetInstance(ctx, r.id)
	if err != nil {
		r.t.Fatalf("reload instance: %v", err)
	}
	instance.Tokens = nil
	for _, at := range tokens {
		nodeID, iteration, _ := strings.Cut(at, "#")
		node := def.FindNode(nodeID)
		if node == nil {
			r.t.Fatalf("stage: no step %s", nodeID)
		}
		instance.AddTokenWithIteration(node, iteration)
	}
	instance.MultiInstance = counting
	if err := r.h.engine.UpdateInstance(ctx, instance); err != nil {
		r.t.Fatalf("stage: %v", err)
	}
}

// countingCalls stands up a partner endpoint and counts the calls the instance
// makes to it.
//
// A service task's address is written by whoever designs the process, so the
// client refuses loopback unless told otherwise; the endpoint is on 127.0.0.1
// and the test opts in for itself.
func countingCalls(t *testing.T, respond http.HandlerFunc) (string, *atomic.Int32) {
	t.Helper()
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	calls := &atomic.Int32{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		respond(w, r)
	}))
	t.Cleanup(api.Close)
	return api.URL, calls
}

func respondOK(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// eachItem is start → a sub-process run once per item → record → end, with the
// given steps inside the sub-process.
func eachItem(projID uuid.UUID, key, loop string, inner []*entities.Node, innerFlows []*entities.SequenceFlow) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projID}, Key: key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "sub", Type: entities.SubProcess, Name: "Handle each item",
				MultiInstanceType: loop, Collection: "items", ElementVariable: "item"},
			{ID: "s_start", Type: entities.StartEvent, ParentID: "sub"},
			{ID: "s_end", Type: entities.EndEvent, ParentID: "sub"},
			{ID: "record", Type: entities.UserTask, Name: "Record the outcome"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "sub"},
			{ID: "f2", SourceRef: "sub", TargetRef: "record"},
			{ID: "f3", SourceRef: "record", TargetRef: "end"},
		},
	}
	for _, node := range inner {
		node.ParentID = "sub"
	}
	def.Nodes = append(def.Nodes, inner...)
	def.Flows = append(def.Flows, innerFlows...)
	return def
}

// oneStepInside is the flows of a sub-process with a single step inside.
func oneStepInside(stepID string) []*entities.SequenceFlow {
	return []*entities.SequenceFlow{
		{ID: "i1", SourceRef: "s_start", TargetRef: stepID},
		{ID: "i2", SourceRef: stepID, TargetRef: "s_end"},
	}
}

// repeated is start → one step run once per item → record → end.
func repeated(projID uuid.UUID, key string, step *entities.Node) *entities.ProcessDefinition {
	step.ID = "step"
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projID}, Key: key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			step,
			{ID: "record", Type: entities.UserTask, Name: "Record the outcome"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "step"},
			{ID: "f2", SourceRef: "step", TargetRef: "record"},
			{ID: "f3", SourceRef: "record", TargetRef: "end"},
		},
	}
}

// withDeadline attaches a deadline to the step named hostID that leads to a
// chase task of its own.
func withDeadline(def *entities.ProcessDefinition, hostID string, properties map[string]any) {
	props := map[string]any{"timer_duration": "P7D"}
	for name, value := range properties {
		props[name] = value
	}
	def.Nodes = append(def.Nodes,
		&entities.Node{ID: "deadline", Type: entities.BoundaryEvent, AttachedToRef: hostID, Properties: props},
		&entities.Node{ID: "chase", Type: entities.UserTask, Name: "Chase it"})
	def.Flows = append(def.Flows,
		&entities.SequenceFlow{ID: "d1", SourceRef: "deadline", TargetRef: "chase"},
		&entities.SequenceFlow{ID: "d2", SourceRef: "chase", TargetRef: "end"})
}

// deployReviewChild deploys the process a call activity calls: one review.
func deployReviewChild(t *testing.T, h engineHarness, key string, waits bool) {
	t.Helper()
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "c1", SourceRef: "start", TargetRef: "end"}},
	}
	if waits {
		def.Nodes = append(def.Nodes, &entities.Node{ID: "review", Type: entities.UserTask, Name: "Review"})
		def.Flows = []*entities.SequenceFlow{
			{ID: "c1", SourceRef: "start", TargetRef: "review"},
			{ID: "c2", SourceRef: "review", TargetRef: "end"},
		}
	}
	h.deploy(t, def)
}

var threeItems = map[string]any{"items": []any{"a", "b", "c"}}

var bothLoops = []string{"parallel", "sequential"}

// BPMN 2.0.2 §10.3.8, §13.3.7: a sub-process run once per item, with a task
// for a person inside it.
func TestARepeatingSubProcessWithATaskInsideIsUnchanged(t *testing.T) {
	for _, loop := range bothLoops {
		t.Run(loop, func(t *testing.T) {
			h := newEngineHarness(t, "Unchanged sub task "+loop)
			def := eachItem(h.projID, "unchanged-sub-task-"+loop, loop,
				[]*entities.Node{{ID: "review", Type: entities.UserTask, Name: "Review"}}, oneStepInside("review"))
			r := startShape(t, h, def, threeItems)
			r.pin("entered")
			for i := range 3 {
				r.pin(fmt.Sprintf("review %d", i+1), r.complete("review"))
			}
			r.pin("a fourth review", r.complete("review"))
			r.pin("recorded", r.complete("record"))
		})
	}
}

// BPMN 2.0.2 §10.3.8, §13.3.7: a sub-process run once per item, with work for
// an outside worker inside it.
func TestARepeatingSubProcessWithWorkForAWorkerInsideIsUnchanged(t *testing.T) {
	for _, loop := range bothLoops {
		t.Run(loop, func(t *testing.T) {
			h := newEngineHarness(t, "Unchanged sub external "+loop)
			topic := "unchanged-sub-external-" + loop
			def := eachItem(h.projID, topic, loop,
				[]*entities.Node{{ID: "work", Type: entities.ServiceTask, Name: "Check", ExternalTopic: topic}},
				oneStepInside("work"))
			r := startShape(t, h, def, threeItems)
			r.pin("entered")
			reports := 0
			for round := 0; round < 4 && reports < 4; round++ {
				for _, task := range r.fetch(topic) {
					reports++
					r.pin(fmt.Sprintf("report %d", reports), r.report(task))
				}
			}
			r.pin("recorded", r.complete("record"))
		})
	}
}

// BPMN 2.0.2 §10.3.8, §13.3.7: a sub-process run once per item, calling
// another process inside it.
func TestARepeatingSubProcessWithACallInsideIsUnchanged(t *testing.T) {
	for _, loop := range bothLoops {
		t.Run(loop, func(t *testing.T) {
			h := newEngineHarness(t, "Unchanged sub call "+loop)
			child := "unchanged-sub-call-child-" + loop
			deployReviewChild(t, h, child, true)
			def := eachItem(h.projID, "unchanged-sub-call-"+loop, loop,
				[]*entities.Node{{ID: "call", Type: entities.CallActivity, Name: "Call",
					Properties: map[string]any{"called_process_key": child}}},
				oneStepInside("call"))
			r := startShape(t, h, def, threeItems)
			r.pin("entered")
			finished := map[uuid.UUID]bool{}
			for round := 0; round < 4; round++ {
				for _, childID := range r.called() {
					if finished[childID] {
						continue
					}
					finished[childID] = true
					r.pin(fmt.Sprintf("called process %d ends", len(finished)), r.completeOn(childID, "review"))
				}
			}
			r.pin("recorded", r.complete("record"))
		})
	}
}

// BPMN 2.0.2 §10.3.8, §13.3.7: a sub-process run once per item, with a service
// call inside it. The parallel one does not finish before the rule or after
// it: every call is made and one run is counted. Pinned as it is.
func TestARepeatingSubProcessWithAServiceCallInsideIsUnchanged(t *testing.T) {
	for _, loop := range bothLoops {
		t.Run(loop, func(t *testing.T) {
			api, calls := countingCalls(t, respondOK)
			h := newEngineHarness(t, "Unchanged sub job "+loop)
			def := eachItem(h.projID, "unchanged-sub-job-"+loop, loop,
				[]*entities.Node{{ID: "notify", Type: entities.ServiceTask, Name: "Notify",
					Properties: map[string]any{"http_url": api, "http_method": "POST"}}},
				oneStepInside("notify"))
			r := startShape(t, h, def, threeItems)
			r.calls = calls
			r.pin("entered")
			for i := range 4 {
				r.pin(fmt.Sprintf("job %d", i+1), r.runJob("notify"))
			}
			r.pin("recorded", r.complete("record"))
		})
	}
}

// BPMN 2.0.2 §10.3.8: a parallel sub-process run once per item with three
// steps inside, two items, the runs interleaved. One run is never counted,
// before the rule or after it. Pinned as it is.
func TestARepeatingSubProcessWithThreeStepsInsideIsUnchanged(t *testing.T) {
	h := newEngineHarness(t, "Unchanged sub three steps")
	def := eachItem(h.projID, "unchanged-sub-three-steps", "parallel",
		[]*entities.Node{
			{ID: "review", Type: entities.UserTask, Name: "Review"},
			{ID: "sign", Type: entities.UserTask, Name: "Sign"},
			{ID: "file", Type: entities.UserTask, Name: "File"},
		},
		[]*entities.SequenceFlow{
			{ID: "i1", SourceRef: "s_start", TargetRef: "review"},
			{ID: "i2", SourceRef: "review", TargetRef: "sign"},
			{ID: "i3", SourceRef: "sign", TargetRef: "file"},
			{ID: "i4", SourceRef: "file", TargetRef: "s_end"},
		})
	r := startShape(t, h, def, map[string]any{"items": []any{"a", "b"}})
	r.pin("entered")
	for i, step := range []string{"review", "sign", "review", "file", "sign", "file"} {
		r.pin(fmt.Sprintf("%d %s", i+1, step), r.complete(step))
	}
	r.pin("recorded", r.complete("record"))
}

// BPMN 2.0.2 §10.3.8: a sub-process given nothing to repeat over runs once.
func TestARepeatingSubProcessOverAnEmptyListIsUnchanged(t *testing.T) {
	for _, loop := range bothLoops {
		t.Run(loop, func(t *testing.T) {
			h := newEngineHarness(t, "Unchanged sub empty "+loop)
			def := eachItem(h.projID, "unchanged-sub-empty-"+loop, loop,
				[]*entities.Node{{ID: "review", Type: entities.UserTask, Name: "Review"}}, oneStepInside("review"))
			r := startShape(t, h, def, map[string]any{"items": []any{}})
			r.pin("entered")
			r.pin("review", r.complete("review"))
			r.pin("recorded", r.complete("record"))
		})
	}
}

// An instance that was already inside a repeating sub-process when the release
// changed carries on as it would have: the state is written here as the
// release before the rule left it — the sub-process holds no token of its own,
// the steps inside hold one per run — rather than reached through this
// release's own way in.
func TestAnInstanceAlreadyInsideARepeatingSubProcessIsUnchanged(t *testing.T) {
	t.Run("over three items", func(t *testing.T) {
		h := newEngineHarness(t, "Unchanged sub staged three")
		def := eachItem(h.projID, "unchanged-sub-staged-three", "parallel",
			[]*entities.Node{{ID: "review", Type: entities.UserTask, Name: "Review"}}, oneStepInside("review"))
		r := startShape(t, h, def, threeItems)
		r.stageAs(def, []string{"review", "review", "review"},
			map[string]entities.MultiInstanceState{"sub": {Total: 3}})
		r.pin("as the earlier release left it")
		for i := range 3 {
			r.pin(fmt.Sprintf("review %d", i+1), r.complete("review"))
		}
		r.pin("recorded", r.complete("record"))
	})
	t.Run("over an empty list", func(t *testing.T) {
		h := newEngineHarness(t, "Unchanged sub staged empty")
		def := eachItem(h.projID, "unchanged-sub-staged-empty", "parallel",
			[]*entities.Node{{ID: "review", Type: entities.UserTask, Name: "Review"}}, oneStepInside("review"))
		r := startShape(t, h, def, map[string]any{"items": []any{}})
		r.stageAs(def, []string{"review"}, nil)
		r.pin("as the earlier release left it")
		r.pin("review", r.complete("review"))
		r.pin("recorded", r.complete("record"))
	})
	t.Run("one at a time, second item open", func(t *testing.T) {
		h := newEngineHarness(t, "Unchanged sub staged sequential")
		def := eachItem(h.projID, "unchanged-sub-staged-sequential", "sequential",
			[]*entities.Node{{ID: "review", Type: entities.UserTask, Name: "Review"}}, oneStepInside("review"))
		r := startShape(t, h, def, threeItems)
		r.pin("review 1", r.complete("review"))
		r.stageAs(def, []string{"review"}, map[string]entities.MultiInstanceState{"sub": {Total: 3, Completed: 1}})
		r.pin("as the earlier release left it")
		r.pin("review 2", r.complete("review"))
		r.pin("review 3", r.complete("review"))
		r.pin("recorded", r.complete("record"))
	})
}

// BPMN 2.0.2 §13.3.2 (sequence flow): two tokens that reach one step without a
// join are two runs of it, and each is owed its own advance — here a step
// done by an outside worker, where each report moves the process on.
func TestAStepHoldingTwoTokensAdvancesOncePerReport(t *testing.T) {
	h := newEngineHarness(t, "Unchanged two tokens")
	topic := "unchanged-two-tokens"
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: topic,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "fork", Type: entities.ParallelGateway},
			{ID: "a", Type: entities.UserTask, Name: "A"},
			{ID: "b", Type: entities.UserTask, Name: "B"},
			{ID: "work", Type: entities.ServiceTask, Name: "Work", ExternalTopic: topic},
			{ID: "after", Type: entities.UserTask, Name: "After"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "fork"},
			{ID: "f2", SourceRef: "fork", TargetRef: "a"},
			{ID: "f3", SourceRef: "fork", TargetRef: "b"},
			{ID: "f4", SourceRef: "a", TargetRef: "work"},
			{ID: "f5", SourceRef: "b", TargetRef: "work"},
			{ID: "f6", SourceRef: "work", TargetRef: "after"},
			{ID: "f7", SourceRef: "after", TargetRef: "end"},
		},
	}
	r := startShape(t, h, def, nil)
	r.pin("a done", r.complete("a"))
	r.pin("b done", r.complete("b"))
	for i, task := range r.fetch(topic) {
		r.pin(fmt.Sprintf("report %d", i+1), r.report(task))
	}
	r.pin("after 1", r.complete("after"))
	r.pin("after 2", r.complete("after"))
}

// The same step, with the second worker giving up instead: the failure is the
// worker's to have raised, whatever the first report did to the step's tokens.
func TestAStepHoldingTwoTokensRaisesTheSecondWorkersFailure(t *testing.T) {
	h := newEngineHarness(t, "Unchanged two tokens failure")
	topic := "unchanged-two-tokens-failure"
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: topic,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "fork", Type: entities.ParallelGateway},
			{ID: "work", Type: entities.ServiceTask, Name: "Work", ExternalTopic: topic},
			{ID: "after", Type: entities.UserTask, Name: "After"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "fork"},
			{ID: "f2", SourceRef: "fork", TargetRef: "work"},
			{ID: "f3", SourceRef: "fork", TargetRef: "work"},
			{ID: "f6", SourceRef: "work", TargetRef: "after"},
			{ID: "f7", SourceRef: "after", TargetRef: "end"},
		},
	}
	r := startShape(t, h, def, nil)
	r.pin("both at work")
	fetched := r.fetch(topic)
	if len(fetched) != 2 {
		t.Fatalf("two pieces of work were expected and %d were fetched", len(fetched))
	}
	r.pin("report", r.report(fetched[0]))
	r.pin("give up", r.giveUp(fetched[1]))
}

// BPMN 2.0.2 §13.4.3 (boundary events): a deadline on a sub-process that is
// run once per item. The sub-process holds no token of its own while its runs
// are inside it, so the deadline does not apply — before the rule and after.
func TestADeadlineOnARepeatingSubProcessIsUnchanged(t *testing.T) {
	for name, properties := range map[string]map[string]any{
		"interrupting":     nil,
		"non-interrupting": {"non_interrupting": true},
	} {
		t.Run(name, func(t *testing.T) {
			h := newEngineHarness(t, "Unchanged sub deadline "+name)
			def := eachItem(h.projID, "unchanged-sub-deadline-"+name, "parallel",
				[]*entities.Node{{ID: "review", Type: entities.UserTask, Name: "Review"}}, oneStepInside("review"))
			withDeadline(def, "sub", properties)
			r := startShape(t, h, def, map[string]any{"items": []any{"a", "b"}})
			r.pin("entered")
			r.pin("the deadline comes due", r.runJob("deadline"))
			r.pin("review 1", r.complete("review"))
			r.pin("review 2", r.complete("review"))
			r.pin("chase", r.complete("chase"))
			r.pin("recorded", r.complete("record"))
		})
	}
}

// repeatedShapes are the ways a step that is not an approval repeats: over
// every item, or until a completion condition is met.
var repeatedShapes = []struct{ name, loop, condition string }{
	{"parallel", "parallel", ""},
	{"sequential", "sequential", ""},
	{"parallel until two", "parallel", "nrOfCompletedInstances >= 2"},
	{"sequential until two", "sequential", "nrOfCompletedInstances >= 2"},
}

func shapeKey(prefix, name string) string {
	return prefix + "-" + strings.ReplaceAll(name, " ", "-")
}

// BPMN 2.0.2 §10.3.8, §13.3.7: a step done by an outside worker, once per
// item — including what a report does after a completion condition has ended
// the step.
func TestARepeatingStepForAWorkerIsUnchanged(t *testing.T) {
	for _, shape := range repeatedShapes {
		t.Run(shape.name, func(t *testing.T) {
			h := newEngineHarness(t, "Unchanged external "+shape.name)
			topic := shapeKey("unchanged-external", shape.name)
			def := repeated(h.projID, topic, &entities.Node{Type: entities.ServiceTask, Name: "Check", ExternalTopic: topic,
				MultiInstanceType: shape.loop, Collection: "items", ElementVariable: "item",
				CompletionCondition: shape.condition})
			r := startShape(t, h, def, threeItems)
			r.pin("entered")
			reports := 0
			for round := 0; round < 4 && reports < 4; round++ {
				for _, task := range r.fetch(topic) {
					reports++
					r.pin(fmt.Sprintf("report %d", reports), r.report(task))
				}
			}
			r.pin("recorded", r.complete("record"))
			r.pin("recorded again", r.complete("record"))
		})
	}
}

// The same step ended by its completion condition, with the third worker
// giving up afterwards.
func TestAWorkerGivingUpAfterARepeatingStepEndedEarlyIsUnchanged(t *testing.T) {
	h := newEngineHarness(t, "Unchanged external give up")
	topic := "unchanged-external-give-up"
	def := repeated(h.projID, topic, &entities.Node{Type: entities.ServiceTask, Name: "Check", ExternalTopic: topic,
		MultiInstanceType: "parallel", Collection: "items", ElementVariable: "item",
		CompletionCondition: "nrOfCompletedInstances >= 2"})
	r := startShape(t, h, def, threeItems)
	fetched := r.fetch(topic)
	if len(fetched) != 3 {
		t.Fatalf("three pieces of work were expected and %d were fetched", len(fetched))
	}
	r.pin("report 1", r.report(fetched[0]))
	r.pin("report 2", r.report(fetched[1]))
	r.pin("give up", r.giveUp(fetched[2]))
}

// BPMN 2.0.2 §10.3.8, §13.3.7: a call activity run once per item — including
// what a called process does when it returns after a completion condition has
// ended the step.
func TestARepeatingCallActivityIsUnchanged(t *testing.T) {
	for _, shape := range repeatedShapes {
		t.Run(shape.name, func(t *testing.T) {
			h := newEngineHarness(t, "Unchanged call "+shape.name)
			child := shapeKey("unchanged-call-child", shape.name)
			deployReviewChild(t, h, child, true)
			def := repeated(h.projID, shapeKey("unchanged-call", shape.name), &entities.Node{Type: entities.CallActivity, Name: "Call",
				Properties:        map[string]any{"called_process_key": child},
				MultiInstanceType: shape.loop, Collection: "items", ElementVariable: "item",
				CompletionCondition: shape.condition})
			r := startShape(t, h, def, threeItems)
			r.pin("entered")
			finished := map[uuid.UUID]bool{}
			for round := 0; round < 4; round++ {
				for _, childID := range r.called() {
					if finished[childID] {
						continue
					}
					finished[childID] = true
					r.pin(fmt.Sprintf("called process %d ends", len(finished)), r.completeOn(childID, "review"))
				}
			}
			r.pin("recorded", r.complete("record"))
			r.pin("recorded again", r.complete("record"))
		})
	}
}

// A call activity run once per item whose called process ends at once, so
// every call returns while the step is still being started.
func TestARepeatingCallActivityWhoseCallsReturnAtOnceIsUnchanged(t *testing.T) {
	for _, shape := range repeatedShapes {
		t.Run(shape.name, func(t *testing.T) {
			h := newEngineHarness(t, "Unchanged quick call "+shape.name)
			child := shapeKey("unchanged-quick-call-child", shape.name)
			deployReviewChild(t, h, child, false)
			def := repeated(h.projID, shapeKey("unchanged-quick-call", shape.name), &entities.Node{Type: entities.CallActivity, Name: "Call",
				Properties:        map[string]any{"called_process_key": child},
				MultiInstanceType: shape.loop, Collection: "items", ElementVariable: "item",
				CompletionCondition: shape.condition})
			r := startShape(t, h, def, threeItems)
			r.pin("entered")
			r.pin("recorded", r.complete("record"))
			r.pin("recorded again", r.complete("record"))
		})
	}
}

// BPMN 2.0.2 §10.3.8, §13.3.7: a service call made once per item — including
// what the calls still queued do after a completion condition has ended the
// step.
func TestARepeatingServiceCallIsUnchanged(t *testing.T) {
	for _, shape := range repeatedShapes {
		t.Run(shape.name, func(t *testing.T) {
			api, calls := countingCalls(t, respondOK)
			h := newEngineHarness(t, "Unchanged job "+shape.name)
			def := repeated(h.projID, shapeKey("unchanged-job", shape.name), &entities.Node{Type: entities.ServiceTask, Name: "Notify",
				Properties:        map[string]any{"http_url": api, "http_method": "POST"},
				MultiInstanceType: shape.loop, Collection: "items", ElementVariable: "item",
				CompletionCondition: shape.condition})
			r := startShape(t, h, def, threeItems)
			r.calls = calls
			r.pin("entered")
			for i := range 4 {
				r.pin(fmt.Sprintf("job %d", i+1), r.runJob("step"))
			}
			r.pin("recorded", r.complete("record"))
			r.pin("recorded again", r.complete("record"))
		})
	}
}

// A service call made once per item, with a catch-all error path on the step,
// whose calls start failing after a completion condition has ended it.
func TestAFailedCallAfterARepeatingStepEndedEarlyIsUnchanged(t *testing.T) {
	var answered atomic.Int32
	api, calls := countingCalls(t, func(w http.ResponseWriter, r *http.Request) {
		if answered.Add(1) > 1 {
			http.Error(w, "the supplier's system is down", http.StatusInternalServerError)
			return
		}
		respondOK(w, r)
	})
	h := newEngineHarness(t, "Unchanged job failing")
	def := repeated(h.projID, "unchanged-job-failing", &entities.Node{Type: entities.ServiceTask, Name: "Ask for a quote",
		Properties:        map[string]any{"http_url": api, "http_method": "POST"},
		MultiInstanceType: "parallel", Collection: "items", ElementVariable: "item",
		CompletionCondition: "nrOfCompletedInstances >= 1"})
	def.Nodes = append(def.Nodes,
		&entities.Node{ID: "failed", Type: entities.BoundaryEvent, AttachedToRef: "step",
			Properties: map[string]any{"error_code": "*"}},
		&entities.Node{ID: "sort-out", Type: entities.UserTask, Name: "Sort it out"})
	def.Flows = append(def.Flows,
		&entities.SequenceFlow{ID: "e1", SourceRef: "failed", TargetRef: "sort-out"},
		&entities.SequenceFlow{ID: "e2", SourceRef: "sort-out", TargetRef: "end"})
	r := startShape(t, h, def, threeItems)
	r.calls = calls
	r.pin("entered")
	for i := range 5 {
		r.pin(fmt.Sprintf("job %d", i+1), r.runJob("step"))
	}
}

// A script run once per item finishes as it starts, so a completion condition
// can be met while the step is still starting its runs.
func TestARepeatingScriptIsUnchanged(t *testing.T) {
	for _, shape := range repeatedShapes {
		t.Run(shape.name, func(t *testing.T) {
			h := newEngineHarness(t, "Unchanged script "+shape.name)
			def := repeated(h.projID, shapeKey("unchanged-script", shape.name), &entities.Node{Type: entities.ScriptTask, Name: "Add up",
				Script:            `setVar("ran", ran + 1);`,
				MultiInstanceType: shape.loop, Collection: "items", ElementVariable: "item",
				CompletionCondition: shape.condition})
			items := map[string]any{"items": []any{"a", "b", "c"}, "ran": 0}
			r := startShape(t, h, def, items)
			instance, err := h.engine.GetInstance(h.Ctx(), r.id)
			if err != nil {
				t.Fatalf("reload instance: %v", err)
			}
			r.pin("entered", fmt.Sprintf("ran=%v", instance.Variables["ran"]))
			r.pin("recorded", r.complete("record"))
			r.pin("recorded again", r.complete("record"))
		})
	}
}

// BPMN 2.0.2 §13.4.3: an interrupting deadline on a step done by an outside
// worker — once, and once per item — and what the worker's report does when
// it arrives after the deadline.
func TestADeadlineOnAStepForAWorkerIsUnchanged(t *testing.T) {
	for _, loop := range []string{"", "parallel"} {
		name := "runs once"
		if loop != "" {
			name = loop
		}
		t.Run(name, func(t *testing.T) {
			h := newEngineHarness(t, "Unchanged external deadline "+name)
			topic := shapeKey("unchanged-external-deadline", name)
			def := repeated(h.projID, topic, &entities.Node{Type: entities.ServiceTask, Name: "Check", ExternalTopic: topic,
				MultiInstanceType: loop, Collection: "items", ElementVariable: "item"})
			withDeadline(def, "step", nil)
			r := startShape(t, h, def, threeItems)
			fetched := r.fetch(topic)
			r.pin("fetched")
			r.pin("the deadline comes due", r.runJob("deadline"))
			for i, task := range fetched {
				r.pin(fmt.Sprintf("late report %d", i+1), r.report(task))
			}
			r.pin("chase", r.complete("chase"))
		})
	}
}

// The same, with the worker giving up after the deadline.
func TestAWorkerGivingUpAfterADeadlineIsUnchanged(t *testing.T) {
	h := newEngineHarness(t, "Unchanged external deadline give up")
	topic := "unchanged-external-deadline-give-up"
	def := repeated(h.projID, topic, &entities.Node{Type: entities.ServiceTask, Name: "Check", ExternalTopic: topic})
	withDeadline(def, "step", nil)
	r := startShape(t, h, def, nil)
	fetched := r.fetch(topic)
	if len(fetched) != 1 {
		t.Fatalf("one piece of work was expected and %d were fetched", len(fetched))
	}
	r.pin("the deadline comes due", r.runJob("deadline"))
	r.pin("give up", r.giveUp(fetched[0]))
}

// BPMN 2.0.2 §13.4.3: an interrupting deadline on a service call — once, and
// once per item — whose job is still queued when the deadline comes due.
func TestADeadlineOnAServiceCallIsUnchanged(t *testing.T) {
	for _, loop := range []string{"", "parallel"} {
		for _, answer := range []string{"answers", "fails"} {
			name := "runs once, " + answer
			if loop != "" {
				name = loop + ", " + answer
			}
			t.Run(name, func(t *testing.T) {
				respond := respondOK
				if answer == "fails" {
					respond = func(w http.ResponseWriter, _ *http.Request) {
						http.Error(w, "the supplier's system is down", http.StatusInternalServerError)
					}
				}
				api, calls := countingCalls(t, respond)
				h := newEngineHarness(t, "Unchanged job deadline "+name)
				def := repeated(h.projID, shapeKey("unchanged-job-deadline", strings.ReplaceAll(name, ",", "")), &entities.Node{
					Type: entities.ServiceTask, Name: "Notify",
					Properties:        map[string]any{"http_url": api, "http_method": "POST"},
					MultiInstanceType: loop, Collection: "items", ElementVariable: "item"})
				withDeadline(def, "step", nil)
				r := startShape(t, h, def, threeItems)
				r.calls = calls
				r.pin("entered")
				r.pin("the deadline comes due", r.runJob("deadline"))
				for i := range 4 {
					r.pin(fmt.Sprintf("job %d", i+1), r.runJob("step"))
				}
				r.pin("chase", r.complete("chase"))
			})
		}
	}
}

// BPMN 2.0.2 §13.4.3: an interrupting deadline on a call activity — once, and
// once per item — and what the called process does when it ends after the
// deadline.
func TestADeadlineOnACallActivityIsUnchanged(t *testing.T) {
	for _, loop := range []string{"", "parallel"} {
		name := "runs once"
		if loop != "" {
			name = loop
		}
		t.Run(name, func(t *testing.T) {
			h := newEngineHarness(t, "Unchanged call deadline "+name)
			child := shapeKey("unchanged-call-deadline-child", name)
			deployReviewChild(t, h, child, true)
			def := repeated(h.projID, shapeKey("unchanged-call-deadline", name), &entities.Node{Type: entities.CallActivity, Name: "Call",
				Properties:        map[string]any{"called_process_key": child},
				MultiInstanceType: loop, Collection: "items", ElementVariable: "item"})
			withDeadline(def, "step", nil)
			r := startShape(t, h, def, threeItems)
			r.pin("entered")
			r.pin("the deadline comes due", r.runJob("deadline"))
			for i, childID := range r.called() {
				r.pin(fmt.Sprintf("called process %d ends late", i+1), r.completeOn(childID, "review"))
			}
			r.pin("chase", r.complete("chase"))
		})
	}
}

// A worker's report is refused when the worker does not hold the lock, in the
// words and with the standing it had before.
func TestAReportWithoutTheLockIsAnsweredAsBefore(t *testing.T) {
	h := newEngineHarness(t, "Unchanged lock refusals")
	topic := "unchanged-lock-refusals"
	def := repeated(h.projID, topic, &entities.Node{Type: entities.ServiceTask, Name: "Check", ExternalTopic: topic})
	r := startShape(t, h, def, nil)
	fetched := r.fetch(topic)
	if len(fetched) != 1 {
		t.Fatalf("one piece of work was expected and %d were fetched", len(fetched))
	}
	ctx := h.Ctx()
	r.pin("another worker reports", plain(h.svc.Complete(ctx, fetched[0].ID, "worker-2", nil)))
	r.pin("another worker gives up", plain(h.svc.HandleFailure(ctx, fetched[0].ID, "worker-2", "no", "", 0, 0)))
	r.pin("a report on work that does not exist", plain(h.svc.Complete(ctx, uuid.New(), "worker-1", nil)))
	if err := h.db.Exec(`UPDATE external_tasks SET lock_expiration = now() - interval '1 minute' WHERE id = ?`,
		fetched[0].ID).Error; err != nil {
		t.Fatalf("let the lock run out: %v", err)
	}
	r.pin("a report after the lock ran out", r.report(fetched[0]))
}

// BPMN 2.0.2 §13.4.3: an interrupting deadline on a service call that is a
// step of an ad-hoc sub-process, while the sub-process is still open. The
// sub-process has not finished, so it has withdrawn nothing: the call still
// queued is made, and a failing one is retried and raised, as for a service
// call anywhere else.
func TestADeadlineOnAServiceCallInsideAnOpenAdHocSubProcessIsUnchanged(t *testing.T) {
	for _, answer := range []string{"answers", "fails"} {
		t.Run(answer, func(t *testing.T) {
			respond := respondOK
			if answer == "fails" {
				respond = func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "the insurer's system is down", http.StatusInternalServerError)
				}
			}
			api, calls := countingCalls(t, respond)
			h := newEngineHarness(t, "Unchanged ad-hoc job deadline "+answer)
			def := &entities.ProcessDefinition{
				Project: &entities.Project{ID: h.projID}, Key: "unchanged-adhoc-job-deadline-" + answer,
				Nodes: []*entities.Node{
					{ID: "start", Type: entities.StartEvent},
					{ID: "research", Type: entities.SubProcess, Name: "Research the claim", IsAdHoc: true,
						CompletionCondition: "done >= 1",
						Nodes: []*entities.Node{
							{ID: "notify", Type: entities.ServiceTask, Name: "Notify the insurer", ParentID: "research",
								Properties: map[string]any{"http_url": api, "http_method": "POST"}},
							{ID: "deadline", Type: entities.BoundaryEvent, AttachedToRef: "notify", ParentID: "research",
								Properties: map[string]any{"timer_duration": "P7D"}},
							{ID: "chase", Type: entities.UserTask, Name: "Chase it", ParentID: "research"},
						}},
					{ID: "record", Type: entities.UserTask, Name: "Record the outcome"},
					{ID: "end", Type: entities.EndEvent},
				},
				Flows: []*entities.SequenceFlow{
					{ID: "f1", SourceRef: "start", TargetRef: "research"},
					{ID: "f2", SourceRef: "research", TargetRef: "record"},
					{ID: "f3", SourceRef: "record", TargetRef: "end"},
					{ID: "d1", SourceRef: "deadline", TargetRef: "chase"},
				},
			}
			r := startShape(t, h, def, map[string]any{"done": 0})
			r.calls = calls
			r.pin("the step is started", plain(h.svc.ActivateTask(h.Ctx(), r.id, "research", "notify")))
			r.pin("the deadline comes due", r.runJob("deadline"))
			for i := range 4 {
				r.pin(fmt.Sprintf("job %d", i+1), r.runJob("notify"))
			}
		})
	}
}

// A step done by an outside worker once per item, ended by a completion
// condition written on what a worker reported rather than on how many have.
// The work of the other runs stays on offer, and a second report that
// satisfies the condition moves the process on a second time. Pinned as it is.
func TestARepeatingStepForAWorkerEndedByWhatWasReportedIsUnchanged(t *testing.T) {
	h := newEngineHarness(t, "Unchanged external verdict")
	topic := "unchanged-external-verdict"
	def := repeated(h.projID, topic, &entities.Node{Type: entities.ServiceTask, Name: "Check", ExternalTopic: topic,
		MultiInstanceType: "parallel", Collection: "items", ElementVariable: "item",
		CompletionCondition: `verdict = "reject"`})
	r := startShape(t, h, def, threeItems)
	fetched := r.fetch(topic)
	if len(fetched) != 3 {
		t.Fatalf("three pieces of work were expected and %d were fetched", len(fetched))
	}
	for i, task := range fetched {
		outcome := plain(h.svc.Complete(h.Ctx(), task.ID, "worker-1", map[string]any{"verdict": "reject"}))
		r.pin(fmt.Sprintf("rejection %d", i+1), outcome)
	}
}
