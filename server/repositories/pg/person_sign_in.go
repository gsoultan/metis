package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/workflowuser"
)

type personSignInRepository struct {
	conn *db.Conn
}

// NewPersonSignInRepository returns the storm-backed reads the People sign-in
// makes. Removed people are not found: the table's soft delete is compiled into
// every read of it.
//
// Always the main database. The directory is the project's, installation-wide
// like accounts; a request arriving on an environment's port would otherwise
// read that environment's empty copy of the table and sign nobody in.
func NewPersonSignInRepository(conn *db.Conn) contracts.PersonSignInRepository {
	return &personSignInRepository{conn: conn}
}

func (r *personSignInRepository) GetForSignIn(ctx context.Context, projectID uuid.UUID, username string) (entities.WorkflowUser, string, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return entities.WorkflowUser{}, "", err
	}
	row, found, err := workflowuser.New().
		Where(workflowuser.ProjectID.Eq(projectID), workflowuser.Username.Eq(username)).
		One(ctx, ex)
	if err != nil {
		return entities.WorkflowUser{}, "", fmt.Errorf("could not read participant %q: %w", username, err)
	}
	if !found {
		return entities.WorkflowUser{}, "", fmt.Errorf("%w: no participant %q in this project", apierr.ErrNotFound, username)
	}
	return participantFrom(row), valueOr(row.PasswordHash), nil
}

func (r *personSignInRepository) GetSession(ctx context.Context, id uuid.UUID) (entities.WorkflowUser, time.Time, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return entities.WorkflowUser{}, time.Time{}, err
	}
	row, found, err := workflowuser.New().Where(workflowuser.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return entities.WorkflowUser{}, time.Time{}, fmt.Errorf("could not read participant %s: %w", id, err)
	}
	if !found {
		return entities.WorkflowUser{}, time.Time{}, fmt.Errorf("%w: no participant %s", apierr.ErrNotFound, id)
	}
	validFrom, _ := row.TokensValidFrom.Get()
	return participantFrom(row), validFrom, nil
}
