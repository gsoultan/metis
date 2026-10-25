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
	"github.com/gsoultan/metis/server/repositories/store/tasklink"
)

type taskLinkRepository struct {
	conn *db.Conn
}

// NewTaskLinkRepository keeps task links beside their tasks: in the database
// of the environment the request is for, as the tasks are.
//
// Not scoped by tenant. A link is opened by somebody who is not signed in, so
// there is no tenant to scope it by; the token is the only key, and a link
// found by it says which project it is for.
func NewTaskLinkRepository(conn *db.Conn) contracts.TaskLinkRepository {
	return &taskLinkRepository{conn: conn}
}

func (r *taskLinkRepository) Create(ctx context.Context, link entities.TaskLink, tokenHash string) error {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return err
	}
	id := link.ID
	if id == uuid.Nil {
		id = uuid.Must(uuid.NewV7())
	}
	ins := tasklink.Create()
	ins.SetID(id)
	ins.SetProjectID(link.ProjectID)
	ins.SetTaskID(link.TaskID)
	ins.SetParticipantID(link.ParticipantID)
	ins.SetTokenHash(tokenHash)
	ins.SetExpiresAt(link.ExpiresAt)
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not record the task link: %w", err)
	}
	return nil
}

func (r *taskLinkRepository) GetByHash(ctx context.Context, tokenHash string) (entities.TaskLink, error) {
	return r.byHash(ctx, tokenHash, false)
}

func (r *taskLinkRepository) GetByHashForUpdate(ctx context.Context, tokenHash string) (entities.TaskLink, error) {
	return r.byHash(ctx, tokenHash, true)
}

func (r *taskLinkRepository) byHash(ctx context.Context, tokenHash string, forUpdate bool) (entities.TaskLink, error) {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return entities.TaskLink{}, err
	}
	q := tasklink.New().Where(tasklink.TokenHash.Eq(tokenHash))
	if forUpdate {
		q = q.ForUpdate()
	}
	row, found, err := q.One(ctx, ex)
	if err != nil {
		return entities.TaskLink{}, fmt.Errorf("could not read the task link: %w", err)
	}
	if !found {
		return entities.TaskLink{}, fmt.Errorf("%w: no such task link", apierr.ErrNotFound)
	}
	return taskLinkFrom(row), nil
}

func (r *taskLinkRepository) MarkUsed(ctx context.Context, id uuid.UUID, at time.Time) error {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return err
	}
	var mut tasklink.Mut
	mut.SetUsedAt(at)
	mut.SetUpdatedAtNow()
	n, err := tasklink.New().Where(tasklink.ID.Eq(id), tasklink.UsedAt.IsNull()).UpdateAll(ctx, ex, mut)
	if err != nil {
		return fmt.Errorf("could not record that the task link was used: %w", err)
	}
	if n == 0 {
		return apierr.Invalidf("this link has already been used")
	}
	return nil
}

func (r *taskLinkRepository) VoidOpenForTask(ctx context.Context, taskID uuid.UUID, at time.Time) (int64, error) {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	var mut tasklink.Mut
	mut.SetVoidedAt(at)
	mut.SetUpdatedAtNow()
	n, err := tasklink.New().
		Where(tasklink.TaskID.Eq(taskID), tasklink.UsedAt.IsNull(), tasklink.VoidedAt.IsNull()).
		UpdateAll(ctx, ex, mut)
	if err != nil {
		return 0, fmt.Errorf("could not void the task's links: %w", err)
	}
	return n, nil
}

func taskLinkFrom(row tasklink.Row) entities.TaskLink {
	link := entities.TaskLink{
		ID:            row.ID,
		ProjectID:     row.ProjectID,
		TaskID:        row.TaskID,
		ParticipantID: row.ParticipantID,
		ExpiresAt:     row.ExpiresAt,
		CreatedAt:     row.CreatedAt,
	}
	if used, ok := row.UsedAt.Get(); ok {
		link.UsedAt = &used
	}
	if voided, ok := row.VoidedAt.Get(); ok {
		link.VoidedAt = &voided
	}
	return link
}
