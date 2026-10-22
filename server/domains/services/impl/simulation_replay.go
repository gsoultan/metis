package impl

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// Replay — a real instance's case, run again on another version.
//
// The question it answers is the one a designer has before deploying: "the
// instance that went wrong last week, or the one stuck right now — what would
// it have done on this version?". So the case is read off the instance rather
// than typed: what it was started with, who did each of its steps and how long
// they took, and what each service told it. A wait for a message is left
// unanswered: when one arrived is not kept, so the run stops there and asks,
// as any case does. Then the run is compared with the
// path the instance actually took.
//
// Everything here is read, through the same tenant-scoped repositories every
// other read goes through. Nothing about the instance is changed; the run
// itself happens in the simulation's rolled-back transaction like any other.

// replayTasksRead bounds the tasks read off one instance. A replay answers
// each step once, so more than a page of tasks is an instance that looped,
// and its first visits are what a replay can use.
const replayTasksRead = repocontracts.MaxPageSize

// readReplay reads the case a real instance makes and lays the request over
// it: variables and answers the request gives win over what was read.
//
// It returns the key of the process the instance belongs to, which the run
// must be a version of — replaying an expense claim on a holiday request
// answers nothing.
func (s *simulationService) readReplay(ctx context.Context, req *entities.SimulationRequest) (*entities.SimulationReplay, string, error) {
	m, err := s.repo.Process().Get(ctx, req.ReplayInstanceID)
	if err != nil {
		return nil, "", err
	}
	// One in another project is answered as not there: the caller named a
	// project, and the instance is not in it.
	if uuid.UUID(m.ProjectID) != req.ProjectID {
		return nil, "", fmt.Errorf("%w: this project has no such process instance", apierr.ErrNotFound)
	}
	defModel, err := s.repo.Definition().Get(ctx, uuid.UUID(m.DefinitionID))
	if err != nil {
		return nil, "", fmt.Errorf("could not read the version the instance ran on: %w", err)
	}
	def := adapters.DefinitionEntityAdapter{Model: defModel}.ToEntity()
	instance := adapters.InstanceEntityAdapter{Model: m}.ToEntity()

	replay := &entities.SimulationReplay{
		InstanceID:     req.ReplayInstanceID,
		Version:        def.Version,
		Status:         string(instance.Status),
		StartedWith:    map[string]any{},
		Answers:        []entities.SimulationAnswer{},
		Path:           []string{},
		OnlyInRun:      []string{},
		OnlyInInstance: []string{},
		Notes:          []string{},
	}

	if err := s.readStart(ctx, replay); err != nil {
		return nil, "", err
	}
	visits, err := s.repo.Audit().NodeVisits(ctx, req.ReplayInstanceID, entities.EventNodeReached)
	if err != nil {
		return nil, "", err
	}
	for _, visit := range visits {
		replay.Path = append(replay.Path, visit.NodeID)
	}
	if err := s.readPeople(ctx, replay, instance); err != nil {
		return nil, "", err
	}
	if err := s.readServices(ctx, replay, def); err != nil {
		return nil, "", err
	}

	req.Variables = layered(replay.StartedWith, req.Variables)
	req.Answers = answersLayered(replay.Answers, req.Answers)
	if req.ClockStart.IsZero() {
		// Its own start, so a timer or a due date falls on the day it did.
		req.ClockStart = m.CreatedAt
	}
	return replay, def.Key, nil
}

// readStart reads what the instance was started with, from the entry its
// start wrote.
func (s *simulationService) readStart(ctx context.Context, replay *entities.SimulationReplay) error {
	entry, found, err := s.repo.Audit().FirstByInstance(ctx, replay.InstanceID, entities.EventProcessStarted)
	if err != nil {
		return err
	}
	if !found {
		replay.Notes = append(replay.Notes,
			"Nothing recorded what this instance was started with, so the run starts with nothing set.")
		return nil
	}
	maps.Copy(replay.StartedWith, entry.Data)
	return nil
}

// readPeople answers each step a person completed: who, how long after it
// opened, and what their task's form let them set.
//
// What they entered is not kept per task, so it is read from what the
// instance holds now, for the fields their form declares. A later step may
// have changed one of those since, which the notes say.
func (s *simulationService) readPeople(ctx context.Context, replay *entities.SimulationReplay, instance entities.ProcessInstance) error {
	page, err := s.repo.Task().ListByInstancePaged(ctx, replay.InstanceID,
		repocontracts.Pagination{Page: 1, PageSize: replayTasksRead})
	if err != nil {
		return fmt.Errorf("could not read the instance's tasks: %w", err)
	}
	tasks := page.Items
	slices.SortStableFunc(tasks, func(a, b models.TaskModel) int { return a.CreatedAt.Compare(b.CreatedAt) })

	answered := map[string]bool{}
	readValues := false
	for _, task := range tasks {
		if task.Status != models.TaskStatus(entities.TaskCompleted) || answered[task.NodeID] {
			continue
		}
		answered[task.NodeID] = true
		answer := entities.SimulationAnswer{
			Node:  task.NodeID,
			Kind:  entities.AnswerPerson,
			Actor: task.Assignee,
			After: isoDuration(task.UpdatedAt.Sub(task.CreatedAt)),
		}
		declared := map[string]struct{}{}
		forms := func() repocontracts.FormRepository { return s.repo.Form() }
		if err := addDeclaredFieldIDs(ctx, forms, declared, task, nil); err != nil {
			return err
		}
		for name := range declared {
			if value, ok := instance.Variables[name]; ok {
				if answer.Returns == nil {
					answer.Returns = map[string]any{}
				}
				answer.Returns[name] = value
				readValues = true
			}
		}
		replay.Answers = append(replay.Answers, answer)
	}
	if readValues {
		replay.Notes = append(replay.Notes,
			"What people entered on their tasks is read from what the instance holds now. "+
				"If a later step changed one of those values, set it on that answer.")
	}
	if page.Total > int64(len(tasks)) {
		replay.Notes = append(replay.Notes, fmt.Sprintf(
			"The instance has %d tasks; the first %d were read.", page.Total, len(tasks)))
	}
	return nil
}

// readServices answers each service step the instance reached with what the
// service told it.
func (s *simulationService) readServices(ctx context.Context, replay *entities.SimulationReplay, def *entities.ProcessDefinition) error {
	for _, nodeID := range replay.Path {
		node := def.FindNode(nodeID)
		if node == nil || node.Type != entities.ServiceTask {
			continue
		}
		call, err := s.repo.ServiceCall().Get(ctx, replay.InstanceID, nodeID, "")
		switch {
		case errors.Is(err, apierr.ErrNotFound):
			replay.Notes = append(replay.Notes, fmt.Sprintf(
				"Nothing recorded what %s answered, so the run stops there to ask.", nodeLabel(node)))
			continue
		case err != nil:
			return fmt.Errorf("could not read what %s answered: %w", nodeLabel(node), err)
		case call.Status != models.ServiceCallCompleted:
			replay.Notes = append(replay.Notes, fmt.Sprintf(
				"%s had not answered yet, so the run stops there to ask.", nodeLabel(node)))
			continue
		}
		replay.Answers = append(replay.Answers, entities.SimulationAnswer{
			Node:    nodeID,
			Kind:    entities.AnswerService,
			Returns: call.Response,
		})
	}
	return nil
}

// compareReplay says where the run went and the instance did not, and the
// other way round. Steps are matched by id, which is what a version edited in
// the designer keeps.
func compareReplay(replay *entities.SimulationReplay, steps []entities.SimulationStep) {
	inInstance := map[string]bool{}
	for _, nodeID := range replay.Path {
		inInstance[nodeID] = true
	}
	inRun := map[string]bool{}
	for _, step := range steps {
		if step.Node == "" || step.Event != "entered" || inRun[step.Node] {
			continue
		}
		inRun[step.Node] = true
		if !inInstance[step.Node] {
			replay.OnlyInRun = append(replay.OnlyInRun, step.Node)
		}
	}
	for _, nodeID := range replay.Path {
		if !inRun[nodeID] {
			replay.OnlyInInstance = append(replay.OnlyInInstance, nodeID)
		}
	}
}

// layered is base with over laid on top, as a new map.
func layered(base, over map[string]any) map[string]any {
	out := maps.Clone(base)
	if out == nil {
		out = map[string]any{}
	}
	maps.Copy(out, over)
	return out
}

// answersLayered keeps every answer the request gave and adds those read off
// the instance for the steps the request left alone.
func answersLayered(read, given []entities.SimulationAnswer) []entities.SimulationAnswer {
	out := slices.Clone(given)
	for _, answer := range read {
		if !slices.ContainsFunc(given, func(g entities.SimulationAnswer) bool { return g.Node == answer.Node }) {
			out = append(out, answer)
		}
	}
	return out
}

// isoDuration writes a span as the ISO-8601 duration an answer takes, in the
// largest units that say it exactly: PT1H rather than PT3600S, which is what
// the designer offers and so what it can show.
func isoDuration(d time.Duration) string {
	secs := int64(d / time.Second)
	if secs <= 0 {
		return "PT0S"
	}
	days, secs := secs/86400, secs%86400
	hours, secs := secs/3600, secs%3600
	minutes, secs := secs/60, secs%60

	out := "P"
	if days > 0 {
		out += fmt.Sprintf("%dD", days)
	}
	if hours == 0 && minutes == 0 && secs == 0 {
		return out
	}
	out += "T"
	if hours > 0 {
		out += fmt.Sprintf("%dH", hours)
	}
	if minutes > 0 {
		out += fmt.Sprintf("%dM", minutes)
	}
	if secs > 0 {
		out += fmt.Sprintf("%dS", secs)
	}
	return out
}
