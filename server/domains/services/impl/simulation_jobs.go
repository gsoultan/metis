package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceContracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// The job service a simulation runs with, in place of the real one.
//
// This is the seam that makes a simulation safe. The real job service is what
// calls connectors, makes outbound HTTP requests and waits on wall-clock
// timers; replacing it means none of that can happen, rather than meaning it
// happens and is then rolled back — which is not a thing a rollback can do to
// an HTTP request.
//
// It is modelled on tests/testutils.SynchronousJobService, and deliberately
// fixes that double's two faults:
//
//   - It never sleeps. The test double calls time.Sleep for a timer's duration,
//     which is fine for a PT1S test and unusable for the PT24H an SLA is written
//     in. Here a timer moves a virtual clock and the run carries on immediately.
//   - It never swallows an incident. The double returns (nil, nil) from
//     ListIncidents. A simulation that hides a refusal certifies a model that
//     will stop in production, which is worse than having no simulation.
//
// Built as a literal rather than through a constructor because the engine it
// drives is built *from* it — the handler factory needs the job service, and
// the job service needs somewhere to call Proceed. The simulation service fills
// engine and reader in immediately after building the engine; there is one
// caller and it is three lines away.
type simulationJobService struct {
	engine   serviceContracts.EngineRunner
	reader   serviceContracts.EngineReader
	request  *entities.SimulationRequest
	clock    *virtualClock
	recorder *simulationRecorder
}

// EnqueueServiceTask records that the process is waiting on something outside
// and stops there.
//
// It does not answer the task even when the request holds an answer for it: the
// runner's loop does that, so that a person's task and a service call are
// answered by one piece of code in one order. Two places advancing the same
// instance is how a simulator ends up with a trace that cannot be reproduced.
func (s *simulationJobService) EnqueueServiceTask(ctx context.Context, instance entities.ProcessInstance, node entities.Node, iterationID string) error {
	_, _ = ctx, iterationID

	s.recorder.Record(entities.SimulationStep{
		Node:  node.ID,
		Event: "waiting",
		Note:  fmt.Sprintf("Waiting on %s", nodeLabel(&node)),
	}, instance.Variables)

	return nil
}

// EnqueueTimer fast-forwards.
//
// A timer needs no answer — nobody has to say that a day passes — so this is
// the one enqueue that advances the instance on its own. The clock moves by the
// ISO-8601 expression the model actually carries, and the step says so, because
// a trace where a PT24H wait takes no visible time teaches a designer that
// their SLA is free.
func (s *simulationJobService) EnqueueTimer(ctx context.Context, instance entities.ProcessInstance, node entities.Node, duration string) error {
	elapsed, err := s.clock.AdvanceISO(duration)
	if err != nil {
		s.recorder.RecordIncident(
			node.ID,
			fmt.Sprintf("%s has a timer this engine cannot read: %q", nodeLabel(&node), duration),
			"BPMN timers are ISO-8601 — PT24H for a day, PT5M for five minutes.",
			instance.Variables,
		)
		return err
	}

	s.recorder.Record(entities.SimulationStep{
		Node:  node.ID,
		Event: "entered",
		Note:  fmt.Sprintf("Waited %s at %s", describeSimDuration(elapsed), nodeLabel(&node)),
	}, instance.Variables)

	def, err := s.reader.GetProcessDefinition(ctx, instance.Definition.ID)
	if err != nil {
		return err
	}
	return s.engine.Proceed(ctx, &instance, def, node.ID)
}

// EnqueueBoundaryTimer fires the timer only if it would go off before the
// activity it is attached to finishes.
//
// A running engine discovers which of those two happened first; a simulation
// can work it out, because the answer for the attached activity says how long
// it takes. So "Sarah takes a day, and there is a four-hour escalation on that
// task" resolves to the escalation firing — which is the case the boundary
// timer was drawn for and the one nobody ever gets to see before production.
//
// With no answer for the attached activity there is nothing to race, and the
// timer is recorded as scheduled rather than fired: guessing would invent a
// branch the author never asked for.
func (s *simulationJobService) EnqueueBoundaryTimer(ctx context.Context, instance entities.ProcessInstance, boundaryNode entities.Node, duration string) error {
	timerFor, ok := s.clock.PeekISO(duration)
	if !ok {
		s.recorder.RecordIncident(
			boundaryNode.ID,
			fmt.Sprintf("%s has a timer this engine cannot read: %q", nodeLabel(&boundaryNode), duration),
			"BPMN timers are ISO-8601 — PT4H for four hours.",
			instance.Variables,
		)
		return nil
	}

	answer, answered := s.request.AnswerFor(boundaryNode.AttachedToRef)
	activityFor, activityKnown := durationOfAnswer(s.clock, answer)

	if !answered || !activityKnown || timerFor >= activityFor {
		s.recorder.Record(entities.SimulationStep{
			Node:  boundaryNode.ID,
			Event: "entered",
			Note: fmt.Sprintf("A %s timer is set on %s",
				describeSimDuration(timerFor), boundaryNode.AttachedToRef),
		}, instance.Variables)
		return nil
	}

	if _, err := s.clock.AdvanceISO(duration); err != nil {
		return err
	}

	s.recorder.Record(entities.SimulationStep{
		Node:  boundaryNode.ID,
		Event: "entered",
		Note: fmt.Sprintf("After %s nobody had finished %s, so %s fired",
			describeSimDuration(timerFor), boundaryNode.AttachedToRef, nodeLabel(&boundaryNode)),
	}, instance.Variables)

	def, err := s.reader.GetProcessDefinition(ctx, instance.Definition.ID)
	if err != nil {
		return err
	}
	return s.engine.ExecuteNode(ctx, &instance, def, boundaryNode.ID)
}

// StartWorkers has nothing to start: every job this service takes is handled
// inline, so nothing is ever queued for a worker to find.
func (s *simulationJobService) StartWorkers(_ context.Context) {}

// StopWorkers has nothing to wait for, for the same reason.
func (s *simulationJobService) StopWorkers(_ context.Context) error { return nil }

// ProcessPendingJobs has nothing pending, for the same reason.
func (s *simulationJobService) ProcessPendingJobs(_ context.Context) error { return nil }

// ListIncidents reports what the run actually refused.
//
// Not nil. The test double this is modelled on returns nothing here, and a
// simulation that inherited that would report a process as healthy at the exact
// moment it proved the process stops.
func (s *simulationJobService) ListIncidents(_ context.Context, _ uuid.UUID) ([]entities.Incident, error) {
	_, incidents, _ := s.recorder.snapshot()
	out := make([]entities.Incident, 0, len(incidents))
	for _, incident := range incidents {
		out = append(out, entities.Incident{
			Error:     incident.Message,
			Status:    entities.IncidentOpen,
			CreatedAt: s.clock.Now(),
		})
	}
	return out, nil
}

// TryConnectorStep refuses. Trying a step runs it against the project's
// connection for real, and calling nothing outside itself is what a simulation
// is for.
func (s *simulationJobService) TryConnectorStep(_ context.Context, _ uuid.UUID, _ entities.Node, _ map[string]any) (map[string]any, error) {
	return nil, fmt.Errorf("a simulation does not try a step against a connection: it calls nothing outside itself")
}

// ResolveIncident refuses. A simulated incident is a finding about the model,
// and clearing it would be clearing the result.
func (s *simulationJobService) ResolveIncident(_ context.Context, _ uuid.UUID) error {
	return fmt.Errorf("a simulated incident cannot be resolved: it is a finding about the model, not a stuck instance")
}

// durationOfAnswer reports how long an answer says its step takes.
//
// Only a wait has a duration: a person taking a day, a message arriving in an
// hour. A service answer is instantaneous here, which is why it reports false
// rather than zero — zero would mean "finishes immediately and therefore beats
// every boundary timer", and that is a different claim.
func durationOfAnswer(clock *virtualClock, answer entities.SimulationAnswer) (time.Duration, bool) {
	switch answer.Kind {
	case entities.AnswerPerson:
		return clock.PeekISO(answer.After)
	case entities.AnswerMessage:
		return clock.PeekISO(answer.ArrivesAfter)
	default:
		return 0, false
	}
}

// describeSimDuration writes an elapsed span the way a process person says it.
//
// Days and hours, because that is what an SLA is written in. "1d 4h" rather
// than "28h0m0s", which is what Go's own formatting gives and which nobody
// reads as "longer than a day".
func describeSimDuration(d time.Duration) string {
	if d <= 0 {
		return "no time"
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}
