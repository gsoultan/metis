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
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/archivedinstance"
)

// forgetArchiveBatch is how many archived instances one statement of the
// archive's own sweep removes. Each row is read first, records and all, to
// find the copies kept outside the database.
const forgetArchiveBatch = 100

type archiveRepository struct{ conn }

// NewArchiveRepository returns the store of the finished instances the
// retention sweep archived.
//
// Each runtime keeps its own archive, beside the instances it archived: a
// request on an environment's port reads that environment's, and one on the
// main port the main database's (see ArchiveFinished).
func NewArchiveRepository(c *db.Conn) contracts.ArchiveRepository {
	return &archiveRepository{conn{conn: c}}
}

// List reads a project's archived instances, most recently finished first,
// from after the filter's cursor.
func (r *archiveRepository) List(ctx context.Context, projectID uuid.UUID, filter contracts.ArchiveFilter) ([]models.ArchivedInstanceModel, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	q := archivedinstance.New().
		Where(archivedinstance.ProjectID.Eq(projectID)).
		WhereIf(filter.ProcessKey != "", archivedinstance.ProcessKey.Eq(filter.ProcessKey)).
		WhereIf(filter.BusinessKey != "", archivedinstance.BusinessKey.Eq(filter.BusinessKey)).
		Order(archivedinstance.FinishedAt.Desc(), archivedinstance.ID.Desc()).
		Limit(filter.Limit)
	if !filter.FinishedBefore.IsZero() {
		// Past the last row of the page before, in the list's order.
		q = q.After(archivedinstance.Row{FinishedAt: filter.FinishedBefore, ID: filter.AfterID})
	}
	rows, err := q.AllSummary(ctx, ex)
	if err != nil {
		return nil, fmt.Errorf("could not read the archive: %w", err)
	}
	out := make([]models.ArchivedInstanceModel, 0, len(rows))
	for _, row := range rows {
		out = append(out, archivedFrom(row))
	}
	return out, nil
}

// Get reads one archived instance of the caller's and opens its record.
func (r *archiveRepository) Get(ctx context.Context, instanceID uuid.UUID) (models.ArchivedInstanceModel, models.ArchiveRecord, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.ArchivedInstanceModel{}, models.ArchiveRecord{}, err
	}
	row, found, err := archivedinstance.New().Where(archivedinstance.ID.Eq(instanceID)).One(ctx, ex)
	if err != nil {
		return models.ArchivedInstanceModel{}, models.ArchiveRecord{}, fmt.Errorf("could not read the archived instance: %w", err)
	}
	if !found || r.requireProjectInTenant(ctx, row.ProjectID) != nil {
		return models.ArchivedInstanceModel{}, models.ArchiveRecord{}, fmt.Errorf("%w: no such archived instance", apierr.ErrNotFound)
	}
	record, err := openStoredRecord(ctx, row.RecordStore, row.Record)
	if err != nil {
		return models.ArchivedInstanceModel{}, models.ArchiveRecord{}, fmt.Errorf("could not open the archive record of instance %s: %w", instanceID, err)
	}
	return archivedFrom(archivedinstance.SummaryRow{
		ID: row.ID, CreatedAt: row.CreatedAt, ProjectID: row.ProjectID, DefinitionID: row.DefinitionID,
		ProcessKey: row.ProcessKey, ProcessName: row.ProcessName, ProcessVersion: row.ProcessVersion,
		BusinessKey: row.BusinessKey, Status: row.Status, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
	}), record, nil
}

// ForgetArchivedBefore deletes the records archived before cutoff, a batch
// at a time, oldest first, and then the copies of them kept outside the
// database. created_at is indexed, so each batch is a range of it.
func (r *archiveRepository) ForgetArchivedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if !entities.IsSystemContext(ctx) {
		return 0, fmt.Errorf("%w: forgetting archived instances spans every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	old := archivedinstance.New().
		Where(archivedinstance.CreatedAt.Lt(cutoff.UTC())).
		Order(archivedinstance.CreatedAt.Asc(), archivedinstance.ID.Asc()).
		Limit(forgetArchiveBatch)
	var removed int64
	for {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		rows, err := old.All(ctx, ex, nil)
		if err != nil {
			return removed, fmt.Errorf("could not read old archived instances: %w", err)
		}
		if len(rows) == 0 {
			return removed, nil
		}
		ids := make([][16]byte, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		n, err := archivedinstance.New().Where(archivedinstance.ID.In(ids...)).DeleteAll(ctx, ex)
		removed += n
		if err != nil {
			return removed, fmt.Errorf("could not forget old archived instances: %w", err)
		}
		if err := forgetStoredRecords(ctx, rows); err != nil {
			return removed, fmt.Errorf("removed old archived instances, and could not remove some of their records from their store: %w", err)
		}
		if len(rows) < forgetArchiveBatch {
			return removed, nil
		}
	}
}

func archivedFrom(row archivedinstance.SummaryRow) models.ArchivedInstanceModel {
	return models.ArchivedInstanceModel{
		Base:           models.Base{ID: models.UUID(row.ID), CreatedAt: row.CreatedAt},
		ProjectID:      models.UUID(row.ProjectID),
		DefinitionID:   models.UUID(row.DefinitionID),
		ProcessKey:     row.ProcessKey,
		ProcessName:    row.ProcessName,
		ProcessVersion: int(row.ProcessVersion),
		BusinessKey:    valueOr(row.BusinessKey),
		Status:         models.ProcessStatus(row.Status),
		StartedAt:      row.StartedAt,
		FinishedAt:     row.FinishedAt,
	}
}
