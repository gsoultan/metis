package pg

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/auditentry"
)

type auditRepository struct{ conn }

// NewAuditRepository returns the account of what happened.
func NewAuditRepository(c *db.Conn) contracts.AuditRepository {
	return &auditRepository{conn{conn: c}}
}

// Create records one entry.
//
// Never scoped and never refused. An audit entry is written on the path of the
// thing it describes, and a write that could fail here would mean an action
// that happened with no record of it — which is worse than a record nobody
// reads.
//
// The entry's place in the trail is never assigned here: the insert names only
// the columns it sets, so seq takes its default, the next number of a sequence,
// in the order this transaction writes.
func (r *auditRepository) Create(ctx context.Context, entry models.AuditModel) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	data, err := sealedJSONOf(entry.Data)
	if err != nil {
		return fmt.Errorf("could not encode the audit entry's data: %w", err)
	}

	ins := auditentry.Create()
	if id := uuid.UUID(entry.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetProjectID(uuid.UUID(entry.ProjectID))
	ins.SetInstanceID(uuid.UUID(entry.InstanceID))
	ins.SetType(entry.Type)
	ins.SetMessage(entry.Message)
	ins.SetData(data)
	setOrNullString(ins.SetNodeID, ins.SetNodeIDNull, entry.NodeID)
	setOrNullString(ins.SetNodeName, ins.SetNodeNameNull, entry.NodeName)
	setOrNullString(ins.SetNarrative, ins.SetNarrativeNull, entry.Narrative)
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not record the audit entry: %w", err)
	}
	return nil
}

// ListByInstance returns one instance's whole history, oldest first.
//
// Every entry, for the callers that act on all of them — a migration reading
// what an instance has done. A screen reads LatestByInstance instead.
func (r *auditRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.AuditModel, error) {
	q, ok, err := r.scoped(ctx, auditentry.InstanceID.Eq(instanceID))
	if err != nil || !ok {
		return nil, err
	}
	return r.read(ctx, q.Order(oldestFirst()...).Limit(allRows))
}

// ListByProject returns the oldest limit entries of a project's history,
// oldest first.
func (r *auditRepository) ListByProject(ctx context.Context, projectID uuid.UUID, limit int64) ([]models.AuditModel, error) {
	q, ok, err := r.scoped(ctx, auditentry.ProjectID.Eq(projectID))
	if err != nil || !ok {
		return nil, err
	}
	return r.read(ctx, q.Order(oldestFirst()...).Limit(limit))
}

// LatestByInstance returns a window of an instance's history counted from its
// newest entry — skip the newest offset, then take limit — in the order they
// were written, and how many entries the instance has in all.
//
// From the newest end because that is the end somebody looks at: where the
// instance is now, and what it did last. The window is read newest first and
// turned round here, so a caller gets the same oldest-first order the whole
// trail comes in. The reverse of oldestFirst is a reverse of the same ties, so
// the window holds exactly the entries the whole trail would end with.
func (r *auditRepository) LatestByInstance(ctx context.Context, instanceID uuid.UUID, limit, offset int64) ([]models.AuditModel, int64, error) {
	q, ok, err := r.scoped(ctx, auditentry.InstanceID.Eq(instanceID))
	if err != nil || !ok {
		return nil, 0, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, 0, err
	}
	total, err := q.Count(ctx, ex)
	if err != nil {
		return nil, 0, fmt.Errorf("could not count the audit trail: %w", err)
	}
	entries, err := r.read(ctx, q.
		Order(auditentry.CreatedAt.Desc(), auditentry.Seq.DescNullsLast()).
		Limit(limit).
		Offset(offset))
	if err != nil {
		return nil, 0, err
	}
	slices.Reverse(entries)
	return entries, total, nil
}

// NodeVisits counts how often an instance reached each node, in the order it
// first reached them.
//
// Counted by the database, so the answer is as long as the process is wide
// rather than as long as the instance has run: the execution path used to read
// the whole trail into memory to count it, and an instance that loops for a
// year has a trail that size. Raw SQL because it is a window over a GROUP BY,
// which the generated store does not express. The first visit is ordered as
// the trail is, by created_at and then seq.
func (r *auditRepository) NodeVisits(ctx context.Context, instanceID uuid.UUID, eventType string) ([]contracts.NodeVisit, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	if !scope.unrestricted() && len(scope.projects) == 0 {
		return nil, nil
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	args := []any{instanceID[:], eventType}
	inScope := ""
	if !scope.unrestricted() {
		args = append(args, uuidsToRaw(scope.projects))
		inScope = " AND project_id = ANY($3)"
	}
	rows, err := ex.Query(ctx, `
		SELECT node_id, visits FROM (
		    SELECT node_id, created_at, seq,
		           COUNT(*) OVER (PARTITION BY node_id) AS visits,
		           ROW_NUMBER() OVER (PARTITION BY node_id ORDER BY created_at, seq NULLS FIRST) AS nth
		      FROM audit_logs
		     WHERE instance_id = $1 AND type = $2 AND node_id <> '' AND deleted_at IS NULL`+inScope+`
		) visited
		WHERE nth = 1
		ORDER BY created_at, seq NULLS FIRST`, args)
	if err != nil {
		return nil, fmt.Errorf("could not count the nodes an instance reached: %w", err)
	}
	defer rows.Close()
	var visits []contracts.NodeVisit
	for rows.Next() {
		values := rows.RawValues()
		if len(values) < 2 {
			continue
		}
		var count int64
		if err := scanInt(values[1:], &count); err != nil {
			return nil, err
		}
		visits = append(visits, contracts.NodeVisit{NodeID: string(values[0]), Visits: int(count)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not count the nodes an instance reached: %w", err)
	}
	return visits, nil
}

// oldestFirst is the order of the trail: by created_at and then by seq.
//
// created_at is the moment the writing transaction began, so every entry one
// step writes shares it; seq is the order that transaction wrote them in, from
// a sequence. Ordered by created_at alone they came back in whatever order
// PostgreSQL returned the ties — where the rows happen to be stored, which a
// reseal, CLUSTER or pg_repack moves — and the ids could not break the tie:
// they are random, and ordering by them started the execution path at the task
// instead of the start event.
//
// Entries written before migration 28 have no seq. They tie as they always did
// and come back as they always have; NULLS FIRST, because every one of them was
// written before any entry that has one.
func oldestFirst() []auditentry.Sort {
	return []auditentry.Sort{auditentry.CreatedAt.Asc(), auditentry.Seq.AscNullsFirst()}
}

// scoped is the trail the caller may see, narrowed by one predicate; false
// when that is nothing at all.
//
// The scope is on project_id, so an entry belonging to another organization is
// absent rather than filtered — an audit trail that could be read across
// tenants is a description of somebody else's business.
func (r *auditRepository) scoped(ctx context.Context, pred auditentry.Pred) (auditentry.Query, bool, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return auditentry.Query{}, false, err
	}
	q := auditentry.New().Where(pred)
	if !scope.unrestricted() {
		if len(scope.projects) == 0 {
			return auditentry.Query{}, false, nil
		}
		q = q.Where(auditentry.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	return q, true, nil
}

// read runs q and decodes what it returns.
//
// Every caller gives the limit it means. The store's default of a thousand is
// not one: past it the audit view stopped a thousand steps in and never
// reached where the instance is now, the execution path stopped at the same
// place, and the OCEL export lost its most recent events. One statement rather
// than pg.everyRow, because a keyset walk needs an order with no ties and the
// entries written before migration 28 still have them.
func (r *auditRepository) read(ctx context.Context, q auditentry.Query) ([]models.AuditModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the audit trail: %w", err)
	}
	out := make([]models.AuditModel, 0, len(rows))
	for _, row := range rows {
		entry := models.AuditModel{
			Base: models.Base{
				ID:        models.UUID(row.ID),
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
			ProjectID:  models.UUID(row.ProjectID),
			InstanceID: models.UUID(row.InstanceID),
			Type:       row.Type,
			NodeID:     valueOr(row.NodeID),
			NodeName:   valueOr(row.NodeName),
			Message:    row.Message,
			Narrative:  valueOr(row.Narrative),
		}
		if seq, ok := row.Seq.Get(); ok {
			entry.Seq = &seq
		}
		if len(row.Data) > 0 {
			if err := unseal(row.Data, &entry.Data); err != nil {
				return nil, fmt.Errorf("could not decode an audit entry's data: %w", err)
			}
		}
		out = append(out, entry)
	}
	return out, nil
}
