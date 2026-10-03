package impl

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
)

type migrationService struct {
	repo repositories.Repository
	// engine advances an instance past a node that is being skipped.
	//
	// Reused rather than reimplemented: advancing a token means cancelling
	// boundary timers, honouring multi-instance counts and evaluating the
	// gateway that follows. A second copy of that here would be a second set of
	// BPMN semantics, and the two would drift.
	//
	// nil in wirings that predate node actions; a skip is refused rather than
	// half-performed when it is missing.
	engine servicecontracts.ExecutionEngine
	// audit records that a migration happened. Without it the trail shows a task
	// completed on a node the instance was never started on, with nothing to
	// explain how it got there — and the OCEL export inherits that gap.
	audit servicecontracts.AuditWriter
	// ledger records each skip, cancel and hold, and each acknowledged control
	// loss, in the transaction that makes it. A ledger that cannot be written
	// refuses the decision rather than letting it happen unrecorded.
	ledger servicecontracts.DeviationRecorder
	// actions is what a skip, a cancel and a hold do once this service has
	// decided to do one. It decides nothing: each of them is asked for under
	// the instance lock, after the row that lock returned has answered that the
	// instance is still one the decision is about.
	actions nodeActions
}

// NewMigrationService creates a new MigrationService implementation.
func NewMigrationService(
	repo repositories.Repository,
	engine servicecontracts.ExecutionEngine,
) servicecontracts.MigrationService {
	// One ledger and one audit writer: the ones the node actions record
	// through are the ones the rest of a migration writes to.
	actions := newNodeActions(repo, engine)
	return &migrationService{
		repo: repo, engine: engine,
		audit: actions.audit, ledger: actions.ledger,
		actions: actions,
	}
}

// MigrateInstances moves running instances from one version of a process onto
// another, rewriting the nodes they are parked on through nodeMapping.
//
// The supported way to change version is still to promote a new one and let the
// old one drain, because an instance that finishes on the graph it started with
// cannot be broken by an edit. This exists for the case drain cannot serve:
// work already in flight on a version that must not continue — a queue whose
// assignee has left, a step a regulator has just prohibited.
//
// It refuses far more than it used to. The first version validated nothing: a
// mapping to a node the target did not have was accepted and applied, migrating
// across two unrelated process keys was accepted, and a token left on a node the
// target lacked hung the request forever the next time anybody advanced it.
// Everything below is checked *before* a single row is written, because a
// half-migrated instance is worse than a refused migration — the caller can retry
// a refusal, and cannot un-strand a token.
func (s *migrationService) MigrateInstances(ctx context.Context, sourceDefID uuid.UUID, targetDefID uuid.UUID, nodeMapping map[string]string, opts ...servicecontracts.MigrationOption) error {
	_, err := s.ApplyInstanceMigration(ctx, sourceDefID, targetDefID, nodeMapping, opts...)
	return err
}

// ApplyInstanceMigration is MigrateInstances, and says what the run did: how
// many instances it acted on, and which it left alone because they were no
// longer where the plan found them.
func (s *migrationService) ApplyInstanceMigration(ctx context.Context, sourceDefID uuid.UUID, targetDefID uuid.UUID, nodeMapping map[string]string, opts ...servicecontracts.MigrationOption) (entities.MigrationResult, error) {
	// Every refusal is worked out by the planner, so what a dry run showed and
	// what an apply does cannot disagree. Two copies of these checks is how a
	// preview comes to say "this is fine" about something the apply rejects.
	options := servicecontracts.ApplyMigrationOptions(opts)
	plan, covered, err := s.planFor(ctx, sourceDefID, targetDefID, nodeMapping, options)
	if err != nil {
		return entities.MigrationResult{}, err
	}
	if !plan.Applicable() {
		return entities.MigrationResult{}, apierr.Invalidf("%s", strings.Join(plan.Refusals, "; "))
	}
	if plan.Instances == 0 {
		return entities.MigrationResult{}, nil
	}
	return s.apply(ctx, sourceDefID, targetDefID, nodeMapping, options, plan, plannedFor(covered))
}

// PlanInstanceMigration works out what MigrateInstances would do, and writes
// nothing.
//
// Refusals are collected rather than returned one at a time: somebody fixing a
// node mapping wants the whole list, not to rediscover the next problem after
// each correction. Warnings are collected the same way but do not block — they
// are the things a human should look at before applying, not the things that
// would corrupt an instance.
func (s *migrationService) PlanInstanceMigration(ctx context.Context, sourceDefID uuid.UUID, targetDefID uuid.UUID, nodeMapping map[string]string, opts ...servicecontracts.MigrationOption) (entities.MigrationPlan, error) {
	plan, _, err := s.planFor(ctx, sourceDefID, targetDefID, nodeMapping, servicecontracts.ApplyMigrationOptions(opts))
	return plan, err
}

// planFor is PlanInstanceMigration, and also hands back the instances the plan
// was made for.
//
// The plan a caller is shown counts them; the apply needs to know which they
// were. Everything a plan establishes about an instance — that its work lands,
// which controls it loses and that somebody accepted the loss — is established
// for these and for no others, so an apply may act on these and on no others.
func (s *migrationService) planFor(
	ctx context.Context,
	sourceDefID, targetDefID uuid.UUID,
	nodeMapping map[string]string,
	options servicecontracts.MigrationOptions,
) (entities.MigrationPlan, []models.ProcessInstanceModel, error) {
	var plan entities.MigrationPlan
	if sourceDefID == targetDefID {
		return plan, nil, apierr.Invalidf("the source and target versions are the same definition")
	}

	source, err := s.repo.Definition().Get(ctx, sourceDefID)
	if err != nil {
		return plan, nil, fmt.Errorf("source definition: %w", err)
	}
	target, err := s.repo.Definition().Get(ctx, targetDefID)
	if err != nil {
		return plan, nil, fmt.Errorf("target definition: %w", err)
	}
	plan.SourceKey = source.Key
	plan.SourceVersion = source.Version
	plan.TargetVersion = target.Version
	plan.TargetID = targetDefID

	// Same process, or it is not a version change. Migrating "expense-approval"
	// onto "supplier-onboarding" was previously accepted and left every instance
	// claiming to be running a process it had never started.
	if source.Key != target.Key {
		return plan, nil, apierr.Invalidf("cannot migrate %q onto %q: they are different processes, not two versions of one",
			source.Key, target.Key)
	}
	if source.ProjectID != target.ProjectID {
		return plan, nil, apierr.Invalidf("cannot migrate between projects")
	}

	// Indexed over the whole tree, not just the top level: a node inside a
	// sub-process is a node the target has, and reading only the outer list made
	// every token parked in one look unlandable.
	sourceNodes := nodeIndex(source.Nodes)
	targetNodes := nodeIndex(target.Nodes)

	// A mapping that names a node the target does not have is a typo that would
	// park a token somewhere unreachable. Checked first because it is wrong
	// regardless of which instances happen to be running.
	var badTargets []string
	for from, to := range nodeMapping {
		if _, ok := targetNodes[to]; !ok {
			badTargets = append(badTargets, fmt.Sprintf("%s→%s", from, to))
		}
	}
	if len(badTargets) > 0 {
		slices.Sort(badTargets)
		// Raised rather than collected: a mapping that names a node the target
		// does not have is wrong on its face, independently of what is running,
		// so there is nothing further to plan.
		return plan, nil, apierr.Invalidf("version %d of %q has no node for %s",
			target.Version, target.Key, strings.Join(badTargets, ", "))
	}

	// A boundary event guards a particular activity. Moving it to one that
	// guards something else is the timer firing against work that is not
	// running — checked here because it depends only on the two graphs.
	plan.Refusals = append(plan.Refusals, boundaryRefusals(sourceNodes, targetNodes, nodeMapping)...)

	// Nodes whose work is decided rather than moved.
	plan.Refusals = append(plan.Refusals, s.actionRefusals(sourceNodes, targetNodes, nodeMapping, options.Actions, incomingFlows(source))...)
	plan.Actions = plannedActions(sourceNodes, options.Actions)

	// Which nodes the new version dropped altogether. Shown whether or not any
	// instance is sitting on one: it is the first thing somebody reviewing a
	// cutover wants to see, and a removed user task is a removed producer of
	// whatever its form used to write.
	plan.RemovedNodes = removedNodes(sourceNodes, targetNodes)

	instances, err := s.repo.Process().ListByDefinition(ctx, sourceDefID)
	if err != nil {
		return plan, nil, fmt.Errorf("failed to list instances for migration: %w", err)
	}
	instances, missing := selectInstances(instances, options.Instances)
	if len(missing) > 0 {
		// Named and not there is a refusal, not a silent omission: somebody who
		// listed twelve instances and had eleven moved would have no way to
		// find out which one did not.
		return plan, nil, apierr.Invalidf("version %d of %q is not running instance(s) %s",
			source.Version, source.Key, strings.Join(missing, ", "))
	}
	plan.Instances = len(instances)
	if len(instances) == 0 {
		return plan, nil, nil
	}

	// Every place the instances currently sit has to land on a node the target
	// actually has — mapped there explicitly, or carried over because the target
	// still has a node of that name. An unlisted one is exactly the token the
	// old code stranded.
	found, err := s.survey(ctx, instances, targetNodes, nodeMapping, options.Actions)
	if err != nil {
		return plan, nil, err
	}
	plan.Moves = found.moves
	if len(found.unlandable) > 0 {
		plan.Refusals = append(plan.Refusals, fmt.Sprintf(
			"version %d of %q has nowhere to put the work parked on %s; map each one to a node it does have",
			target.Version, target.Key, strings.Join(found.unlandable, ", "))+boundaryAdvice(sourceNodes, found.unlandable))
	}
	// Engine bookkeeping is keyed by node id just as tokens are, and it is the
	// half nobody sees. A join counter left behind is a gateway that waits for
	// branches that already arrived; a multi-instance counter left behind
	// re-asks everyone who already answered.
	if len(found.stateStranded) > 0 {
		plan.Refusals = append(plan.Refusals, fmt.Sprintf(
			"version %d of %q has nowhere to put the engine bookkeeping held for %s; "+
				"a join or multi-instance counter that does not land is a gateway that never completes",
			target.Version, target.Key, strings.Join(found.stateStranded, ", ")))
	}
	if len(found.stateCollisions) > 0 {
		plan.Refusals = append(plan.Refusals, fmt.Sprintf(
			"this mapping merges %s, and each side carries its own join or multi-instance counter; "+
				"there is no correct way to add two of those together, so map them to distinct nodes",
			strings.Join(found.stateCollisions, ", ")))
	}

	plan.Warnings = append(plan.Warnings, defaultFlowWarnings(target, targetNodes, found.landings)...)
	plan.Warnings = append(plan.Warnings, disarmedDutyWarnings(targetNodes)...)
	plan.Warnings = append(plan.Warnings, claimWarnings(found.moves)...)
	plan.Warnings = append(plan.Warnings, redirectWarnings(sourceNodes, nodeMapping, instances)...)

	// A step somebody marked as carrying a control obligation is not a step a
	// mapping may quietly drop. Held rather than refused outright: the answer is
	// sometimes yes — the approver has left, the regulator has just forbidden
	// the step — but it has to be somebody's answer, recorded with their name on
	// it, rather than a consequence of a node id nobody mapped.
	plan.ComplianceHolds = complianceHolds(sourceNodes, targetNodes, nodeMapping, instances)
	for _, hold := range plan.ComplianceHolds {
		if slices.Contains(options.Acknowledged, hold.NodeID) {
			continue
		}
		plan.Refusals = append(plan.Refusals, fmt.Sprintf(
			"%q is marked as carrying a control obligation and %d instance(s) have not passed it yet%s; "+
				"acknowledge it explicitly to migrate them without it",
			hold.NodeID, hold.Instances, noteSuffix(hold.Note)))
	}

	slices.Sort(plan.Refusals)
	slices.Sort(plan.Warnings)
	return plan, instances, nil
}

// complianceHolds finds the control-bearing steps this migration would take
// away from instances that have not performed them yet.
//
// Three populations arrive at a removed approval, and only one of them is a
// question: the instances that already passed it keep their record, the ones
// that never reach it were never going to, and these are the ones whose
// approval was pending when somebody deleted the step.
func complianceHolds(
	sourceNodes, targetNodes map[string]models.FlowNode,
	nodeMapping map[string]string,
	instances []models.ProcessInstanceModel,
) []entities.ComplianceHold {
	pending := map[string]int{}
	for id, node := range sourceNodes {
		if !boolProperty(node.Properties, "compliance_relevant") {
			continue
		}
		// The obligation survives only if it lands on a node that carries one
		// too. Existing is not enough: mapping "operations approve" onto "sales
		// approve" lands the token perfectly and still means nobody performs
		// the check, and a version that keeps the node id but drops the marking
		// has removed the control just as surely as deleting the node would.
		landed, ok := targetNodes[mapNode(nodeMapping, id)]
		if ok && boolProperty(landed.Properties, "compliance_relevant") {
			continue
		}
		for _, instance := range instances {
			// An instance that already performed the step waived nothing. Only
			// the ones whose approval was still pending are a question.
			if !slices.Contains(instance.CompletedNodes, id) {
				pending[id]++
			}
		}
	}

	holds := make([]entities.ComplianceHold, 0, len(pending))
	for id, count := range pending {
		if count == 0 {
			continue
		}
		holds = append(holds, entities.ComplianceHold{
			NodeID:    id,
			Name:      sourceNodes[id].Name,
			Note:      stringProperty(sourceNodes[id].Properties, "compliance_note"),
			Instances: count,
		})
	}
	slices.SortFunc(holds, func(a, b entities.ComplianceHold) int { return strings.Compare(a.NodeID, b.NodeID) })
	return holds
}

func noteSuffix(note string) string {
	if note == "" {
		return ""
	}
	return fmt.Sprintf(" (%s)", note)
}

// apply performs the migration. Every refusal has already been made by the
// planner, so this only writes.
//
// It reports what it did. The instances it leaves alone are the ones that, by
// the time it held their lock, were no longer where its listing found them,
// and the ones the plan was not made for (planned is the plan's own listing);
// they are in the result so the caller can be told, not only in the log.
//
// "Only writes" is true of an instance that stayed where the plan found it. The
// plan is made from one listing and the instances are then taken one at a time,
// so each is asked again where it stands: before its work is decided, and under
// the lock the rewrite holds (whyNotMoved). One that has moved onto work the
// new version cannot take is not rewritten.
func (s *migrationService) apply(
	ctx context.Context,
	sourceDefID, targetDefID uuid.UUID,
	nodeMapping map[string]string,
	options servicecontracts.MigrationOptions,
	plan entities.MigrationPlan,
	planned map[uuid.UUID]struct{},
) (entities.MigrationResult, error) {
	var result entities.MigrationResult
	source, err := s.repo.Definition().Get(ctx, sourceDefID)
	if err != nil {
		return result, fmt.Errorf("source definition: %w", err)
	}
	target, err := s.repo.Definition().Get(ctx, targetDefID)
	if err != nil {
		return result, fmt.Errorf("target definition: %w", err)
	}
	targetNodes := nodeIndex(target.Nodes)
	// The part of the mapping that only renames a step: what finished work
	// follows.
	renames := renamedSteps(nodeIndex(source.Nodes), nodeMapping)

	instances, err := s.repo.Process().ListByDefinition(ctx, sourceDefID)
	if err != nil {
		return result, fmt.Errorf("failed to list instances for migration: %w", err)
	}
	instances, _ = selectInstances(instances, options.Instances)

	// One id across every entry this run writes, so the trail can be read back
	// as "what did that migration do" rather than as unrelated events that
	// happen to share a timestamp.
	runID := uuid.New()
	done := 0

	for _, instance := range instances {
		// Anything that is not still running is already settled — migrated by
		// an earlier attempt, cancelled, or finished on its own. Skipping it is
		// what makes re-running a migration a resume rather than a second pass:
		// an instance that moved is no longer on the source version at all, and
		// one that was cancelled must not be cancelled again.
		if instance.Status != models.ProcessActive {
			continue
		}
		// Nor is one the plan never saw this run's to move. The plan is made
		// from one listing and this loop from a second, and an instance that
		// arrived on the source version in between — started there, or moved
		// there by another migration — was asked nothing: not whether its work
		// lands, and not whether it loses a control somebody has to accept.
		// Moved all the same, it lost that control with no acknowledgement
		// asked for and no row to say so.
		if _, covered := planned[uuid.UUID(instance.ID)]; !covered {
			result.PassedOver = append(result.PassedOver, passedOver(instance, notPlannedFor(source)))
			log.Info().Str("instance", uuid.UUID(instance.ID).String()).Str("run", runID.String()).
				Msg("A migration passed over an instance that was not on the source version when it was planned. " +
					"It stays on the version it is running; plan the migration again to include it")
			continue
		}

		// Work that is being decided rather than moved is settled first, and
		// an instance that was cancelled or finished by a skip is not migrated
		// at all: it will never run again, and its record should name the
		// version it actually ran on.
		made, err := s.decide(ctx, instance, sourceDefID, source, target, options, runID)
		if err != nil {
			return result, fmt.Errorf("%w (%d of %d instances had already been dealt with; "+
				"run the same migration again to carry on from here)", err, done, len(instances))
		}
		if made.wrote {
			result.Changed++
		}
		switch made.outcome {
		case outcomeDealtWith:
			done++
			continue
		case outcomeMovedOn:
			// Passed over as an instance that finished meanwhile is, further
			// down: not counted among those dealt with, and still on the
			// source version for the next run. Said in the result, for whoever
			// asked for the migration, and in the log.
			result.PassedOver = append(result.PassedOver, passedOver(instance, leftTheStep(source, made.left)))
			log.Info().Str("instance", uuid.UUID(instance.ID).String()).Str("run", runID.String()).
				Msg("A migration passed over an instance that was no longer where its listing found it. " +
					"It stays on the version it is running; run the same migration again to plan for where it is now")
			continue
		case outcomeMove:
		}

		moved := map[string]string{}
		settled, elsewhere := false, false
		// Why the instance, once locked, is not to be moved; empty when it is.
		stuck := ""
		// The instance_migrated entry is written after the rewrite commits, but
		// the control-loss rows written inside it point at that entry, so its id
		// is chosen first.
		entryID, err := uuid.NewV7()
		if err != nil {
			return result, err
		}
		var waived []entities.ComplianceHold
		err = s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
			// Hold the instance for the whole rewrite, and work from the row the
			// lock returns rather than the one the listing did.
			//
			// Taking the lock and then writing the copy read before it is worse
			// than not locking at all: a completion that commits in between is
			// overwritten wholesale, and the instance comes back to life with
			// the status and the tokens it had before somebody finished it.
			fresh, lockErr := s.repo.Process().GetForUpdate(txCtx, uuid.UUID(instance.ID))
			if lockErr != nil {
				return lockErr
			}
			// Is it still on the version this migration moves instances off?
			// Asked first, and of the locked row: a second run of the same
			// migration, started while the first was still working, listed the
			// instance on the old version and reaches it after the first has
			// moved it. Only the status used to be asked, so the mapping was
			// applied a second time, to wherever the instance then stood on
			// the new version.
			if !onVersion(fresh, sourceDefID) {
				elsewhere = true
				return nil
			}
			// And re-read the question the listing answered. An instance that
			// finished while this migration was working through the ones ahead
			// of it is not ours to move.
			if fresh.Status != models.ProcessActive {
				settled = true
				return nil
			}
			// And the question the plan answered from its listing: can what this
			// instance holds land on the new version? Asked of the locked row,
			// before anything is written. An instance that reached a step the
			// new version does not have, after it was listed, used to be
			// re-pointed all the same; its holder then completed a task nothing
			// follows, and the instance stayed active with no token for ever.
			why, checkErr := s.whyNotMoved(txCtx, fresh, source, target, targetNodes, nodeMapping, options.Actions)
			if checkErr != nil {
				return checkErr
			}
			if why != "" {
				stuck = why
				return nil
			}
			// Worked out from the locked row's own completed steps, before they
			// are re-pointed at the new graph: the ledger and the migration's
			// narrative then name the same losses.
			waived = controlsNotPassed(plan.ComplianceHolds, fresh.CompletedNodes)
			if err := s.recordControlLosses(txCtx, fresh, sourceDefID, waived, options, runID, entryID); err != nil {
				return err
			}
			instance = fresh
			instance.DefinitionID = models.UUID(targetDefID)
			for i := range instance.Tokens {
				instance.Tokens[i].NodeID = mapNode(nodeMapping, instance.Tokens[i].NodeID)
			}
			// Everything else on the instance that is keyed by node id. The
			// counters are live work and follow the mapping like the tokens
			// they count: Joins is what a parallel gateway counts arrived
			// branches in, and MultiInstance is how far through its items a
			// node is. Leaving them pointing at the old graph deadlocks the
			// join and re-asks the multi-instance assignees.
			//
			// The two lists are a record of work done, read by compensation
			// and by the question "has this instance passed that control".
			// They follow a rename only, as a finished task does. Following a
			// redirect recorded a step the instance had finished as the step
			// the mapping pointed at: with a finished step redirected onto a
			// control the instance was only waiting at, the control read as
			// passed, and a later migration that dropped it asked nobody.
			instance.CompletedNodes = mapNodeList(renames, instance.CompletedNodes)
			instance.CompensatedNodes = mapNodeList(renames, instance.CompensatedNodes)
			instance.MultiInstance = mapMultiInstance(nodeMapping, instance.MultiInstance)
			instance.Joins = mapJoins(nodeMapping, instance.Joins)
			if err := s.repo.Process().Update(txCtx, instance); err != nil {
				return err
			}

			tasks, err := s.repo.Task().ListByInstance(txCtx, uuid.UUID(instance.ID))
			if err != nil {
				return err
			}
			for _, task := range tasks {
				// Only work that is still somebody's to do follows the mapping.
				// A mapping says where work in progress goes; a task that was
				// completed or cancelled is the record of what happened, on the
				// version it happened on. Rebuilt from the step the mapping
				// names, as every task used to be, it came back claimed or
				// unclaimed: an approval already given was open work again, in
				// the name of whoever the new step is for, and nothing said any
				// longer who had given it.
				//
				// What a finished task may take is its step's new id, and only
				// where the mapping renames the step (renamedSteps): the work
				// was done on that step, and a rule of the new version — who
				// did this may not also do that — names it by the new id and
				// reads finished tasks to find who. That one column is written,
				// guarded by the status, and it is not listed among the work
				// re-pointed. Under any other mapping the task is not written
				// at all: it must not say somebody did a step they did not do.
				if !openTask(task.Status) {
					if err := s.renameFinishedStep(txCtx, task, renames); err != nil {
						return err
					}
					continue
				}
				mapped := mapNode(nodeMapping, task.NodeID)
				if mapped == task.NodeID {
					continue
				}
				node, ok := targetNodes[mapped]
				if !ok {
					// The planner refuses this, so reaching it means the graph
					// changed under us between plan and apply. Stop rather than
					// write a task pointing at a node that is not there.
					return apierr.Invalidf("version %d of %q no longer has node %q",
						target.Version, target.Key, mapped)
				}
				moved[task.NodeID] = mapped
				if err := s.repo.Task().Update(txCtx, retargetTask(task, node)); err != nil {
					return err
				}
			}

			jobs, err := s.repo.Job().ListByInstance(txCtx, uuid.UUID(instance.ID))
			if err != nil {
				return err
			}
			for _, job := range jobs {
				// The same rule for a timer or a call that has already run: it
				// stays naming the version and the step it ran on. Only what can
				// still run is pointed at the new graph — a failed job too, which
				// runs again when its incident is resolved.
				if finishedJob(job.Status) {
					continue
				}
				job.DefinitionID = models.UUID(targetDefID)
				job.NodeID = mapNode(nodeMapping, job.NodeID)
				if err := s.repo.Job().Update(txCtx, job); err != nil {
					return err
				}
			}

			return s.remapSubscriptions(txCtx, uuid.UUID(instance.ID), nodeMapping, targetNodes)
		})
		if err != nil {
			return result, fmt.Errorf("failed to migrate instance %s: %w (%d of %d instances had already been "+
				"dealt with; run the same migration again to carry on from here)",
				instance.ID, err, done, len(instances))
		}
		if elsewhere {
			// Nothing of this run's was written to it. Not counted among those
			// dealt with either: it is no longer one of the source version's.
			result.PassedOver = append(result.PassedOver, passedOver(instance, alreadyMoved(source)))
			log.Info().Str("instance", uuid.UUID(instance.ID).String()).Str("run", runID.String()).
				Msg("A migration passed over an instance that another run had already moved off the source version. " +
					"It was not decided or moved again")
			continue
		}
		if settled {
			result.PassedOver = append(result.PassedOver, passedOver(instance, noLongerRunning(source)))
			continue
		}
		if stuck != "" {
			// Left exactly as its lock found it, on the version it is running.
			// Not counted among those dealt with: the next run, or the next
			// plan, finds it where it now stands.
			result.PassedOver = append(result.PassedOver, passedOver(instance, stuck))
			log.Info().Str("instance", uuid.UUID(instance.ID).String()).Str("run", runID.String()).
				Msg("A migration passed over an instance that holds work the new version cannot take as it stands. " +
					"It stays on the version it is running; the reply says what the work is and what to do")
			continue
		}
		s.recordMigration(ctx, instance, source, target, moved, options, waived, runID, entryID)
		done++
		if !made.wrote {
			result.Changed++
		}
	}

	return result, nil
}

// renameFinishedStep gives a finished task its step's new id, when the mapping
// renames that step, and writes nothing otherwise.
func (s *migrationService) renameFinishedStep(ctx context.Context, task models.TaskModel, renames map[string]string) error {
	to, renamed := renames[task.NodeID]
	if !renamed {
		return nil
	}
	if _, err := s.repo.Task().RenameFinishedStep(ctx, uuid.UUID(task.ID), to); err != nil {
		return fmt.Errorf("renaming the step of finished task %s: %w", task.ID, err)
	}
	return nil
}

// recordControlLosses enters in the instance's ledger each acknowledged control
// step it loses, in the rewrite's transaction: a loss that cannot be recorded
// is a rewrite that does not happen.
func (s *migrationService) recordControlLosses(
	ctx context.Context,
	instance models.ProcessInstanceModel,
	sourceDefID uuid.UUID,
	waived []entities.ComplianceHold,
	options servicecontracts.MigrationOptions,
	runID, entryID uuid.UUID,
) error {
	for _, row := range controlLossDeviations(instance, sourceDefID, waived, migrationActor(options), runID, entryID) {
		if _, err := s.ledger.Record(ctx, row); err != nil {
			return fmt.Errorf("recording that instance %s loses %q: %w", instance.ID, row.Node.ID, err)
		}
	}
	return nil
}

// recordMigration writes the audit entry for one migrated instance.
//
// Outside the unit of work on purpose: a migration that succeeded should not be
// rolled back because the audit write failed, and an audit entry for a
// migration that did not happen is worse than a missing one. A lost entry is
// logged loudly because the trail is what somebody will be asked for later.
//
// entryID is the id the instance's control-loss ledger rows already point at,
// and waived the losses those rows record.
func (s *migrationService) recordMigration(
	ctx context.Context,
	instance models.ProcessInstanceModel,
	source, target models.ProcessDefinitionModel,
	moved map[string]string,
	options servicecontracts.MigrationOptions,
	waivedHolds []entities.ComplianceHold,
	runID, entryID uuid.UUID,
) {
	if s.audit == nil {
		return
	}
	pairs := make([]string, 0, len(moved))
	for from, to := range moved {
		pairs = append(pairs, fmt.Sprintf("%s→%s", from, to))
	}
	slices.Sort(pairs)

	actor := migrationActor(options)
	narrative := fmt.Sprintf("This instance was moved from version %d to version %d of %q by a migration, authorised by %s.",
		source.Version, target.Version, target.Key, actor)
	if len(pairs) > 0 {
		narrative += fmt.Sprintf(" Work in progress was re-pointed: %s.", strings.Join(pairs, ", "))
	}

	// Which control-bearing steps this instance lost, and only the ones it had
	// not already performed — worked out once, before the rewrite, so this
	// entry and the ledger rows that point at it agree.
	var waived []string
	for _, hold := range waivedHolds {
		waived = append(waived, hold.NodeID)
	}
	if len(waived) > 0 {
		narrative += fmt.Sprintf(" It had not yet passed %s, and %s accepted that it never will.",
			strings.Join(waived, ", "), actor)
	}

	entry := entities.AuditEntry{
		ID:        entryID,
		Type:      EventInstanceMigrated,
		Message:   fmt.Sprintf("migrated from version %d to version %d", source.Version, target.Version),
		Narrative: narrative,
		Timestamp: time.Now(),
		Data: map[string]any{
			"source_version":  source.Version,
			"target_version":  target.Version,
			"process_key":     target.Key,
			"task_moves":      pairs,
			"run_id":          runID.String(),
			"actor":           actor,
			"waived_controls": waived,
		},
		Project:  &entities.Project{ID: uuid.UUID(instance.ProjectID)},
		Instance: &entities.ProcessInstance{ID: uuid.UUID(instance.ID)},
	}
	if err := s.audit.RecordEvent(ctx, entry); err != nil {
		log.Error().Err(err).
			Str("instance", uuid.UUID(instance.ID).String()).
			Int("source_version", source.Version).
			Int("target_version", target.Version).
			Msg("A migration audit event was lost; the trail cannot explain how this instance changed version")
	}
}

// surveyResult is what one pass over the running instances found.
type surveyResult struct {
	moves []entities.NodeMove
	// unlandable are nodes holding tokens, tasks or jobs that the target has no
	// node for.
	unlandable []string
	// stateStranded are nodes holding live engine bookkeeping — a join counter
	// or a multi-instance counter — that the target has no node for.
	stateStranded []string
	// stateCollisions are target nodes that two or more bookkeeping-carrying
	// source nodes would merge onto.
	stateCollisions []string
	// landings are the target nodes work would arrive on, for the downstream
	// graph checks.
	landings map[string]struct{}
	// leftOnDecided are the decided nodes the target has no node for that hold
	// an open task or a waiting event. In the plan that is the work the
	// decision is for, and it is not read. The rewrite reads it, once the
	// decisions have been taken (whyNotMoved).
	leftOnDecided []string
}

// survey works out where the running work sits and where it would land.
//
// One pass answers every question the caller has: the plan to show, the nodes
// that have nowhere to go, and the engine bookkeeping that would be stranded or
// merged. Doing them separately meant walking every instance's tasks and jobs
// more than once — and, worse, meant a preview and an apply could disagree
// about what "landable" meant.
//
// Moves are aggregated by node rather than listed per instance: a cutover of a
// thousand purchase orders parked on three nodes is three lines, not a thousand.
func (s *migrationService) survey(
	ctx context.Context,
	instances []models.ProcessInstanceModel,
	targetNodes map[string]models.FlowNode,
	nodeMapping map[string]string,
	actions map[string]servicecontracts.NodeAction,
) (surveyResult, error) {
	type counts struct{ tokens, tasks, claimed, delegated, jobs, events int }
	byNode := map[string]*counts{}
	stranded := map[string]struct{}{}
	strandedState := map[string]struct{}{}
	// Which source nodes each target node would receive bookkeeping from. More
	// than one is an ambiguity nothing can resolve.
	mergedInto := map[string]map[string]struct{}{}
	result := surveyResult{landings: map[string]struct{}{}}

	lands := func(nodeID string) (string, bool) {
		// Work on a node with an action does not need anywhere to land: it is
		// being cancelled or advanced past, not moved. Refusing it for having
		// no home in the target is refusing the very thing the caller asked
		// for.
		if _, decided := actions[nodeID]; decided {
			return nodeID, true
		}
		to := mapNode(nodeMapping, nodeID)
		_, ok := targetNodes[to]
		return to, ok
	}

	count := func(nodeID string, add func(*counts)) {
		if nodeID == "" {
			return
		}
		to, ok := lands(nodeID)
		_, decided := actions[nodeID]
		switch {
		case !ok:
			stranded[nodeID] = struct{}{}
		case !decided:
			result.landings[to] = struct{}{}
		}
		c, ok := byNode[nodeID]
		if !ok {
			c = &counts{}
			byNode[nodeID] = c
		}
		add(c)
	}

	// An open task or a waiting event on a decided node the target lacks. A
	// decided node is never mapped (the planner refuses both at once), so it
	// lands only where the target has a node of the same id.
	left := map[string]struct{}{}
	noteLeft := func(nodeID string) {
		if _, decided := actions[nodeID]; !decided {
			return
		}
		if _, ok := targetNodes[nodeID]; !ok {
			left[nodeID] = struct{}{}
		}
	}

	// Bookkeeping is tracked separately from tokens: a node can hold a join
	// counter with no token on it, and that counter still has to land.
	noteState := func(nodeID string) {
		if nodeID == "" {
			return
		}
		to, ok := lands(nodeID)
		if !ok {
			strandedState[nodeID] = struct{}{}
			return
		}
		if mergedInto[to] == nil {
			mergedInto[to] = map[string]struct{}{}
		}
		mergedInto[to][nodeID] = struct{}{}
	}

	for _, instance := range instances {
		for _, token := range instance.Tokens {
			count(token.NodeID, func(c *counts) { c.tokens++ })
		}
		for nodeID := range instance.Joins {
			noteState(nodeID)
		}
		for nodeID := range instance.MultiInstance {
			noteState(nodeID)
		}
		// Tasks and jobs are separate rows pointing at the same graph. A task
		// left on a node the target lacks is work nobody can complete, and a job
		// is a timer or a service call that fires into nothing.
		tasks, listErr := s.repo.Task().ListByInstance(ctx, uuid.UUID(instance.ID))
		if listErr != nil {
			return surveyResult{}, listErr
		}
		for _, task := range tasks {
			if openTask(task.Status) {
				noteLeft(task.NodeID)
			}
			switch task.Status {
			case models.TaskUnclaimed:
				count(task.NodeID, func(c *counts) { c.tasks++ })
			case models.TaskClaimed:
				// Counted apart because they read differently to whoever is
				// deciding: an unclaimed task is a queue item, a claimed one is
				// somebody with the form open right now.
				count(task.NodeID, func(c *counts) { c.tasks++; c.claimed++ })
			case models.TaskDelegated:
				count(task.NodeID, func(c *counts) { c.tasks++; c.delegated++ })
			}
		}
		jobs, listErr := s.repo.Job().ListByInstance(ctx, uuid.UUID(instance.ID))
		if listErr != nil {
			return surveyResult{}, listErr
		}
		for _, job := range jobs {
			// A job that has run is a record of it, not work: a job cannot be
			// deleted, so a timer that fired stays behind, one row for every
			// occurrence of a repeating one. Counted, it refused the migration
			// of an instance that had long since passed a wait the new version
			// dropped, for "work parked" on a step nobody was on. A failed one
			// is still counted: resolving its incident queues it again.
			if finishedJob(job.Status) {
				continue
			}
			count(job.NodeID, func(c *counts) { c.jobs++ })
		}
		// Event subscriptions are the one kind of waiting that does not always
		// sit under a token. A message boundary event on a user task keeps its
		// subscription on the boundary node while the token stays on the task
		// it guards, so mapping the task and forgetting the event leaves a
		// subscription pointing at a node the target does not have — and the
		// message, when it arrives, correlates to nothing.
		subs, listErr := s.repo.Subscription().ListByInstance(ctx, uuid.UUID(instance.ID))
		if listErr != nil {
			return surveyResult{}, listErr
		}
		for _, sub := range subs {
			noteLeft(sub.NodeID)
			count(sub.NodeID, func(c *counts) { c.events++ })
		}
	}

	for from, c := range byNode {
		to, mapped := nodeMapping[from]
		if !mapped {
			to = from
		}
		result.moves = append(result.moves, entities.NodeMove{
			From:           from,
			To:             to,
			Tokens:         c.tokens,
			Tasks:          c.tasks,
			TasksClaimed:   c.claimed,
			TasksDelegated: c.delegated,
			Jobs:           c.jobs,
			Events:         c.events,
			Mapped:         mapped,
		})
	}
	// Sorted so the same plan reads the same way twice; a map's order would make
	// a preview look different every time it was refreshed.
	slices.SortFunc(result.moves, func(a, b entities.NodeMove) int { return strings.Compare(a.From, b.From) })

	result.unlandable = sortedKeys(stranded)
	result.stateStranded = sortedKeys(strandedState)
	result.leftOnDecided = sortedKeys(left)

	for to, froms := range mergedInto {
		if len(froms) < 2 {
			continue
		}
		sources := sortedKeys(froms)
		result.stateCollisions = append(result.stateCollisions,
			fmt.Sprintf("%s onto %s", strings.Join(sources, " and "), to))
	}
	slices.Sort(result.stateCollisions)
	return result, nil
}

// retargetTask rewrites a task onto the node it has been mapped to.
//
// Everything a task shows and everything it authorises comes from its node, so
// a task that has moved to a different node has to be rebuilt from that node
// rather than carried across. Carrying it was an authorisation bug: a task
// mapped from "operations-approve" onto "sales-approve" kept the operations
// manager as its assignee and its candidate groups, which let the person whose
// step had just been deleted complete the step that replaced it — and left the
// sales manager, who was supposed to approve, never seeing it.
//
// Camunda preserves the assignee across a migration, but only because it
// requires the two activities to be semantically equivalent first. Nothing here
// can establish that, so the safe default is the other one: re-derive, and make
// whoever held it claim it again.
func retargetTask(task models.TaskModel, node models.FlowNode) models.TaskModel {
	task.NodeID = node.ID
	task.Name = node.Name
	task.Description = node.Documentation
	task.Type = node.Type
	task.Priority = node.Priority
	task.FormKey = node.FormKey
	task.FormDefinition = entities.PropertyText(node.Properties, "form_definition")
	task.CandidateUsers = slices.Clone(node.CandidateUsers)
	task.CandidateGroups = slices.Clone(node.CandidateGroups)

	// A duration counts from when the task appeared, not from the move: moving
	// work to another version must not quietly extend its deadline.
	appeared := task.CreatedAt
	if appeared.IsZero() {
		appeared = time.Now()
	}
	task.DueDate = entities.ResolveDueDate(node.DueDate, appeared)

	// The claim does not survive the move. Whoever held this task claimed the
	// step that was deleted, not the one they are now looking at. Nor does a
	// delegation: left on, the moved task would wait for a delegate who no
	// longer holds it to hand it back.
	task.Owner = ""
	task.DelegationState = ""
	if node.Assignee != "" {
		task.Assignee = node.Assignee
		task.Status = models.TaskClaimed
		return task
	}
	task.Assignee = ""
	task.Status = models.TaskUnclaimed
	return task
}

// boundaryRefusals reports mappings that would detach a boundary event from the
// activity it guards.
//
// A boundary event only means anything in relation to its host: a three-day
// escalation on "operations approve" is not a three-day escalation on whatever
// else the target happens to attach one to. Mapping the event without mapping
// the host the same way leaves a timer that fires against work that is not
// running. Camunda 8 refuses the same shape.
//
// And it may be mapped only to a boundary event at all. Only a mapping from one
// boundary event to another used to be looked at, so one onto an ordinary step
// passed because the step exists.
func boundaryRefusals(sourceNodes, targetNodes map[string]models.FlowNode, nodeMapping map[string]string) []string {
	var out []string
	for from, to := range nodeMapping {
		src, ok := sourceNodes[from]
		if !ok || src.Type != models.BoundaryEvent {
			continue
		}
		tgt, ok := targetNodes[to]
		if !ok {
			continue
		}
		// What sits on a boundary event is a timer or a waiting message that,
		// when it fires, acts on the node it sits on. Moved onto a step that
		// is not a boundary event, the timer is read as that step's own wait
		// coming to an end: the instance was advanced past an approval nobody
		// gave, and finished with the approver's task still open.
		if tgt.Type != models.BoundaryEvent {
			out = append(out, fmt.Sprintf(
				"%s→%s would put what waits on a boundary event onto a step that is not one: the timer or message of %q "+
					"would then act on %q itself, as though that step had been performed; a boundary event may be mapped "+
					"only to a boundary event of the new version",
				from, to, flowNodeName(src), flowNodeName(tgt)))
			continue
		}
		if src.AttachedToRef == "" {
			continue
		}
		want := mapNode(nodeMapping, src.AttachedToRef)
		if tgt.AttachedToRef == want {
			continue
		}
		out = append(out, fmt.Sprintf(
			"%s→%s would detach a boundary event from the activity it guards: %s watches %q, "+
				"but %s watches %q; map the activity the same way or leave the event alone",
			from, to, from, src.AttachedToRef, to, tgt.AttachedToRef))
	}
	slices.Sort(out)
	return out
}

// boundaryAdvice is what the landing refusal adds for a boundary event among
// the nodes it names, and nothing when there is none.
//
// "Map each one to a node it does have" is the wrong thing to say of a boundary
// event: it may be mapped only to a boundary event, and the usual case is that
// the new version dropped it. What works then is said instead.
func boundaryAdvice(sourceNodes map[string]models.FlowNode, unlandable []string) string {
	var events []string
	for _, id := range unlandable {
		if node, ok := sourceNodes[id]; ok && node.Type == models.BoundaryEvent {
			events = append(events, fmt.Sprintf("%q", flowNodeName(node)))
		}
	}
	switch len(events) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf(". Of those, %s is a boundary event: it can be mapped only to a boundary event of the new "+
			"version; where the new version has none, decide the step it is attached to and name the event in the same "+
			"decision, and what waits on the event ends with that step", events[0])
	}
	return fmt.Sprintf(". Of those, %s and %s are boundary events: each can be mapped only to a boundary event of the "+
		"new version; where the new version has none, decide the step each is attached to and name its events in the "+
		"same decision, and what waits on them ends with that step",
		strings.Join(events[:len(events)-1], ", "), events[len(events)-1])
}

// defaultFlowWarnings reports gateways downstream of the landing nodes that
// would route silently rather than raise an incident.
//
// A removed user task is a removed producer of whatever its form used to write.
// A gateway that reads one of those variables and finds nothing raises an
// incident, which is loud and correct — unless it has a default flow, in which
// case every migrated instance takes that branch and nobody is told. This is
// the one failure in this file with no error attached to it, so the plan says
// it out loud instead.
func defaultFlowWarnings(
	target models.ProcessDefinitionModel,
	targetNodes map[string]models.FlowNode,
	landings map[string]struct{},
) []string {
	if len(landings) == 0 {
		return nil
	}
	outgoing := map[string][]string{}
	for _, flow := range allFlows(target) {
		outgoing[flow.SourceRef] = append(outgoing[flow.SourceRef], flow.TargetRef)
	}

	seen := map[string]struct{}{}
	queue := sortedKeys(landings)
	for _, id := range queue {
		seen[id] = struct{}{}
	}
	var risky []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		node, ok := targetNodes[id]
		if ok && node.DefaultFlow != "" &&
			(node.Type == models.ExclusiveGateway || node.Type == models.InclusiveGateway) {
			risky = append(risky, fmt.Sprintf(
				"gateway %q is downstream of this migration and has a default flow, so an instance "+
					"missing a variable the removed work used to set will take that branch silently "+
					"instead of raising an incident", id))
		}
		for _, next := range outgoing[id] {
			if _, done := seen[next]; done {
				continue
			}
			seen[next] = struct{}{}
			queue = append(queue, next)
		}
	}
	slices.Sort(risky)
	return risky
}

// claimWarnings reports work somebody has in their hands right now.
func claimWarnings(moves []entities.NodeMove) []string {
	var out []string
	for _, move := range moves {
		if move.From == move.To {
			continue
		}
		if held := move.TasksClaimed + move.TasksDelegated; held > 0 {
			out = append(out, fmt.Sprintf(
				"%d task(s) on %q are claimed or delegated right now; migrating re-derives who may do "+
					"the work, so they will be returned to the queue and whoever held them loses their place",
				held, move.From))
		}
	}
	return out
}

// redirectWarnings reports mappings that send a step's open work to a
// different step, where that says something about work already done.
//
// Finished work does not follow a redirect: a step an instance completed stays
// in its record under its own id, and is not counted as the step the mapping
// names. That is the truthful record, and it has a consequence somebody should
// see before applying: a control or a separation-of-duties rule on the step
// mapped to does not see the work done on the step mapped from. Said only
// where it bites — an instance in the plan has completed the step, or the step
// carries a control obligation. The count is of running instances, which are
// the ones a migration moves.
func redirectWarnings(
	sourceNodes map[string]models.FlowNode,
	nodeMapping map[string]string,
	instances []models.ProcessInstanceModel,
) []string {
	renames := renamedSteps(sourceNodes, nodeMapping)
	// How many of the instances this migration would move have completed each
	// step: the running ones. The listing has every instance of the version,
	// finished ones included, and those are not the plan's to count. Counted
	// in one pass, so that the warning costs a look at each instance's record
	// and not one for every entry of the mapping.
	completedBy := map[string]int{}
	for _, instance := range instances {
		if instance.Status != models.ProcessActive {
			continue
		}
		for _, id := range instance.CompletedNodes {
			completedBy[id]++
		}
	}
	var out []string
	for from, to := range nodeMapping {
		node, known := sourceNodes[from]
		if _, renamed := renames[from]; renamed || !known || from == to {
			continue
		}
		completed := completedBy[from]
		controlled := boolProperty(node.Properties, "compliance_relevant")
		if completed == 0 && !controlled {
			continue
		}
		detail := fmt.Sprintf("%d running instance(s) have completed %q", completed, from)
		if controlled {
			detail += fmt.Sprintf("; %q carries a control obligation", from)
		}
		out = append(out, fmt.Sprintf(
			"%q is mapped onto %q, which is a different step: open work moves there, but work already done on %q "+
				"does not count as done on %q, so a control or a separation-of-duties rule on %q will not see it (%s)",
			from, to, from, to, to, detail))
	}
	slices.Sort(out)
	return out
}

// removedNodes lists the nodes the target version no longer has.
func removedNodes(sourceNodes, targetNodes map[string]models.FlowNode) []string {
	var out []string
	for id := range sourceNodes {
		if _, ok := targetNodes[id]; !ok {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// nodeIndex indexes every node in a definition by id, including the ones nested
// inside sub-processes. Reading only the top-level list made a token parked
// inside a sub-process look like it had nowhere to land.
func nodeIndex(nodes []models.FlowNode) map[string]models.FlowNode {
	index := map[string]models.FlowNode{}
	var walk func([]models.FlowNode)
	walk = func(list []models.FlowNode) {
		for _, node := range list {
			index[node.ID] = node
			walk(node.Nodes)
		}
	}
	walk(nodes)
	return index
}

// incomingFlows counts the sequence flows that arrive at each node of a
// definition, nested ones included. Counted from the flows, as the engine
// counts them: a node's own list of incoming flows is what the designer drew
// and need not be there.
func incomingFlows(def models.ProcessDefinitionModel) map[string]int {
	incoming := map[string]int{}
	for _, flow := range allFlows(def) {
		incoming[flow.TargetRef]++
	}
	return incoming
}

// allFlows returns every sequence flow in a definition, nested ones included.
func allFlows(def models.ProcessDefinitionModel) []models.SequenceFlow {
	flows := slices.Clone(def.Flows)
	var walk func([]models.FlowNode)
	walk = func(list []models.FlowNode) {
		for _, node := range list {
			flows = append(flows, node.Flows...)
			walk(node.Nodes)
		}
	}
	walk(def.Nodes)
	return flows
}

// renamedSteps is the part of a mapping that renames a step and does nothing
// else: the id it maps to is not a step of the source version, and no other
// step is mapped onto it.
//
// A mapping has two shapes and they mean different things for work already
// done. "submit → request", where request is new, says the step has a new
// name: whoever did the submit did the request. "opsApprove → salesApprove",
// where the source version has both, sends the open operations approvals to
// the sales manager, and says nothing of the kind about an operations approval
// already given; nor do two steps mapped onto one new id, which cannot both
// be it. Work in progress follows any mapping. Finished work follows only a
// rename.
func renamedSteps(sourceNodes map[string]models.FlowNode, nodeMapping map[string]string) map[string]string {
	mappedOnto := make(map[string]int, len(nodeMapping))
	for _, to := range nodeMapping {
		mappedOnto[to]++
	}
	renames := map[string]string{}
	for from, to := range nodeMapping {
		if _, alsoASourceStep := sourceNodes[to]; alsoASourceStep || mappedOnto[to] != 1 || from == to {
			continue
		}
		renames[from] = to
	}
	return renames
}

// mapNodeList rewrites a list of node ids through the mapping.
//
// Entries with no mapping are kept rather than dropped: CompletedNodes is the
// record of what this instance actually ran, and a step the new version deleted
// is still a step this instance performed. The rewrite gives it the renames of
// a mapping only (renamedSteps), for the same reason.
func mapNodeList(nodeMapping map[string]string, ids []string) []string {
	if len(ids) == 0 {
		return ids
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		mapped := mapNode(nodeMapping, id)
		if _, dup := seen[mapped]; dup {
			continue
		}
		seen[mapped] = struct{}{}
		out = append(out, mapped)
	}
	return out
}

// mapMultiInstance rekeys multi-instance bookkeeping onto the target's node ids.
//
// The planner refuses a mapping that would merge two of these onto one key, so
// this never has to decide what adding two iteration counters together means.
func mapMultiInstance(nodeMapping map[string]string, state map[string]models.MultiInstanceStateModel) map[string]models.MultiInstanceStateModel {
	if len(state) == 0 {
		return state
	}
	out := make(map[string]models.MultiInstanceStateModel, len(state))
	for nodeID, value := range state {
		out[mapNode(nodeMapping, nodeID)] = value
	}
	return out
}

// mapJoins rekeys gateway join counters onto the target's node ids.
func mapJoins(nodeMapping map[string]string, joins map[string]int) map[string]int {
	if len(joins) == 0 {
		return joins
	}
	out := make(map[string]int, len(joins))
	for nodeID, arrived := range joins {
		out[mapNode(nodeMapping, nodeID)] = arrived
	}
	return out
}

func mapNode(nodeMapping map[string]string, nodeID string) string {
	if mapped, ok := nodeMapping[nodeID]; ok {
		return mapped
	}
	return nodeID
}

// boolProperty reads a boolean node property, accepting the string form a JSON
// round-trip or an XML import may leave behind — the same two shapes
// Node.GetBoolProperty accepts, because a definition that imports one way and
// is read the other is how a control marking goes quietly missing.
func boolProperty(properties map[string]any, key string) bool {
	if properties == nil {
		return false
	}
	switch value := properties[key].(type) {
	case bool:
		return value
	case string:
		return value == "true"
	default:
		return false
	}
}

func stringProperty(properties map[string]any, key string) string {
	if properties == nil {
		return ""
	}
	if value, ok := properties[key].(string); ok {
		return value
	}
	return ""
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// actionRefusals checks the nodes whose work is to be decided rather than moved.
//
// Every one of these is a case where doing the thing half-way is worse than not
// doing it: a skip with no successor leaves a token nowhere, a skip and a
// mapping on the same node are two contradictory instructions, and a skip or a
// cancel with no reason produces a trail that cannot be told apart from the
// step having been performed.
func (s *migrationService) actionRefusals(
	sourceNodes, targetNodes map[string]models.FlowNode,
	nodeMapping map[string]string,
	actions map[string]servicecontracts.NodeAction,
	incoming map[string]int,
) []string {
	var out []string
	// Which nodes are inside which, for telling what to decide instead of a
	// sub-process: built at most once for the plan, and only if it is asked for.
	inside := sync.OnceValue(func() map[string][]models.FlowNode { return nodesByParent(sourceNodes) })
	for nodeID, action := range actions {
		node, known := sourceNodes[nodeID]
		if !known {
			out = append(out, fmt.Sprintf("there is no node %q in the version being migrated from", nodeID))
			continue
		}
		switch action.Kind {
		case servicecontracts.NodeActionSkip, servicecontracts.NodeActionCancel, servicecontracts.NodeActionHold:
		default:
			out = append(out, fmt.Sprintf("%q is not something a migration can do with %q; use skip, cancel or hold",
				action.Kind, nodeID))
			continue
		}
		reason := strings.TrimSpace(action.Reason)
		if reason == "" {
			out = append(out, fmt.Sprintf(
				"%s of %q needs a reason: without one the trail cannot tell a step nobody performed "+
					"from a step somebody did", action.Kind, nodeID))
		}
		// Counted as the ledger counts it — characters, after the spaces around
		// it are dropped — and refused here, where a dry run shows it. Left to
		// the ledger, it would stop an apply at the first instance on this
		// node, after the ones ahead of it had already been moved.
		if utf8.RuneCountInString(reason) > entities.MaxDeviationReasonLength {
			out = append(out, fmt.Sprintf(
				"%s of %q has a reason longer than %d characters, which is more than its record can hold; shorten it",
				action.Kind, nodeID, entities.MaxDeviationReasonLength))
		}
		if _, mapped := nodeMapping[nodeID]; mapped {
			out = append(out, fmt.Sprintf(
				"%q is both mapped and set to %s; those are different instructions, so say which one you mean",
				nodeID, action.Kind))
		}
		// A decision is taken on the instances holding a token on the node, so
		// one on a node no instance ever holds a token on is taken on nobody —
		// and still excused whatever waits on that node from having to land.
		if why := nobodyWaitsAt(node, sourceNodes, actions, incoming, inside); why != "" {
			out = append(out, fmt.Sprintf("%s of %q cannot be taken: %s", action.Kind, flowNodeName(node), why))
		}
		if action.Kind != servicecontracts.NodeActionSkip {
			continue
		}
		if s.engine == nil {
			out = append(out, fmt.Sprintf(
				"skipping %q needs the execution engine, and this deployment was wired without one", nodeID))
			continue
		}
		// Skipping means the engine advances along the node's outgoing flow, so
		// there has to be exactly one and it has to land somewhere the target
		// still has. Skipping a gateway is not defined: which branch would it
		// have taken?
		successors := node.Outgoing
		switch {
		case len(successors) == 0:
			out = append(out, fmt.Sprintf("%q has no outgoing flow, so there is nowhere to advance to when it is skipped", nodeID))
		case len(successors) > 1:
			out = append(out, fmt.Sprintf(
				"%q has %d outgoing flows, so skipping it would have to choose a branch on the business's behalf",
				nodeID, len(successors)))
		}
	}
	slices.Sort(out)
	return out
}

// plannedActions is what the plan reports back about the decided nodes.
func plannedActions(sourceNodes map[string]models.FlowNode, actions map[string]servicecontracts.NodeAction) []entities.PlannedNodeAction {
	if len(actions) == 0 {
		return nil
	}
	out := make([]entities.PlannedNodeAction, 0, len(actions))
	for nodeID, action := range actions {
		out = append(out, entities.PlannedNodeAction{
			NodeID: nodeID,
			Name:   sourceNodes[nodeID].Name,
			Kind:   string(action.Kind),
			Reason: action.Reason,
		})
	}
	slices.SortFunc(out, func(a, b entities.PlannedNodeAction) int { return strings.Compare(a.NodeID, b.NodeID) })
	return out
}

// decide performs the cancel and skip actions for one instance.
//
// It runs before the node ids are rewritten, and deliberately outside the
// migration's own transaction. A skip is the engine advancing an instance,
// which opens its own unit of work; nesting the two would make one rollback
// mean something different from the other. Split this way each step is atomic
// on its own, and the worst interleaving — a skip that lands and a migration
// that then fails — leaves the instance past a node that was going away anyway,
// still on the version it started on, and still correct.
//
// The copy of the instance it is given is the listing's, and as old as the run
// has been going, so it reads the instance again and tries an action at a step
// the instance holds work on in either copy. On the fresh copy, because an
// instance that reached the step after the listing is one of the instances the
// decision is about: left out, it was moved with its token on the very step a
// skip was there to clear. On the listing's too, so that an instance which has
// left the step meanwhile is still found to have left it, and is left alone
// rather than moved.
//
// Neither copy decides anything. Each action asks again, under the instance's
// lock, whether the instance is still running, still on the version being
// migrated from and still on that step, and acts on nothing else. One that is
// not is left alone altogether (notDecided) — a holder who completed the step
// meanwhile gave the approval, and advancing past it again would be a second
// advance; an instance another run has already moved is not this migration's
// to decide at all. And an instance that reaches a decided step after this
// read is caught by the rewrite, under its own lock.
//
// Reports what the run should do with the instance next, which step it was
// found to have left when it is to be passed over, and whether anything was
// written to it.
func (s *migrationService) decide(
	ctx context.Context,
	listed models.ProcessInstanceModel,
	sourceDefID uuid.UUID,
	source, target models.ProcessDefinitionModel,
	options servicecontracts.MigrationOptions,
	runID uuid.UUID,
) (decision, error) {
	if len(options.Actions) == 0 {
		return decision{outcome: outcomeMove}, nil
	}
	instanceID := uuid.UUID(listed.ID)
	instance, err := s.repo.Process().Get(ctx, instanceID)
	if err != nil {
		return decision{}, fmt.Errorf("re-reading instance %s to decide its work: %w", instanceID, err)
	}
	atStep := func(nodeID string) bool {
		return holdsWork(instance, nodeID) || holdsWork(listed, nodeID)
	}

	// Cancel wins over skip: there is no point advancing an instance past a
	// node in order to end it two lines later.
	for _, nodeID := range sortedKeys(options.Actions) {
		action := options.Actions[nodeID]
		if action.Kind != servicecontracts.NodeActionCancel || !atStep(nodeID) {
			continue
		}
		cancelled, err := s.cancelInstance(ctx, instance, nodeID, action, options, source, target, runID)
		if err != nil || cancelled {
			return settledOr(cancelled, nodeID), err
		}
		return s.notDecided(ctx, instanceID, sourceDefID, nodeID, false)
	}

	for _, nodeID := range sortedKeys(options.Actions) {
		action := options.Actions[nodeID]
		if action.Kind != servicecontracts.NodeActionHold || !atStep(nodeID) {
			continue
		}
		held, err := s.holdInstance(ctx, instance, nodeID, action, options, source, target, runID)
		if err != nil || held {
			return settledOr(held, nodeID), err
		}
		return s.notDecided(ctx, instanceID, sourceDefID, nodeID, false)
	}

	skips := 0
	for _, nodeID := range sortedKeys(options.Actions) {
		action := options.Actions[nodeID]
		if action.Kind != servicecontracts.NodeActionSkip || !atStep(nodeID) {
			continue
		}
		skipped, err := s.skipNode(ctx, instanceID, sourceDefID, nodeID, action, instance, source, target, options, runID)
		if err != nil {
			return decision{outcome: outcomeMovedOn, left: nodeID, wrote: skips > 0}, err
		}
		if !skipped {
			// Not on the step any more: the plan no longer describes this
			// instance, so nothing further is decided about it in this run.
			// A skip made before this one stands, and is said to have been.
			return s.notDecided(ctx, instanceID, sourceDefID, nodeID, skips > 0)
		}
		skips++
	}
	if skips == 0 {
		return decision{outcome: outcomeMove}, nil
	}

	// The engine moved the instance, so anything read before this is stale. A
	// skip can also finish the instance outright — the removed approval was the
	// last step — and there is then nothing left to migrate.
	refreshed, err := s.repo.Process().Get(ctx, instanceID)
	if err != nil {
		return decision{outcome: outcomeDealtWith, wrote: true}, fmt.Errorf("re-reading instance %s after a skip: %w", instanceID, err)
	}
	if refreshed.Status != models.ProcessActive {
		return decision{outcome: outcomeDealtWith, wrote: true}, nil
	}
	return decision{outcome: outcomeMove, wrote: true}, nil
}

// notDecided is what the run does with an instance an action found, under its
// lock, not to be one it decides: no longer running, no longer on the step, or
// no longer on the version being migrated from.
//
// The first two are the instance that moved on, which is passed over and told
// which step it had left. The third is one another run of a migration has
// already moved. Telling that one it "stays on" the source version would be
// false, so it is sent on to the rewrite, which asks under its own lock which
// version the instance is on, writes nothing to one that has left the source
// version, and says so. wrote is whether an earlier skip of this run stands.
func (s *migrationService) notDecided(ctx context.Context, instanceID, sourceDefID uuid.UUID, nodeID string, wrote bool) (decision, error) {
	left := decision{outcome: outcomeMovedOn, left: nodeID, wrote: wrote}
	now, err := s.repo.Process().Get(ctx, instanceID)
	if err != nil {
		return left, fmt.Errorf("re-reading instance %s, which was not decided at %q: %w", instanceID, nodeID, err)
	}
	if !onVersion(now, sourceDefID) {
		return decision{outcome: outcomeMove, wrote: wrote}, nil
	}
	return left, nil
}

// settledOr is what a cancel or a hold at nodeID made of an instance: dealt
// with when the action found it where the listing said, and moved on from that
// step, with nothing written, when it did not.
func settledOr(found bool, nodeID string) decision {
	if found {
		return decision{outcome: outcomeDealtWith, wrote: true}
	}
	return decision{outcome: outcomeMovedOn, left: nodeID}
}

// skipNode cancels the work parked on one node and advances the instance past
// it as though it had been performed.
//
// It reports whether it did. An instance found, once locked, to have finished
// or to hold no token on the node is not skipped: nothing is withdrawn,
// advanced or recorded.
func (s *migrationService) skipNode(
	ctx context.Context,
	instanceID, sourceDefID uuid.UUID,
	nodeID string,
	action servicecontracts.NodeAction,
	instance models.ProcessInstanceModel,
	source, target models.ProcessDefinitionModel,
	options servicecontracts.MigrationOptions,
	runID uuid.UUID,
) (skipped bool, err error) {
	if s.engine == nil {
		return false, apierr.Invalidf("skipping %q needs the execution engine, and this deployment was wired without one", nodeID)
	}
	// One unit of work. Withdrawing the task and advancing past the step each
	// committed on their own, so an advance that failed — a gateway after the
	// step with no branch to take — left the task withdrawn and the token on
	// the step: nobody could do the work, and nothing would move the instance
	// on. Now a failed advance puts the task back as it was.
	err = s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		// The instance first, then its task, the order CompleteTask takes
		// them in: a completion racing the skip waits here, and then finds
		// the task withdrawn rather than performing a step already advanced
		// past.
		live, err := s.engine.GetInstanceForUpdate(txCtx, instanceID)
		if err != nil {
			return fmt.Errorf("reading instance %s to skip %q: %w", instanceID, nodeID, err)
		}
		// And the other way round: a completion that won the race has already
		// advanced the instance past the step. Asked of the locked row, because
		// the copy that said "it is parked here" was read before the lock.
		// Advancing again would put a second token after the step and record
		// as waived an approval its holder gave.
		//
		// The same lock answers for the version. An instance another run of a
		// migration has moved meanwhile can stand on a step of the same name on
		// the new version, and advancing it from there along this version's
		// graph would be advancing it along a graph it no longer runs.
		if skipped = runs(live, sourceDefID) && parkedOn(live, nodeID); !skipped {
			return nil
		}
		def, err := s.engine.GetProcessDefinition(txCtx, sourceDefID)
		if err != nil {
			return fmt.Errorf("reading the version %s is running to skip %q: %w", instanceID, nodeID, err)
		}
		// Advanced on the graph the instance is actually running, not the one
		// it is moving to: the node being skipped is the one the new version
		// does not have, so only the old graph knows what follows it.
		withdrawn, err := s.actions.waive(txCtx, &live, def, nodeID)
		if err != nil {
			return err
		}
		// Recorded in the same unit of work: a skip the ledger cannot hold is
		// a skip that does not happen, and the task goes back as it was.
		if err := s.recordDecision(txCtx, decisionRecord{
			instance: instance, definitionID: sourceDefID, source: source, target: target,
			nodeID: nodeID, action: action, options: options, runID: runID, withdrawn: withdrawn,
		}); err != nil {
			return fmt.Errorf("skipping a step of instance %s: %w", instanceID, err)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return skipped, nil
}

// cancelInstance ends an instance where it stands. What ending it does, and
// what it deliberately leaves, is nodeActions.cancel's to say.
//
// It reports whether it did. An instance found, once locked, to have finished
// or to have left nodeID is not one of the instances the cancel was asked to
// end, and is left running.
func (s *migrationService) cancelInstance(
	ctx context.Context,
	instance models.ProcessInstanceModel,
	nodeID string,
	action servicecontracts.NodeAction,
	options servicecontracts.MigrationOptions,
	source, target models.ProcessDefinitionModel,
	runID uuid.UUID,
) (cancelled bool, err error) {
	instanceID := uuid.UUID(instance.ID)
	err = s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		// The locked row, not the listed one, for the same reason apply uses
		// it: writing a copy read earlier would undo whatever landed in between.
		fresh, lockErr := s.repo.Process().GetForUpdate(txCtx, instanceID)
		if lockErr != nil {
			return lockErr
		}
		// Still running, and still at the step: the cancel is of the instances
		// waiting there, and the listing that said this was one of them is as
		// old as the run.
		// And still on the version being migrated from: one another run has
		// moved is no longer part of this migration, whatever step it stands on.
		if cancelled = stillWaitingAt(fresh, source, nodeID); !cancelled {
			return nil
		}
		ended, withdrawn, err := s.actions.cancel(txCtx, fresh)
		if err != nil {
			return err
		}
		// The ledger row and the trail entry go in with the change: a
		// cancellation that cannot be recorded leaves the instance running.
		return s.recordDecision(txCtx, decisionRecord{
			instance: ended, definitionID: uuid.UUID(fresh.DefinitionID), source: source, target: target,
			nodeID: nodeID, action: action, options: options, runID: runID, withdrawn: withdrawn,
			instanceBefore: string(models.ProcessActive), instanceAfter: string(models.ProcessCancelled),
		})
	})
	if err != nil {
		return false, fmt.Errorf("cancelling instance %s: %w", instanceID, err)
	}
	return cancelled, nil
}

// recordDecision writes the ledger row and the trail entry for a step nobody
// performed, in the caller's unit of work, and makes them name each other: the
// entry is given its id as they are written, so the row can point at it before
// it exists.
//
// Separate from the migration entry because it is a separate fact, and the one
// an auditor actually asks about: not "this instance changed version" but "this
// approval did not happen, and here is who said so and why".
//
// Its error is the decision's: a skip, cancel or hold that cannot be recorded
// is not made. The row names its entry even in a wiring with no audit writer
// (tests only), where no entry is written.
func (s *migrationService) recordDecision(ctx context.Context, d decisionRecord) error {
	row := decisionDeviation(d, nodeNameIn(d.source.Nodes, d.nodeID))
	if _, err := s.actions.record(ctx, row, decisionEntry(d)); err != nil {
		return fmt.Errorf("recording that %q was %s: %w", d.nodeID, d.action.Kind, err)
	}
	return nil
}

// decisionEntry is the trail entry of a migration's node action, told the way
// it always has been.
func decisionEntry(d decisionRecord) entities.AuditEntry {
	actor := migrationActor(d.options)
	nodeID, action, target := d.nodeID, d.action, d.target
	eventType := EventNodeSkipped
	narrative := fmt.Sprintf(
		"%q was skipped without being performed, by %s, when %q moved from version %d to version %d. Reason: %s.",
		nodeID, actor, target.Key, d.source.Version, target.Version, action.Reason)
	switch action.Kind {
	case servicecontracts.NodeActionCancel:
		eventType = EventInstanceCancelled
		narrative = fmt.Sprintf(
			"This instance was ended at %q by %s, rather than moved to version %d of %q. Reason: %s.",
			nodeID, actor, target.Version, target.Key, action.Reason)
	case servicecontracts.NodeActionHold:
		eventType = EventInstanceHeld
		narrative = fmt.Sprintf(
			"This instance was held at %q by %s rather than moved to version %d of %q, and raised as an incident for somebody to decide. Reason: %s.",
			nodeID, actor, target.Version, target.Key, action.Reason)
	}
	return entities.AuditEntry{
		Type:      eventType,
		Message:   fmt.Sprintf("%s %s during migration", action.Kind, nodeID),
		Narrative: narrative,
		Timestamp: time.Now(),
		Data: map[string]any{
			"node_id": nodeID,
			"run_id":  d.runID.String(),
			"action":  string(action.Kind),
			"reason":  action.Reason,
			"actor":   actor,
		},
		Project:  &entities.Project{ID: uuid.UUID(d.instance.ProjectID)},
		Instance: &entities.ProcessInstance{ID: uuid.UUID(d.instance.ID)},
		Node:     &entities.Node{ID: nodeID},
	}
}

// holdsWork reports whether an instance has a token parked on nodeID.
//
// Tokens rather than tasks: a token is where the instance actually is, and a
// service task or a timer has no task row at all.
func holdsWork(instance models.ProcessInstanceModel, nodeID string) bool {
	for _, token := range instance.Tokens {
		if token.NodeID == nodeID {
			return true
		}
	}
	return false
}

// onVersion reports whether an instance runs the version with this id.
func onVersion(instance models.ProcessInstanceModel, definitionID uuid.UUID) bool {
	return uuid.UUID(instance.DefinitionID) == definitionID
}

// runs is onVersion for the instance as the engine reads it. One whose version
// cannot be told is not taken to run this one.
func runs(instance entities.ProcessInstance, definitionID uuid.UUID) bool {
	return instance.Definition != nil && instance.Definition.ID == definitionID
}

// stillWaitingAt is the question a cancel and a hold put to the row their lock
// returned: is this still one of the instances the decision is about — running,
// on the version being migrated from, and holding a token on the step.
func stillWaitingAt(locked models.ProcessInstanceModel, source models.ProcessDefinitionModel, nodeID string) bool {
	return locked.Status == models.ProcessActive && onVersion(locked, uuid.UUID(source.ID)) && holdsWork(locked, nodeID)
}

// parkedOn is holdsWork for the instance as the engine reads it, and asks as
// well that it is still running: the question a skip puts to the row its lock
// returned.
func parkedOn(instance entities.ProcessInstance, nodeID string) bool {
	if instance.Status != entities.ProcessActive {
		return false
	}
	for _, token := range instance.Tokens {
		if token.Node != nil && token.Node.ID == nodeID {
			return true
		}
	}
	return false
}

// openTask reports whether a task is still somebody's to do.
func openTask(status models.TaskStatus) bool {
	return status == models.TaskUnclaimed || status == models.TaskClaimed || status == models.TaskDelegated
}

// finishedJob reports whether a job has run and will not run again. Only a
// completed one: a pending or running job is work, and a failed one is work
// too, queued again when its incident is resolved.
func finishedJob(status models.JobStatus) bool {
	return status == models.JobCompleted
}

// remapSubscriptions moves an instance's waiting events onto the new graph.
//
// A subscription is a promise that a message or signal arriving later will find
// the instance waiting for it. Left naming a node the new version does not
// have, that promise is quietly broken: the message arrives, correlates to
// nothing, and the sender is never told.
//
// One with nowhere to land is closed rather than carried, which is what Camunda
// does with an unmapped catch event too — a subscription to an event no graph
// can receive is not a promise, it is a row.
//
// Re-pointing is delete-then-create because the repository has no update for
// the node: a subscription is identified by where it waits, so changing that is
// a different subscription. The new row takes a new id for the same reason.
func (s *migrationService) remapSubscriptions(
	ctx context.Context,
	instanceID uuid.UUID,
	nodeMapping map[string]string,
	targetNodes map[string]models.FlowNode,
) error {
	subs, err := s.repo.Subscription().ListByInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	for _, sub := range subs {
		mapped := mapNode(nodeMapping, sub.NodeID)
		if mapped == sub.NodeID {
			continue
		}
		if err := s.repo.Subscription().Delete(ctx, uuid.UUID(sub.ID)); err != nil {
			return err
		}
		if _, ok := targetNodes[mapped]; !ok {
			continue
		}
		sub.ID = models.UUID(uuid.New())
		sub.NodeID = mapped
		if err := s.repo.Subscription().Create(ctx, sub); err != nil {
			return err
		}
	}
	return nil
}

// plannedFor is the instances a plan was made for, by id.
func plannedFor(instances []models.ProcessInstanceModel) map[uuid.UUID]struct{} {
	planned := make(map[uuid.UUID]struct{}, len(instances))
	for _, instance := range instances {
		planned[uuid.UUID(instance.ID)] = struct{}{}
	}
	return planned
}

// selectInstances narrows a list to the ones named, and reports any name that
// matched nothing.
//
// An empty selection means everything, which is what a migration without one
// has always meant.
func selectInstances(all []models.ProcessInstanceModel, wanted []uuid.UUID) (kept []models.ProcessInstanceModel, missing []string) {
	if len(wanted) == 0 {
		return all, nil
	}
	want := make(map[uuid.UUID]struct{}, len(wanted))
	for _, id := range wanted {
		want[id] = struct{}{}
	}
	found := make(map[uuid.UUID]struct{}, len(wanted))
	for _, instance := range all {
		id := uuid.UUID(instance.ID)
		if _, ok := want[id]; !ok {
			continue
		}
		found[id] = struct{}{}
		kept = append(kept, instance)
	}
	for _, id := range wanted {
		if _, ok := found[id]; !ok {
			missing = append(missing, id.String())
		}
	}
	slices.Sort(missing)
	return kept, missing
}

// holdInstance leaves an instance exactly where it is and raises an incident
// against it.
//
// Nothing is moved, cancelled or advanced on purpose. A hold is the case where
// the right answer is "a person needs to look at this one", and the worst thing
// to do with that is guess — so this only changes where it is visible, not what
// it is.
//
// It reports whether the instance is held at nodeID: true when it raised the
// incident and when one was already open there, false for an instance found,
// once locked, to have finished or to have left the step — nothing is raised
// or recorded for that one.
func (s *migrationService) holdInstance(
	ctx context.Context,
	instance models.ProcessInstanceModel,
	nodeID string,
	action servicecontracts.NodeAction,
	options servicecontracts.MigrationOptions,
	source, target models.ProcessDefinitionModel,
	runID uuid.UUID,
) (held bool, err error) {
	instanceID := uuid.UUID(instance.ID)
	// One unit of work with the instance locked, as a cancel takes it: a hold
	// waits behind a completion racing it, and does nothing to an instance that
	// finished meanwhile or that the completion moved off the step. The
	// incident, its ledger row and its trail entry go in together, so a hold
	// that cannot be recorded raises nothing.
	err = s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		fresh, err := s.repo.Process().GetForUpdate(txCtx, instanceID)
		if err != nil {
			return fmt.Errorf("reading instance %s to hold it: %w", instanceID, err)
		}
		if held = stillWaitingAt(fresh, source, nodeID); !held {
			return nil
		}
		incidentID, raised, err := s.raiseHoldIncident(txCtx, fresh, nodeID, action, options, source, target)
		if err != nil || !raised {
			return err
		}
		if err := s.recordDecision(txCtx, decisionRecord{
			instance: fresh, definitionID: uuid.UUID(fresh.DefinitionID), source: source, target: target,
			nodeID: nodeID, action: action, options: options, runID: runID, incidentID: incidentID,
		}); err != nil {
			return fmt.Errorf("holding instance %s: %w", instanceID, err)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return held, nil
}

// raiseHoldIncident raises the incident that holds an instance at nodeID, and
// reports whether it raised one: an instance already held there keeps its one
// open incident, and its id is answered with false.
func (s *migrationService) raiseHoldIncident(
	ctx context.Context,
	instance models.ProcessInstanceModel,
	nodeID string,
	action servicecontracts.NodeAction,
	options servicecontracts.MigrationOptions,
	source, target models.ProcessDefinitionModel,
) (uuid.UUID, bool, error) {
	// A held instance stays on the source version, so a second run of the same
	// migration finds it again: the incident already open on the step is kept,
	// and a second one is not raised beside it.
	message := fmt.Sprintf(
		"held out of the migration from version %d to version %d of %q by %s: %s",
		source.Version, target.Version, target.Key, migrationActor(options), action.Reason)
	return s.actions.hold(ctx, instance, nodeID, message)
}

// disarmedDutyWarnings reports separation-of-duties rules the new version has
// left pointing at a step it no longer has.
//
// The rule survives the edit and stops meaning anything: "approve may not be
// done by whoever did opsApprove" is satisfied by everybody once opsApprove is
// gone, because nobody did it. That is a control weakening itself quietly,
// which is the shape auditors find and nobody else does — so the plan says it
// out loud rather than leaving the property sitting there looking enforced.
func disarmedDutyWarnings(targetNodes map[string]models.FlowNode) []string {
	var out []string
	for _, id := range sortedKeys(targetNodes) {
		node := targetNodes[id]
		for _, conflict := range splitNodeList(stringProperty(node.Properties, SeparationOfDutiesKey)) {
			if _, ok := targetNodes[conflict]; ok {
				continue
			}
			out = append(out, fmt.Sprintf(
				"%q may not be performed by whoever performed %q, and the new version has no %q — "+
					"the rule survives the edit and can no longer refuse anybody",
				id, conflict, conflict))
		}
	}
	slices.Sort(out)
	return out
}
