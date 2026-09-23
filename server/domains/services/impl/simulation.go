package impl

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observerContracts "github.com/gsoultan/metis/server/domains/observers/contracts"
	observerimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	serviceContracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
)

// errRollbackSimulation is returned from the unit of work to discard everything
// the run wrote.
//
// A sentinel rather than a flag, because the unit of work's contract is "an
// error rolls back" and a simulation wants exactly that behaviour for a reason
// that is not a failure. Every write the real engine made — tokens, tasks,
// jobs, incidents, audit rows, variable snapshots — is produced by the real
// code and then discarded, so the audit trail stays what §0 requires it to be:
// a record of what actually happened to real business commitments.
var errRollbackSimulation = errors.New("simulation finished; rolling back")

// SimulationEngineBuilder builds one isolated engine for one run.
//
// Supplied by the composition root rather than assembled here, because the
// engine's collaborators — task service, decision service, connectors, audit
// writer — are wired there and duplicating that wiring is how the simulated
// engine starts quietly differing from the real one.
type SimulationEngineBuilder func(
	dispatcher observerContracts.EventDispatcher,
	jobs serviceContracts.JobService,
) serviceContracts.ExecutionEngine

type simulationService struct {
	repo  repositories.Repository
	build SimulationEngineBuilder
}

// NewSimulationService runs definitions on the real engine and throws away the
// evidence.
func NewSimulationService(repo repositories.Repository, build SimulationEngineBuilder) serviceContracts.SimulationService {
	return &simulationService{repo: repo, build: build}
}

func (s *simulationService) Simulate(ctx context.Context, req entities.SimulationRequest) (entities.SimulationRun, error) {
	req.Normalize(time.Now())

	clock := newVirtualClock(req.ClockStart)
	recorder := newSimulationRecorder(clock, req.MaxSteps)

	// Its own dispatcher, carrying only the recorder. The real one fans events
	// out to SSE, notifications and webhook delivery; a simulation that reused
	// it would push a fictional process onto every browser watching the project
	// and post it to a partner.
	dispatcher := observerimpl.NewEventDispatcher()
	dispatcher.Register(recorder)

	jobs := &simulationJobService{request: &req, clock: clock, recorder: recorder}
	engine := s.build(dispatcher, jobs)
	jobs.engine = engine
	jobs.reader = engine

	var run entities.SimulationRun
	err := s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		run = s.drive(txCtx, engine, &req, clock, recorder)
		return errRollbackSimulation
	})

	if err != nil && !errors.Is(err, errRollbackSimulation) {
		return entities.SimulationRun{}, err
	}
	return run, nil
}

// drive starts the instance and answers it forward until it finishes, asks for
// something nobody answered, or runs out of budget.
//
// One loop, answering both a person's task and a service call, so the order a
// trace is produced in is decided in one place. Two pieces of code advancing
// the same instance is how a simulation stops being reproducible, and
// reproducibility is the whole basis of asserting on one in CI.
func (s *simulationService) drive(
	ctx context.Context,
	engine serviceContracts.ExecutionEngine,
	req *entities.SimulationRequest,
	clock *virtualClock,
	recorder *simulationRecorder,
) entities.SimulationRun {
	run := entities.SimulationRun{RunID: uuid.New()}
	run.Definition.Key = req.DefinitionKey
	run.Definition.Version = req.Version

	instanceID, err := engine.StartSubProcess(
		ctx, req.ProjectID, req.DefinitionKey, req.Version, maps.Clone(req.Variables), uuid.Nil, "",
	)
	if err != nil {
		recorder.RecordIncident("", fmt.Sprintf("The process could not start: %s", err), "", req.Variables)
		return s.finish(ctx, engine, run, clock, recorder, uuid.Nil)
	}

	for range req.MaxSteps {
		instance, err := engine.GetInstance(ctx, instanceID)
		if err != nil {
			recorder.RecordIncident("", fmt.Sprintf("The run could not be read back: %s", err), "", nil)
			break
		}
		if instance.Status != entities.ProcessActive {
			break
		}

		node, answer, found := nextAnswerable(instance, req)
		if !found {
			break
		}

		if err := s.apply(ctx, engine, &instance, node, answer, clock, recorder); err != nil {
			recorder.RecordIncident(
				node.ID,
				fmt.Sprintf("%s could not be answered: %s", nodeLabel(node), err),
				"",
				instance.Variables,
			)
			break
		}
	}

	return s.finish(ctx, engine, run, clock, recorder, instanceID)
}

// apply satisfies one step from the request's answers and moves the token on.
func (s *simulationService) apply(
	ctx context.Context,
	engine serviceContracts.ExecutionEngine,
	instance *entities.ProcessInstance,
	node *entities.Node,
	answer entities.SimulationAnswer,
	clock *virtualClock,
	recorder *simulationRecorder,
) error {
	def, err := engine.GetProcessDefinition(ctx, instance.Definition.ID)
	if err != nil {
		return err
	}

	switch answer.Kind {
	case entities.AnswerPerson:
		elapsed, err := clock.AdvanceISO(answer.After)
		if err != nil {
			return err
		}
		who := answer.Actor
		if who == "" {
			who = "Somebody"
		}
		recorder.Record(entities.SimulationStep{
			Node:  node.ID,
			Event: "left",
			Note:  fmt.Sprintf("%s completed %s after %s", who, nodeLabel(node), describeSimDuration(elapsed)),
		}, instance.Variables)
		return engine.Proceed(ctx, instance, def, node.ID)

	case entities.AnswerMessage:
		elapsed, err := clock.AdvanceISO(answer.ArrivesAfter)
		if err != nil {
			return err
		}
		recorder.Record(entities.SimulationStep{
			Node:  node.ID,
			Event: "left",
			Note:  fmt.Sprintf("The message %s was waiting for arrived after %s", nodeLabel(node), describeSimDuration(elapsed)),
		}, instance.Variables)
		return engine.Proceed(ctx, instance, def, node.ID)

	case entities.AnswerService:
		if answer.Fails != "" {
			return s.failService(ctx, engine, instance, def, node, answer, recorder)
		}
		if len(answer.Returns) > 0 {
			if instance.Variables == nil {
				instance.Variables = map[string]any{}
			}
			maps.Copy(instance.Variables, answer.Returns)
			if err := engine.UpdateInstance(ctx, *instance); err != nil {
				return err
			}
		}
		recorder.Record(entities.SimulationStep{
			Node:  node.ID,
			Event: "left",
			Note:  fmt.Sprintf("%s ran and came back", nodeLabel(node)),
		}, instance.Variables)
		return engine.Proceed(ctx, instance, def, node.ID)

	default:
		return fmt.Errorf("no idea how to answer a %q step", answer.Kind)
	}
}

// failService routes a simulated failure to the boundary event that catches it.
//
// This is the case a simulation is most worth having for: an error path nobody
// exercises, because exercising it in production means breaking something. If
// no boundary event matches, that *is* the finding — the process has no answer
// for this failure and would stop — so it is recorded as an incident rather
// than quietly carrying on.
func (s *simulationService) failService(
	ctx context.Context,
	engine serviceContracts.ExecutionEngine,
	instance *entities.ProcessInstance,
	def *entities.ProcessDefinition,
	node *entities.Node,
	answer entities.SimulationAnswer,
	recorder *simulationRecorder,
) error {
	boundary := matchingErrorBoundary(def, node.ID, answer.Fails)
	if boundary == nil {
		recorder.RecordIncident(
			node.ID,
			fmt.Sprintf("%s failed with %s and nothing catches it", nodeLabel(node), answer.Fails),
			fmt.Sprintf("Attach an error boundary event to %s, with error code %s or no code at all.",
				nodeLabel(node), answer.Fails),
			instance.Variables,
		)
		return nil
	}

	recorder.Record(entities.SimulationStep{
		Node:  node.ID,
		Event: "left",
		Note: fmt.Sprintf("%s failed with %s, caught by %s",
			nodeLabel(node), answer.Fails, nodeLabel(boundary)),
	}, instance.Variables)

	return engine.ExecuteNode(ctx, instance, def, boundary.ID)
}

// finish reads the outcome off the instance the run left behind.
func (s *simulationService) finish(
	ctx context.Context,
	engine serviceContracts.ExecutionEngine,
	run entities.SimulationRun,
	clock *virtualClock,
	recorder *simulationRecorder,
	instanceID uuid.UUID,
) entities.SimulationRun {
	steps, incidents, exhausted := recorder.snapshot()
	run.Steps = steps
	run.Incidents = incidents
	run.Variables = map[string]any{}

	if len(steps) > 0 {
		run.VirtualDurationMs = clock.Now().Sub(steps[0].Clock).Milliseconds()
	}

	if instanceID != uuid.Nil {
		if instance, err := engine.GetInstance(ctx, instanceID); err == nil {
			run.Variables = instance.Variables
			if instance.Definition != nil {
				run.Definition.ID = instance.Definition.ID
				run.Definition.Version = instance.Definition.Version
			}

			switch {
			case len(incidents) > 0:
				run.Outcome = entities.SimulationOutcomeIncident
			case exhausted:
				run.Outcome = entities.SimulationOutcomeStepBudget
			case instance.Status == entities.ProcessCompleted:
				run.Outcome = entities.SimulationOutcomeCompleted
				run.EndedAtNode = lastNode(steps)
			default:
				// Still active with nothing left to answer: the process is
				// asking for something. Not an error — this is the normal way a
				// case gets built, one question at a time.
				run.Outcome = entities.SimulationOutcomeNeedsAnswer
				if node := firstWaiting(instance); node != nil {
					run.AwaitingNode = node.ID
					// Resolved against the definition rather than read off the
					// token. The instance adapter rebuilds a token's *Node from
					// the row, and that copy carries the id without the name —
					// so trusting it asks "what happens at approve?" instead of
					// "what happens at Approve the expense?", which is the one
					// thing this question must not do.
					run.AwaitingLabel = labelFromDefinition(ctx, engine, instance, node)
				}
			}
			return run
		}
	}

	switch {
	case len(incidents) > 0:
		run.Outcome = entities.SimulationOutcomeIncident
	case exhausted:
		run.Outcome = entities.SimulationOutcomeStepBudget
	default:
		run.Outcome = entities.SimulationOutcomeNeedsAnswer
	}
	return run
}

// nextAnswerable finds the first token sitting on a step the request answers.
//
// In token order, which is the order the engine produced them, so a definition
// and a set of answers always yield the same trace. Picking by node id or by
// map iteration would make the run unreproducible, and an unreproducible run
// cannot be a CI gate.
func nextAnswerable(instance entities.ProcessInstance, req *entities.SimulationRequest) (*entities.Node, entities.SimulationAnswer, bool) {
	for _, token := range instance.Tokens {
		if token.Node == nil || token.Status != entities.TokenActive {
			continue
		}
		if answer, ok := req.AnswerFor(token.Node.ID); ok {
			return token.Node, answer, true
		}
	}
	return nil, entities.SimulationAnswer{}, false
}

// firstWaiting names the step to ask about. Token order again, for the same reason.
func firstWaiting(instance entities.ProcessInstance) *entities.Node {
	for _, token := range instance.Tokens {
		if token.Node != nil && token.Status == entities.TokenActive {
			return token.Node
		}
	}
	return nil
}

// matchingErrorBoundary finds the boundary event that catches a code.
//
// An empty ErrorCode on the boundary catches everything, which is what BPMN
// says and what entities.Node documents. An exact match wins over a catch-all,
// so a process with both a specific handler and a fallback exercises the one it
// would actually use.
func matchingErrorBoundary(def *entities.ProcessDefinition, attachedTo, code string) *entities.Node {
	var catchAll *entities.Node
	for _, node := range def.Nodes {
		if node == nil || node.Type != entities.BoundaryEvent || node.AttachedToRef != attachedTo {
			continue
		}
		if node.ErrorCode == code {
			return node
		}
		if node.ErrorCode == "" && catchAll == nil {
			catchAll = node
		}
	}
	return catchAll
}

// labelFromDefinition finds what the model calls a node, falling back to the id.
func labelFromDefinition(
	ctx context.Context,
	engine serviceContracts.ExecutionEngine,
	instance entities.ProcessInstance,
	node *entities.Node,
) string {
	if node.Name != "" {
		return node.Name
	}
	if instance.Definition == nil {
		return node.ID
	}
	def, err := engine.GetProcessDefinition(ctx, instance.Definition.ID)
	if err != nil || def == nil {
		return node.ID
	}
	for _, candidate := range def.Nodes {
		if candidate != nil && candidate.ID == node.ID && candidate.Name != "" {
			return candidate.Name
		}
	}
	return node.ID
}

func lastNode(steps []entities.SimulationStep) string {
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].Node != "" {
			return steps[i].Node
		}
	}
	return ""
}
