package impl

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/bcrypt"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/userimport"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
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

func (s *workflowUserService) PageWorkflowUsers(ctx context.Context, projectID uuid.UUID, search string, page repocontracts.Pagination) (repocontracts.Page[entities.WorkflowUser], error) {
	if projectID == uuid.Nil {
		return repocontracts.NewPage([]entities.WorkflowUser{}, 0, page), apierr.Invalidf("a project is required")
	}
	return s.participants.ListByProjectPaged(ctx, projectID, search, page)
}

func (s *workflowUserService) FindWorkflowUserByExternalID(ctx context.Context, projectID uuid.UUID, externalID string) (entities.WorkflowUser, error) {
	if projectID == uuid.Nil {
		return entities.WorkflowUser{}, apierr.Invalidf("a project is required")
	}
	if externalID == "" {
		return entities.WorkflowUser{}, apierr.Invalidf("an external ID is required")
	}
	return s.participants.GetByExternalID(ctx, projectID, externalID)
}

func (s *workflowUserService) ListWorkflowGroups(ctx context.Context, projectID uuid.UUID) ([]entities.WorkflowGroup, error) {
	if projectID == uuid.Nil {
		return nil, apierr.Invalidf("a project is required")
	}
	return s.participants.ListGroups(ctx, projectID)
}

// ImportWorkflowUsers reads a CSV of participants into a project.
func (s *workflowUserService) ImportWorkflowUsers(ctx context.Context, projectID uuid.UUID, csv io.Reader) (entities.ImportSummary, error) {
	body, err := io.ReadAll(csv)
	if err != nil {
		return entities.ImportSummary{}, apierr.Invalidf("could not read the file: %v", err)
	}
	return s.ImportDirectory(ctx, projectID, entities.DirectoryImport{Kind: "csv", CSV: body})
}

// SyncWorkflowUsersFromHTTP reads a directory from a JSON endpoint.
func (s *workflowUserService) SyncWorkflowUsersFromHTTP(ctx context.Context, projectID uuid.UUID, url, method string) (entities.ImportSummary, error) {
	return s.ImportDirectory(ctx, projectID, entities.DirectoryImport{Kind: "http", URL: url, Method: method})
}

// SyncWorkflowUsersFromPostgres reads a directory from a query against another
// database. See sourceFor for who may.
func (s *workflowUserService) SyncWorkflowUsersFromPostgres(ctx context.Context, projectID uuid.UUID, dsn, query string) (entities.ImportSummary, error) {
	return s.ImportDirectory(ctx, projectID, entities.DirectoryImport{Kind: "postgres", DSN: dsn, Query: query})
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

// SetWorkflowUserPassword gives a person a password for the People login.
//
// A person has no other way to get one: an import carries names, never
// passwords. The new one ends every People session they had, as any password
// change does, and the change is logged with who made it. The password is not.
// The rules for it are a User's: long enough to replace one, short enough for
// bcrypt to read whole.
func (s *workflowUserService) SetWorkflowUserPassword(ctx context.Context, projectID, id uuid.UUID, password string) error {
	if projectID == uuid.Nil {
		return apierr.Invalidf("a project is required")
	}
	if id == uuid.Nil {
		return apierr.Invalidf("a participant is required")
	}
	if err := checkNewPassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("could not hash the new password: %w", err)
	}
	if err := s.participants.SetPassword(ctx, projectID, id, string(hash)); err != nil {
		return err
	}
	by := ""
	if caller := signedIn(ctx); caller != nil {
		by = caller.Username
	}
	log.Info().
		Str("project_id", projectID.String()).
		Str("participant_id", id.String()).
		Str("set_by", by).
		Msg("A person's password for the People login was set; their earlier sessions have ended")
	return nil
}
