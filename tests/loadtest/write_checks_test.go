package loadtest

import (
	"cmp"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/models"
)

// The checks TestConcurrentApprovalsLoseNothingAndRepeatNothing holds a run
// to. Each collects every problem it finds and reports the first few with the
// count, so two hundred broken instances read as one pattern.

// requireEveryStart stops the test if an instance could not be started: what
// follows is measured on the instances that exist.
func requireEveryStart(t *testing.T, starts []apiResult) {
	t.Helper()
	var failed []apiResult
	for _, s := range starts {
		if s.err != nil || s.status != http.StatusOK {
			failed = append(failed, s)
		}
	}
	if len(failed) > 0 {
		t.Fatalf("%d of %d starts failed under load: %s", len(failed), len(starts), summarize(failed))
	}
}

// checkCompletions holds every task to exactly one successful completion, and
// every other answer to a refusal of a submission that arrived twice. It returns
// the successful answers, which are what the latency describes, and how many
// second submissions were refused.
func checkCompletions(t *testing.T, approvals []approval, completions []completion) ([]apiResult, int) {
	t.Helper()
	sent := map[uuid.UUID]int{}
	for _, c := range completions {
		sent[c.taskID]++
	}
	succeeded := map[uuid.UUID]int{}
	var ok, failed []apiResult
	refused := 0
	for _, c := range completions {
		switch {
		case c.err == nil && c.status == http.StatusOK:
			succeeded[c.taskID]++
			ok = append(ok, c.apiResult)
		case sent[c.taskID] > 1 && isRefusal(c.apiResult):
			refused++ // the other half of a double submission, told the task is done
		default:
			failed = append(failed, c.apiResult)
		}
	}
	reportFailedCompletions(t, failed, len(completions))

	var problems []string
	for _, a := range approvals {
		for _, task := range []uuid.UUID{a.finance, a.legal} {
			if n := succeeded[task]; n != 1 {
				problems = append(problems, fmt.Sprintf("%s: task %s was completed %d times, sent %d times", a.order, task, n, sent[task]))
			}
		}
	}
	reportProblems(t, "every task completed exactly once", problems)
	return ok, refused
}

// isRefusal reports whether an answer says, as the caller's business, that the
// task cannot be completed — which is right for the second of two submissions.
// 400 is what the task service answers for a task that is already done, and 409
// is what the inbox's offline queue also reads as a refusal
// (ui/src/domain/outbox.ts): both tell a client not to send it again. A 5xx
// tells it to.
func isRefusal(r apiResult) bool {
	return r.err == nil && (r.status == http.StatusBadRequest || r.status == http.StatusConflict)
}

// reportFailedCompletions fails on any completion that neither succeeded nor
// was rightly refused, and names the concurrency failures among them.
//
// The engine serializes the work on one instance with a row lock on it, at
// READ COMMITTED, and nothing retries a transaction: a serialization failure
// should be impossible and a deadlock means two paths took their locks in
// different orders. Either reaches the caller as a 500 for work that was never
// done, so both are failures here, not noise to retry past.
func reportFailedCompletions(t *testing.T, failed []apiResult, sent int) {
	t.Helper()
	if len(failed) == 0 {
		return
	}
	var deadlocks, serialization int
	for _, f := range failed {
		text := strings.ToLower(string(f.body))
		switch {
		case strings.Contains(text, "deadlock"):
			deadlocks++
		case strings.Contains(text, "serializ"):
			serialization++
		}
	}
	t.Errorf("%d of %d completion requests failed (%d deadlocks, %d serialization failures): %s",
		len(failed), sent, deadlocks, serialization, summarize(failed))
}

// checkInstances holds every instance to having finished once, with both
// approvals' data and the partner's answer, and nothing left waiting.
func (w *writeLoad) checkInstances(t *testing.T, approvals []approval) {
	t.Helper()
	tasks := w.tasksByInstance(t)
	var problems []string
	for _, a := range approvals {
		problems = append(problems, w.instanceProblems(t, a)...)
		problems = append(problems, taskProblems(a, tasks[a.instance])...)
		problems = append(problems, w.auditProblems(t, a)...)
	}
	reportProblems(t, "every instance finished exactly once", problems)
}

func (w *writeLoad) instanceProblems(t *testing.T, a approval) []string {
	t.Helper()
	instance, err := w.svc.GetInstance(w.ctx, a.instance)
	if err != nil {
		return []string{fmt.Sprintf("%s: could not be read: %v", a.order, err)}
	}
	var problems []string
	if instance.Status != entities.ProcessCompleted {
		problems = append(problems, fmt.Sprintf("%s is %s, waiting at %v", a.order, instance.Status, tokenNodes(instance)))
	}
	if waiting := len(instance.GetTokensByNode(&entities.Node{ID: joinNode})); waiting > 0 || instance.JoinArrivals(joinNode) > 0 {
		problems = append(problems, fmt.Sprintf("%s was left at the join: %d tokens there, %d arrivals counted",
			a.order, waiting, instance.JoinArrivals(joinNode)))
	}
	for _, name := range []string{financeNode + "_approved", legalNode + "_approved"} {
		if instance.Variables[name] != true {
			problems = append(problems, fmt.Sprintf("%s lost %s: an approval's update was overwritten", a.order, name))
		}
	}
	if got := instance.Variables["receipt"]; got != receiptFor(a.order) {
		problems = append(problems, fmt.Sprintf("%s holds receipt %v, want %s", a.order, got, receiptFor(a.order)))
	}
	return problems
}

func tokenNodes(instance entities.ProcessInstance) []string {
	nodes := make([]string, 0, len(instance.Tokens))
	for _, token := range instance.Tokens {
		if token.Node != nil {
			nodes = append(nodes, token.Node.ID)
		}
	}
	return nodes
}

func taskProblems(a approval, tasks []taskRow) []string {
	if len(tasks) != 2 {
		return []string{fmt.Sprintf("%s has %d tasks, want its two approvals", a.order, len(tasks))}
	}
	var problems []string
	for _, task := range tasks {
		if task.Status != string(entities.TaskCompleted) {
			problems = append(problems, fmt.Sprintf("%s: the %s task is %s", a.order, task.NodeID, task.Status))
		}
	}
	return problems
}

// auditProblems reads the trail an auditor would: each approval recorded once,
// and the process concluded once.
func (w *writeLoad) auditProblems(t *testing.T, a approval) []string {
	t.Helper()
	entries, err := w.svc.GetAuditLogs(w.ctx, a.instance)
	if err != nil {
		return []string{fmt.Sprintf("%s: the audit trail could not be read: %v", a.order, err)}
	}
	recorded := map[string]int{}
	for _, entry := range entries {
		switch {
		case entry.Type == serviceimpl.EventTaskCompleted && entry.Node != nil:
			recorded[entry.Node.ID]++
		case entry.Type == entities.EventProcessCompleted:
			recorded[entry.Type]++
		}
	}
	var problems []string
	for _, what := range []string{financeNode, legalNode, entities.EventProcessCompleted} {
		if recorded[what] != 1 {
			problems = append(problems, fmt.Sprintf("%s: the audit trail records %s %d times", a.order, what, recorded[what]))
		}
	}
	return problems
}

// jobRow and serviceCallRow are read straight from the tables, because the
// question is what the queue and the call ledger hold, not what an API makes of
// them.
type jobRow struct {
	InstanceID uuid.UUID `gorm:"column:instance_id"`
	Status     string    `gorm:"column:status"`
	Retries    int       `gorm:"column:retries"`
	LastError  *string   `gorm:"column:last_error"`
}

type serviceCallRow struct {
	InstanceID uuid.UUID `gorm:"column:instance_id"`
	Status     string    `gorm:"column:status"`
	Attempts   int       `gorm:"column:attempts"`
}

// checkLedger holds each instance to one service-task job, run once, and one
// recorded partner call, made once.
func (w *writeLoad) checkLedger(t *testing.T, approvals []approval) {
	t.Helper()
	var jobs []jobRow
	if err := w.db.Raw(`SELECT instance_id, status, retries, last_error FROM jobs`).Scan(&jobs).Error; err != nil {
		t.Fatalf("read the jobs: %v", err)
	}
	var calls []serviceCallRow
	if err := w.db.Raw(`SELECT instance_id, status, attempts FROM service_calls`).Scan(&calls).Error; err != nil {
		t.Fatalf("read the service calls: %v", err)
	}
	jobsOf, callsOf := map[uuid.UUID][]jobRow{}, map[uuid.UUID][]serviceCallRow{}
	for _, j := range jobs {
		jobsOf[j.InstanceID] = append(jobsOf[j.InstanceID], j)
	}
	for _, c := range calls {
		callsOf[c.InstanceID] = append(callsOf[c.InstanceID], c)
	}

	var problems []string
	for _, a := range approvals {
		problems = append(problems, ledgerProblems(a, jobsOf[a.instance], callsOf[a.instance])...)
	}
	reportProblems(t, "one job and one partner call per instance", problems)
}

func ledgerProblems(a approval, jobs []jobRow, calls []serviceCallRow) []string {
	var problems []string
	if len(jobs) != 1 {
		problems = append(problems, fmt.Sprintf("%s has %d jobs, want one service task", a.order, len(jobs)))
	}
	for _, j := range jobs {
		if j.Status != string(models.JobCompleted) || j.Retries != 0 {
			problems = append(problems, fmt.Sprintf("%s: its job is %s after %d retries (last error %q)",
				a.order, j.Status, j.Retries, valueOf(j.LastError)))
		}
	}
	if len(calls) != 1 {
		problems = append(problems, fmt.Sprintf("%s has %d recorded partner calls, want one", a.order, len(calls)))
	}
	for _, c := range calls {
		if c.Status != models.ServiceCallCompleted || c.Attempts != 1 {
			problems = append(problems, fmt.Sprintf("%s: its partner call is %s after %d attempts", a.order, c.Status, c.Attempts))
		}
	}
	return problems
}

func valueOf(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// checkPartner holds the outside world to one call per instance, and no key used
// for two of them.
func checkPartner(t *testing.T, approvals []approval, calls map[string][]string) {
	t.Helper()
	var problems []string
	owner := map[string]string{}
	for _, a := range approvals {
		keys := calls[a.order]
		if len(keys) != 1 {
			problems = append(problems, fmt.Sprintf("%s: the partner was called %d times", a.order, len(keys)))
		}
		for _, key := range keys {
			if other, seen := owner[key]; seen && other != a.order {
				problems = append(problems, fmt.Sprintf("%s and %s were sent under one idempotency key", other, a.order))
			}
			owner[key] = a.order
		}
	}
	if len(calls) != len(approvals) {
		problems = append(problems, fmt.Sprintf("the partner heard about %d orders, want %d", len(calls), len(approvals)))
	}
	reportProblems(t, "the partner told once per instance", problems)
}

// reportProblems fails with the first few problems and the count of the rest.
func reportProblems(t *testing.T, property string, problems []string) {
	t.Helper()
	if len(problems) == 0 {
		return
	}
	shown := problems[:min(len(problems), problemsShown)]
	t.Errorf("%s: %d problems, first %d:\n  %s", property, len(problems), len(shown), strings.Join(shown, "\n  "))
}

// identifiers matches the ids an answer quotes, so answers that differ only in
// which task they are about are counted as one.
var identifiers = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// summarize groups failed answers by what they said, most frequent first.
func summarize(results []apiResult) string {
	counts := map[string]int{}
	for _, r := range results {
		counts[identifiers.ReplaceAllString(r.String(), "<id>")]++
	}
	answers := slices.SortedFunc(maps.Keys(counts), func(a, b string) int {
		return cmp.Or(counts[b]-counts[a], strings.Compare(a, b))
	})
	lines := make([]string, 0, min(len(answers), problemsShown))
	for _, answer := range answers[:min(len(answers), problemsShown)] {
		lines = append(lines, fmt.Sprintf("%d× %s", counts[answer], answer))
	}
	return "\n  " + strings.Join(lines, "\n  ")
}

// timesOf is the latencies of a set of answers, sorted for the percentiles.
func timesOf(results []apiResult) report {
	latencies := make([]time.Duration, 0, len(results))
	for _, r := range results {
		latencies = append(latencies, r.elapsed)
	}
	slices.Sort(latencies)
	return report{latencies: latencies, n: len(latencies)}
}
