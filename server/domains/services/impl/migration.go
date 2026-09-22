package impl

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

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
	// audit records that a migration happened. Without it the trail shows a task
	// completed on a node the instance was never started on, with nothing to
	// explain how it got there — and the OCEL export inherits that gap.
	audit servicecontracts.AuditWriter
}

// NewMigrationService creates a new MigrationService implementation.
func NewMigrationService(
	repo repositories.Repository,
) servicecontracts.MigrationService {
	s := &migrationService{repo: repo}
	if repo != nil && repo.Audit() != nil {
		s.audit = NewAuditWriter(repo.Audit())
	}
	return s
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
	// Every refusal is worked out by the planner, so what a dry run showed and
	// what an apply does cannot disagree. Two copies of these checks is how a
	// preview comes to say "this is fine" about something the apply rejects.
	plan, err := s.PlanInstanceMigration(ctx, sourceDefID, targetDefID, nodeMapping, opts...)
	if err != nil {
		return err
	}
	if !plan.Applicable() {
		return apierr.Invalidf("%s", strings.Join(plan.Refusals, "; "))
	}
	if plan.Instances == 0 {
		return nil
	}
	return s.apply(ctx, sourceDefID, targetDefID, nodeMapping, servicecontracts.ApplyMigrationOptions(opts), plan)
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
	options := servicecontracts.ApplyMigrationOptions(opts)
	var plan entities.MigrationPlan
	if sourceDefID == targetDefID {
		return plan, apierr.Invalidf("the source and target versions are the same definition")
	}

	source, err := s.repo.Definition().Get(ctx, sourceDefID)
	if err != nil {
		return plan, fmt.Errorf("source definition: %w", err)
	}
	target, err := s.repo.Definition().Get(ctx, targetDefID)
	if err != nil {
		return plan, fmt.Errorf("target definition: %w", err)
	}
	plan.SourceKey = source.Key
	plan.SourceVersion = source.Version
	plan.TargetVersion = target.Version
	plan.TargetID = targetDefID

	// Same process, or it is not a version change. Migrating "expense-approval"
	// onto "supplier-onboarding" was previously accepted and left every instance
	// claiming to be running a process it had never started.
	if source.Key != target.Key {
		return plan, apierr.Invalidf("cannot migrate %q onto %q: they are different processes, not two versions of one",
			source.Key, target.Key)
	}
	if source.ProjectID != target.ProjectID {
		return plan, apierr.Invalidf("cannot migrate between projects")
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
		return plan, apierr.Invalidf("version %d of %q has no node for %s",
			target.Version, target.Key, strings.Join(badTargets, ", "))
	}

	// A boundary event guards a particular activity. Moving it to one that
	// guards something else is the timer firing against work that is not
	// running — checked here because it depends only on the two graphs.
	plan.Refusals = append(plan.Refusals, boundaryRefusals(sourceNodes, targetNodes, nodeMapping)...)

	// Which nodes the new version dropped altogether. Shown whether or not any
	// instance is sitting on one: it is the first thing somebody reviewing a
	// cutover wants to see, and a removed user task is a removed producer of
	// whatever its form used to write.
	plan.RemovedNodes = removedNodes(sourceNodes, targetNodes)

	instances, err := s.repo.Process().ListByDefinition(ctx, sourceDefID)
	if err != nil {
		return plan, fmt.Errorf("failed to list instances for migration: %w", err)
	}
	plan.Instances = len(instances)
	if len(instances) == 0 {
		return plan, nil
	}

	// Every place the instances currently sit has to land on a node the target
	// actually has — mapped there explicitly, or carried over because the target
	// still has a node of that name. An unlisted one is exactly the token the
	// old code stranded.
	found, err := s.survey(ctx, instances, targetNodes, nodeMapping)
	if err != nil {
		return plan, err
	}
	plan.Moves = found.moves
	if len(found.unlandable) > 0 {
		plan.Refusals = append(plan.Refusals, fmt.Sprintf(
			"version %d of %q has nowhere to put the work parked on %s; map each one to a node it does have",
			target.Version, target.Key, strings.Join(found.unlandable, ", ")))
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
	plan.Warnings = append(plan.Warnings, claimWarnings(found.moves)...)

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
	return plan, nil
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
func (s *migrationService) apply(
	ctx context.Context,
	sourceDefID, targetDefID uuid.UUID,
	nodeMapping map[string]string,
	options servicecontracts.MigrationOptions,
	plan entities.MigrationPlan,
) error {
	source, err := s.repo.Definition().Get(ctx, sourceDefID)
	if err != nil {
		return fmt.Errorf("source definition: %w", err)
	}
	target, err := s.repo.Definition().Get(ctx, targetDefID)
	if err != nil {
		return fmt.Errorf("target definition: %w", err)
	}
	targetNodes := nodeIndex(target.Nodes)

	instances, err := s.repo.Process().ListByDefinition(ctx, sourceDefID)
	if err != nil {
		return fmt.Errorf("failed to list instances for migration: %w", err)
	}

	for _, instance := range instances {
		moved := map[string]string{}
		err := s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
			instance.DefinitionID = models.UUID(targetDefID)
			for i := range instance.Tokens {
				instance.Tokens[i].NodeID = mapNode(nodeMapping, instance.Tokens[i].NodeID)
			}
			// Everything else on the instance that is keyed by node id. These
			// are not cosmetic: Joins is what a parallel gateway counts arrived
			// branches in, MultiInstance is how far through its items a node
			// is, and CompletedNodes is the guard that stops an activity from
			// running twice. Leaving them pointing at the old graph deadlocks
			// the join, re-asks the multi-instance assignees, and lets a
			// service task fire a second time.
			instance.CompletedNodes = mapNodeList(nodeMapping, instance.CompletedNodes)
			instance.CompensatedNodes = mapNodeList(nodeMapping, instance.CompensatedNodes)
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
				job.DefinitionID = models.UUID(targetDefID)
				job.NodeID = mapNode(nodeMapping, job.NodeID)
				if err := s.repo.Job().Update(txCtx, job); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to migrate instance %s: %w", instance.ID, err)
		}
		s.recordMigration(ctx, instance, source, target, moved, options, plan)
	}

	return nil
}

// recordMigration writes the audit entry for one migrated instance.
//
// Outside the unit of work on purpose: a migration that succeeded should not be
// rolled back because the audit write failed, and an audit entry for a
// migration that did not happen is worse than a missing one. A lost entry is
// logged loudly because the trail is what somebody will be asked for later.
func (s *migrationService) recordMigration(
	ctx context.Context,
	instance models.ProcessInstanceModel,
	source, target models.ProcessDefinitionModel,
	moved map[string]string,
	options servicecontracts.MigrationOptions,
	plan entities.MigrationPlan,
) {
	if s.audit == nil {
		return
	}
	pairs := make([]string, 0, len(moved))
	for from, to := range moved {
		pairs = append(pairs, fmt.Sprintf("%s→%s", from, to))
	}
	slices.Sort(pairs)

	actor := options.Actor
	if actor == "" {
		actor = "System"
	}
	narrative := fmt.Sprintf("This instance was moved from version %d to version %d of %q by a migration, authorised by %s.",
		source.Version, target.Version, target.Key, actor)
	if len(pairs) > 0 {
		narrative += fmt.Sprintf(" Work in progress was re-pointed: %s.", strings.Join(pairs, ", "))
	}

	// Which control-bearing steps this instance lost, and only the ones it had
	// not already performed. An instance that passed the approval before the
	// cutover did not waive anything and must not read as though it did.
	var waived []string
	for _, hold := range plan.ComplianceHolds {
		if slices.Contains(instance.CompletedNodes, hold.NodeID) {
			continue
		}
		waived = append(waived, hold.NodeID)
	}
	if len(waived) > 0 {
		narrative += fmt.Sprintf(" It had not yet passed %s, and %s accepted that it never will.",
			strings.Join(waived, ", "), actor)
	}

	entry := entities.AuditEntry{
		ID:        uuid.New(),
		Type:      EventInstanceMigrated,
		Message:   fmt.Sprintf("migrated from version %d to version %d", source.Version, target.Version),
		Narrative: narrative,
		Timestamp: time.Now(),
		Data: map[string]any{
			"source_version":  source.Version,
			"target_version":  target.Version,
			"process_key":     target.Key,
			"task_moves":      pairs,
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
) (surveyResult, error) {
	type counts struct{ tokens, tasks, claimed, delegated, jobs int }
	byNode := map[string]*counts{}
	stranded := map[string]struct{}{}
	strandedState := map[string]struct{}{}
	// Which source nodes each target node would receive bookkeeping from. More
	// than one is an ambiguity nothing can resolve.
	mergedInto := map[string]map[string]struct{}{}
	result := surveyResult{landings: map[string]struct{}{}}

	lands := func(nodeID string) (string, bool) {
		to := mapNode(nodeMapping, nodeID)
		_, ok := targetNodes[to]
		return to, ok
	}

	count := func(nodeID string, add func(*counts)) {
		if nodeID == "" {
			return
		}
		to, ok := lands(nodeID)
		if !ok {
			stranded[nodeID] = struct{}{}
		} else {
			result.landings[to] = struct{}{}
		}
		c, ok := byNode[nodeID]
		if !ok {
			c = &counts{}
			byNode[nodeID] = c
		}
		add(c)
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
			count(job.NodeID, func(c *counts) { c.jobs++ })
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
			Mapped:         mapped,
		})
	}
	// Sorted so the same plan reads the same way twice; a map's order would make
	// a preview look different every time it was refreshed.
	slices.SortFunc(result.moves, func(a, b entities.NodeMove) int { return strings.Compare(a.From, b.From) })

	result.unlandable = sortedKeys(stranded)
	result.stateStranded = sortedKeys(strandedState)

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
	task.FormDefinition = stringProperty(node.Properties, "form_definition")
	task.CandidateUsers = slices.Clone(node.CandidateUsers)
	task.CandidateGroups = slices.Clone(node.CandidateGroups)

	task.DueDate = nil
	if node.DueDate != "" {
		if due, err := time.Parse(time.RFC3339, node.DueDate); err == nil {
			task.DueDate = &due
		}
	}

	// The claim does not survive the move. Whoever held this task claimed the
	// step that was deleted, not the one they are now looking at.
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
func boundaryRefusals(sourceNodes, targetNodes map[string]models.FlowNode, nodeMapping map[string]string) []string {
	var out []string
	for from, to := range nodeMapping {
		src, ok := sourceNodes[from]
		if !ok || src.Type != models.BoundaryEvent || src.AttachedToRef == "" {
			continue
		}
		tgt, ok := targetNodes[to]
		if !ok || tgt.Type != models.BoundaryEvent {
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

// mapNodeList rewrites a list of node ids through the mapping.
//
// Entries with no mapping are kept rather than dropped: CompletedNodes is the
// record of what this instance actually ran, and a step the new version deleted
// is still a step this instance performed.
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
