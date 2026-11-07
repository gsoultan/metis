package impl

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// ListApprovalsBehind lists, version by version, the open requests still on
// a line's versions older than the live one, and what moving each version's
// requests onto the live version would do.
//
// It changes nothing. A new version never moves a request by itself: a
// request keeps the line it was submitted on until somebody moves it here.
func (s *approvalLineService) ListApprovalsBehind(ctx context.Context, projectID uuid.UUID, key string) (entities.ApprovalTakeover, error) {
	if s.moves == nil {
		return entities.ApprovalTakeover{}, errNoMoves
	}
	live, older, err := s.lineVersions(ctx, projectID, key)
	if err != nil {
		return entities.ApprovalTakeover{}, err
	}
	out := entities.ApprovalTakeover{Key: key, LiveVersion: live.Version, Behind: []entities.ApprovalBehind{}}
	for _, def := range older {
		line, _ := approvalLineOf(def)
		requests, ids, err := s.openRequests(ctx, projectID, def, line)
		if err != nil {
			return entities.ApprovalTakeover{}, err
		}
		if len(requests) == 0 {
			continue
		}
		plan, err := s.moves.PlanInstanceMigration(ctx, def.ID, live.ID, nil, servicecontracts.WithInstances(ids...))
		if err != nil {
			return entities.ApprovalTakeover{}, err
		}
		out.Behind = append(out.Behind, entities.ApprovalBehind{
			Version: def.Version, DefinitionID: def.ID, Requests: requests, Plan: plan,
		})
	}
	return out, nil
}

// MoveApprovals moves the named open requests onto the live version, by the
// migration, as actor.
//
// The requests are moved version by version. A version whose move the
// migration refuses as a whole — a step's work with nowhere to land, an
// approval it would take away, a move that needs a second administrator —
// moves none of its requests; they are left, with the refusal, for the
// designer, where it can be given what it asks for. A request already
// finished or not on an older version is left, and said so.
func (s *approvalLineService) MoveApprovals(ctx context.Context, projectID uuid.UUID, key string, instanceIDs []uuid.UUID, actor string) (entities.ApprovalMoveResult, error) {
	if s.moves == nil {
		return entities.ApprovalMoveResult{}, errNoMoves
	}
	if strings.TrimSpace(actor) == "" {
		return entities.ApprovalMoveResult{}, apierr.Forbiddenf("a move is made by somebody")
	}
	if len(instanceIDs) == 0 {
		return entities.ApprovalMoveResult{}, apierr.Invalidf("name the requests to move")
	}
	if len(instanceIDs) > entities.MaxApprovalRequestsMoved {
		return entities.ApprovalMoveResult{}, apierr.Invalidf("move at most %d requests at once", entities.MaxApprovalRequestsMoved)
	}
	live, older, err := s.lineVersions(ctx, projectID, key)
	if err != nil {
		return entities.ApprovalMoveResult{}, err
	}

	wanted := make(map[uuid.UUID]bool, len(instanceIDs))
	for _, id := range instanceIDs {
		wanted[id] = true
	}
	var result entities.ApprovalMoveResult
	for _, def := range older {
		line, _ := approvalLineOf(def)
		_, open, err := s.openRequests(ctx, projectID, def, line)
		if err != nil {
			return entities.ApprovalMoveResult{}, err
		}
		var ids []uuid.UUID
		for _, id := range open {
			if wanted[id] {
				ids = append(ids, id)
				delete(wanted, id)
			}
		}
		if len(ids) == 0 {
			continue
		}
		plan, err := s.moves.PlanInstanceMigration(ctx, def.ID, live.ID, nil, servicecontracts.WithInstances(ids...))
		if err != nil {
			return entities.ApprovalMoveResult{}, err
		}
		if why := planStops(plan); why != "" {
			for _, id := range ids {
				result.Left = append(result.Left, entities.ApprovalLeft{InstanceID: id, Why: why})
			}
			continue
		}
		run, err := s.moves.ApplyInstanceMigration(ctx, def.ID, live.ID, nil,
			servicecontracts.WithInstances(ids...), servicecontracts.WithActor(actor),
			// A line changes to change who decides a step: a moved request's
			// open step goes to the new line's approvers.
			servicecontracts.WithTasksRederived())
		if err != nil {
			why := err.Error()
			for _, id := range ids {
				result.Left = append(result.Left, entities.ApprovalLeft{InstanceID: id, Why: why})
			}
			continue
		}
		result.Moved += run.Changed
		for _, passed := range run.PassedOver {
			if passed.Instance != nil {
				result.Left = append(result.Left, entities.ApprovalLeft{InstanceID: passed.Instance.ID, Why: string(passed.Cause)})
			}
		}
	}
	for id := range wanted {
		result.Left = append(result.Left, entities.ApprovalLeft{
			InstanceID: id, Why: fmt.Sprintf("not an open request on an older version of %s", key),
		})
	}
	slices.SortFunc(result.Left, func(a, b entities.ApprovalLeft) int {
		return strings.Compare(a.InstanceID.String(), b.InstanceID.String())
	})
	return result, nil
}

// planStops says why a plan cannot be applied here, or "": a refusal, an
// approval the move would take away, or a decision it would make for a step.
// Each of those wants a person to say so by name, which the designer asks
// for and a move of requests does not.
func planStops(plan entities.MigrationPlan) string {
	switch {
	case len(plan.Refusals) > 0:
		return plan.Refusals[0]
	case len(plan.ComplianceHolds) > 0:
		return "the move would take an approval away from requests that have not given it; plan it in the designer"
	case len(plan.Actions) > 0:
		return "the move would decide a step's work rather than move it; plan it in the designer"
	case plan.RequiresSecondApprover:
		return "the move needs a second administrator; ask for it in the designer"
	}
	return ""
}

// lineVersions is the line's live version and its older ones, newest first,
// each with its graph.
func (s *approvalLineService) lineVersions(ctx context.Context, projectID uuid.UUID, key string) (*entities.ProcessDefinition, []*entities.ProcessDefinition, error) {
	live, found, err := s.live(ctx, projectID, key)
	if err != nil {
		return nil, nil, err
	}
	if _, isLine := approvalLineOf(live); !found || !isLine {
		return nil, nil, fmt.Errorf("%w: no approval line %s in this project", apierr.ErrNotFound, key)
	}
	defs, err := s.defs.ListDefinitions(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	var older []*entities.ProcessDefinition
	for _, d := range defs {
		if d == nil || d.Key != key || d.Version >= live.Version {
			continue
		}
		def := d
		if len(def.Nodes) == 0 {
			if def, err = s.defs.GetDefinition(ctx, d.ID); err != nil {
				return nil, nil, err
			}
		}
		if _, isLine := approvalLineOf(def); isLine {
			older = append(older, def)
		}
	}
	slices.SortFunc(older, func(a, b *entities.ProcessDefinition) int { return b.Version - a.Version })
	return live, older, nil
}

// openRequests is a version's running requests, oldest first, and their IDs.
func (s *approvalLineService) openRequests(ctx context.Context, projectID uuid.UUID, def *entities.ProcessDefinition, line entities.ApprovalLine) ([]entities.ApprovalRequest, []uuid.UUID, error) {
	var requests []entities.ApprovalRequest
	var ids []uuid.UUID
	for page := 1; page <= 20; page++ {
		got, err := s.engine.ListInstancesPaged(ctx, projectID,
			repocontracts.InstanceFilter{DefinitionID: def.ID, Status: models.ProcessActive},
			repocontracts.Pagination{Page: page, PageSize: 100})
		if err != nil {
			return nil, nil, err
		}
		for _, instance := range got.Items {
			requests = append(requests, approvalRequestOf(line, instance, nil))
			ids = append(ids, instance.ID)
		}
		if int64(page*100) >= got.Total || len(got.Items) == 0 {
			break
		}
	}
	slices.SortStableFunc(requests, func(a, b entities.ApprovalRequest) int { return a.SubmittedAt.Compare(b.SubmittedAt) })
	return requests, ids, nil
}

// errNoMoves is the answer of a server wired without migrations.
var errNoMoves = errors.New("this server cannot move requests between versions")
