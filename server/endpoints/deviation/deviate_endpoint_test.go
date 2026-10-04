package deviation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
)

// askedToDeviate is a facade that only deviates, and keeps the commands it
// was asked with. Anything else the endpoint called would panic.
type askedToDeviate struct {
	services.ServiceFacade
	commands []entities.DeviationCommand
	outcome  entities.DeviationOutcome
	err      error
}

func (s *askedToDeviate) DeviateInstance(_ context.Context, command entities.DeviationCommand) (entities.DeviationOutcome, error) {
	s.commands = append(s.commands, command)
	return s.outcome, s.err
}

// A.7. The endpoint asks the service for a preview unless the request says
// dry_run false: said nothing, and said true, are both a dry run.
func TestADeviationIsAPreviewUnlessTheRequestSaysDryRunFalse(t *testing.T) {
	t.Parallel()
	yes, no := true, false
	instance := uuid.Must(uuid.NewV7())
	for _, c := range []struct {
		name   string
		said   *bool
		dryRun bool
	}{{"nothing said", nil, true}, {"true", &yes, true}, {"false", &no, false}} {
		svc := &askedToDeviate{}
		reply, err := MakeDeviateInstanceEndpoint(svc)(context.Background(), DeviateInstanceRequest{
			InstanceID: instance.String(), Kind: "waive", NodeID: "step", Reason: "why",
			Outputs: map[string]any{"approved": true}, VisitKey: "dv1-k", DryRun: c.said,
		})
		if err != nil || reply.(DeviateInstanceResponse).Err != nil || len(svc.commands) != 1 {
			t.Fatalf("%s: %v, %+v, %d command(s)", c.name, err, reply, len(svc.commands))
		}
		got := svc.commands[0]
		if got.DryRun != c.dryRun {
			t.Errorf("dry_run %s: the service was asked with DryRun %v, want %v", c.name, got.DryRun, c.dryRun)
		}
		if got.InstanceID != instance || got.Kind != entities.DeviationWaive || got.NodeID != "step" || got.Reason != "why" ||
			got.VisitKey != "dv1-k" || len(got.Outputs) != 1 || got.Outputs["approved"] != true {
			t.Errorf("dry_run %s: the service was asked with %+v", c.name, got)
		}
	}
}

// An address that names no instance is the caller's to fix, and the service
// is not asked.
func TestADeviationOfAnIdThatIsNotOneIsRefusedBeforeTheServiceIsAsked(t *testing.T) {
	t.Parallel()
	svc := &askedToDeviate{}
	reply, err := MakeDeviateInstanceEndpoint(svc)(context.Background(), DeviateInstanceRequest{InstanceID: "not-an-id", Kind: "cancel"})
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	failed := reply.(DeviateInstanceResponse).Failed()
	if !errors.Is(failed, apierr.ErrInvalidArgument) || failed.Error() != `invalid argument: instance id "not-an-id" is not a valid identifier` || len(svc.commands) != 0 {
		t.Fatalf("answered %v with %d command(s) sent to the service", failed, len(svc.commands))
	}
}

// What the service refuses with is what the reply fails with, class and
// words: the transport takes the status from it, and from nothing else.
func TestADeviationTheServiceRefusesFailsWithTheServicesOwnError(t *testing.T) {
	t.Parallel()
	for name, refusal := range map[string]error{
		"an invalid argument": apierr.Invalidf("this instance has moved since you previewed it; preview again"),
		"forbidden":           apierr.Forbiddenf("only an administrator can waive, cancel or hold an instance"),
		"not found":           apierr.ErrNotFound,
		"the server's":        errors.New("waiving “Approve”: not found: decision \"x\" has no live version"),
	} {
		svc := &askedToDeviate{err: refusal, outcome: entities.DeviationOutcome{Applied: true}}
		reply, err := MakeDeviateInstanceEndpoint(svc)(context.Background(), DeviateInstanceRequest{InstanceID: uuid.NewString(), Kind: "waive"})
		if err != nil {
			t.Fatalf("%s: the endpoint itself failed: %v", name, err)
		}
		response := reply.(DeviateInstanceResponse)
		if response.Failed() != refusal { //nolint:errorlint // the very error, not one like it
			t.Errorf("%s: the reply fails with %v, want %v", name, response.Failed(), refusal)
		}
		if response.Applied || response.Deviation != nil {
			t.Errorf("%s: a refused request is answered as applied: %+v", name, response)
		}
	}
}

// The reply has one shape whatever the plan holds: every list a list, the
// outputs an object, and no record on a preview.
func TestAPlanWithNothingInItIsWrittenWithEmptyListsNotNull(t *testing.T) {
	t.Parallel()
	body, err := json.Marshal(responseOf(entities.DeviationOutcome{Plan: entities.DeviationPlan{
		Kind: entities.DeviationCancel, Scope: entities.DeviationScopeInstance,
		DecisionPoints: []entities.DecisionPoint{{NodeID: "g", NodeName: "Check the supplier", Kind: entities.DecisionPointCalledProcess}},
	}}))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(string(body), "null") {
		t.Errorf("the reply holds null: %s", body)
	}
	for _, want := range []string{`"open_work":[]`, `"outputs":{}`, `"missing":[]`, `"called_instances":[]`, `"refusals":[]`, `"warnings":[]`,
		`"reads":[]`, `"supplied":[]`, `"applicable":true`, `"applied":false`, `"replayed":false`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the reply has no %s: %s", want, body)
		}
	}
	// The plan names no step, so it carries neither field for one: the only
	// step named is the decision point's.
	for _, absent := range []string{`"deviation"`, `"node_id":""`, `"node_name":""`, `"err"`, `"instance_id":"0000`, `"scope":"instance","node_id"`} {
		if strings.Contains(string(body), absent) {
			t.Errorf("the reply carries %s: %s", absent, body)
		}
	}
}

// A plan that refuses is not applicable, and says so in a field of its own:
// a client asks that, not whether a list is empty.
func TestAPlanViewSaysWhetherItCanBeAppliedAndCountsWhatItDoesNotList(t *testing.T) {
	t.Parallel()
	task := uuid.Must(uuid.NewV7())
	view := PlanViewOf(entities.DeviationPlan{
		Kind: entities.DeviationWaive, Scope: entities.DeviationScopeTask, NodeID: "step", NodeName: "Approve", VisitKey: "dv1-k",
		OpenWork:      []entities.DeviationOpenWork{{TaskID: task, Name: "Approve", NodeID: "step", NodeName: "Approve", Status: entities.TaskClaimed, Assignee: "alice", IterationID: "2"}},
		OpenWorkInAll: 300, DecisionPointsInAll: 120, Missing: []string{"amount"}, MissingInAll: 60,
		DecisionPoints: []entities.DecisionPoint{{NodeID: "g", NodeName: "Large?", Kind: entities.DecisionPointGateway,
			Reads: []string{"amount"}, ReadsInAll: 12, Missing: []string{"amount"}, MissingInAll: 11, HasDefaultFlow: true, Analysed: true}},
		Refusals: []string{"no"}, Warnings: []string{"careful"}, RequiresSecondApprover: true,
	})
	body, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := `{"instance_id":"","kind":"waive","scope":"task","node_id":"step","node_name":"Approve","visit_key":"dv1-k",` +
		`"open_work":[{"task_id":"` + task.String() + `","name":"Approve","node_id":"step","node_name":"Approve","status":"claimed","assignee":"alice","iteration_id":"2"}],` +
		`"open_work_in_all":300,"outputs":{},` +
		`"decision_points":[{"node_id":"g","node_name":"Large?","kind":"gateway","reads":["amount"],"reads_in_all":12,"supplied":[],"missing":["amount"],"missing_in_all":11,"has_default_flow":true,"analysed":true}],` +
		`"decision_points_in_all":120,"missing":["amount"],"missing_in_all":60,"called_instances":[],"requires_second_approver":true,` +
		`"refusals":["no"],"warnings":["careful"],"applicable":false}`
	if string(body) != want {
		t.Errorf("the plan is written as\n%s\nwant\n%s", body, want)
	}
}
