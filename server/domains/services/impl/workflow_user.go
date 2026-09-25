package impl

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/userimport"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl/participantsource"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

type workflowUserService struct {
	participants repocontracts.WorkflowUserRepository
}

// NewWorkflowUserService returns the service that manages participants.
func NewWorkflowUserService(participants repocontracts.WorkflowUserRepository) servicecontracts.WorkflowUserService {
	return &workflowUserService{participants: participants}
}

func (s *workflowUserService) ListWorkflowUsers(ctx context.Context, projectID uuid.UUID, limit int) ([]entities.WorkflowUser, error) {
	if projectID == uuid.Nil {
		return nil, apierr.Invalidf("a project is required")
	}
	return s.participants.ListByProject(ctx, projectID, limit)
}

// ImportWorkflowUsers reads a CSV of participants into a project.
//
// Deliberately not one transaction. A file is a list of independent facts about
// independent people, and rolling the whole import back because row four
// hundred had a malformed address would discard three hundred and ninety-nine
// rows that were fine — while telling somebody their import "failed", which
// they would then have to reconcile by hand.
//
// What is atomic is each participant: their row and their group memberships go
// in together, so nobody ends up in a group they are not a member of.
func (s *workflowUserService) ImportWorkflowUsers(ctx context.Context, projectID uuid.UUID, csv io.Reader) (entities.ImportSummary, error) {
	if projectID == uuid.Nil {
		return entities.ImportSummary{}, apierr.Invalidf("a project is required")
	}

	parsed, err := userimport.Parse(csv)
	if err != nil {
		// The file itself is unusable — empty, or missing the one column that
		// identifies a person. That is a refusal, not a list of problems.
		return entities.ImportSummary{}, apierr.Invalidf("%s", err.Error())
	}

	return s.write(ctx, projectID, parsed), nil
}

// write saves what a source produced.
//
// An import is additive: somebody absent from the source is left alone, not
// deactivated. A partial file is far more common than a complete one, and
// taking people's work away because a file did not mention them is a change
// nobody asked for and nobody would see.
func (s *workflowUserService) write(ctx context.Context, projectID uuid.UUID, parsed userimport.Result) entities.ImportSummary {
	summary := entities.ImportSummary{Problems: problemsFrom(parsed.Problems)}
	// Groups already ensured in this file, so a directory where four hundred
	// people are in "approvers" looks the group up once rather than four
	// hundred times.
	groups := map[string]uuid.UUID{}

	for _, row := range parsed.Rows {
		created, err := s.participants.Upsert(ctx, entities.WorkflowUser{
			Project:     &entities.Project{ID: projectID},
			Username:    row.Username,
			DisplayName: row.DisplayName,
			Email:       row.Email,
			Active:      row.Active,
		})
		if err != nil {
			// One person failing is that row's problem, reported where somebody
			// can find it. The file carries on.
			summary.Problems = append(summary.Problems, entities.ImportProblem{
				Line:     row.Line,
				Username: row.Username,
				Reason:   reasonFor(err),
			})
			continue
		}
		if created {
			summary.Created++
		} else {
			summary.Updated++
		}

		if err := s.joinGroups(ctx, projectID, row, groups, &summary); err != nil {
			summary.Problems = append(summary.Problems, entities.ImportProblem{
				Line:     row.Line,
				Username: row.Username,
				Reason:   reasonFor(err),
			})
		}
	}

	return summary
}

// SyncWorkflowUsersFromHTTP reads a directory from a JSON endpoint.
func (s *workflowUserService) SyncWorkflowUsersFromHTTP(ctx context.Context, projectID uuid.UUID, url, method string) (entities.ImportSummary, error) {
	return s.importFrom(ctx, projectID, participantsource.NewHTTPSource(participantsource.HTTPConfig{
		URL:    url,
		Method: method,
	}))
}

// SyncWorkflowUsersFromPostgres reads a directory from a query against another
// database.
func (s *workflowUserService) SyncWorkflowUsersFromPostgres(ctx context.Context, projectID uuid.UUID, dsn, query string) (entities.ImportSummary, error) {
	return s.importFrom(ctx, projectID, participantsource.NewPostgresSource(participantsource.PostgresConfig{
		DSN:   dsn,
		Query: query,
	}))
}

// importFrom fetches from a source and writes what it found.
//
// Every source ends here, so an endpoint and a file produce the same summary
// and the same per-row problems — the sources differ only in where the rows
// came from.
func (s *workflowUserService) importFrom(ctx context.Context, projectID uuid.UUID, source participantsource.Source) (entities.ImportSummary, error) {
	if projectID == uuid.Nil {
		return entities.ImportSummary{}, apierr.Invalidf("a project is required")
	}
	parsed, err := source.Fetch(ctx)
	if err != nil {
		// The source could not be read at all. That is a refusal, not a list of
		// problems — nothing was imported and nothing partially landed.
		return entities.ImportSummary{}, apierr.Invalidf("%s", err.Error())
	}
	return s.write(ctx, projectID, parsed), nil
}

// joinGroups puts a participant in the groups their row named, creating any the
// project does not have yet.
//
// Importing a directory that mentions a team should not fail because nobody
// created the team first — the file is the statement of what the teams are.
func (s *workflowUserService) joinGroups(
	ctx context.Context,
	projectID uuid.UUID,
	row userimport.Row,
	groups map[string]uuid.UUID,
	summary *entities.ImportSummary,
) error {
	if len(row.Groups) == 0 {
		return nil
	}
	participant, err := s.participants.GetByUsername(ctx, projectID, row.Username)
	if err != nil {
		return err
	}
	for _, name := range row.Groups {
		groupID, known := groups[name]
		if !known {
			group, created, err := s.participants.EnsureGroup(ctx, projectID, name)
			if err != nil {
				return err
			}
			if created {
				summary.Groups++
			}
			groupID = group.ID
			groups[name] = groupID
		}
		if err := s.participants.AddToGroup(ctx, participant.ID, groupID); err != nil {
			return err
		}
	}
	return nil
}

func problemsFrom(problems []userimport.Problem) []entities.ImportProblem {
	if len(problems) == 0 {
		return nil
	}
	out := make([]entities.ImportProblem, 0, len(problems))
	for _, p := range problems {
		out = append(out, entities.ImportProblem{Line: p.Line, Username: p.Username, Reason: p.Reason})
	}
	return out
}

// reasonFor keeps a storage failure readable in a per-row report without
// leaking the query that produced it.
func reasonFor(err error) string {
	return fmt.Sprintf("could not be saved: %v", err)
}

// WorkflowUserServiceFor narrows the public service back to the concrete one.
//
// The syncer needs the write half, which is deliberately not on the contract —
// fetching a directory is not something a caller should be able to ask for row
// by row. Exported so a test can wire the same graph the composition root does.
func WorkflowUserServiceFor(svc servicecontracts.WorkflowUserService) *workflowUserService {
	concrete, ok := svc.(*workflowUserService)
	if !ok {
		// Loudly, because the alternative is what happened the first time this
		// was written: a nil receiver handed to the syncer, which does not fail
		// where it was created but much later, inside the write path, as a
		// hang with no error attached to it.
		panic(fmt.Sprintf("WorkflowUserServiceFor: expected the concrete service, got %T", svc))
	}
	return concrete
}

// RemoveWorkflowUser takes somebody out of a project's directory.
//
// The project is checked against the participant rather than trusted from the
// path: the id alone would let a caller who can manage one project's directory
// remove somebody from another's, and the reply would look identical either way.
func (s *workflowUserService) RemoveWorkflowUser(ctx context.Context, projectID, id uuid.UUID) error {
	if projectID == uuid.Nil {
		return apierr.Invalidf("a project is required")
	}
	if id == uuid.Nil {
		return apierr.Invalidf("a participant is required")
	}

	people, err := s.participants.ListByProject(ctx, projectID, 0)
	if err != nil {
		return err
	}
	for _, person := range people {
		if person.ID == id {
			return s.participants.Delete(ctx, id)
		}
	}
	// Not found rather than refused: from this caller's point of view there is
	// no such participant, which is also the honest answer when there is one and
	// they belong to somebody else.
	return fmt.Errorf("%w: no such participant in this project", apierr.ErrNotFound)
}
