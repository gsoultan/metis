package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/storm/runtime"
)

// Raw SQL because the generated store reads every column and has no DISTINCT
// ON, and the point of this read is the columns it leaves out: a key's newest
// version is found by the (project_id, key, version) index without touching
// the table's lines or examples.
const (
	latestDecisionColumns = `SELECT DISTINCT ON (project_id, key)
	       id, project_id, key, name, version, COALESCE(required_decisions::text, '')
	  FROM decision_definitions
	 WHERE `
	latestDecisionOrder = ` ORDER BY project_id, key, version DESC`
	latestDecisionCount = `SELECT COUNT(*) FROM (SELECT DISTINCT project_id, key FROM decision_definitions WHERE %s) AS decision_keys`
)

// latestSummaryColumns is how many columns latestDecisionColumns selects.
const latestSummaryColumns = 6

// ListLatestByProject returns one page of a project's decision keys, each as
// its newest version, ordered by key.
//
// Scoped the way ListByProjectPaged is: to the requested project when the
// caller may see it, to the caller's projects when it names none, and to
// nothing when it may see nothing.
func (r *decisionRepository) ListLatestByProject(ctx context.Context, projectID uuid.UUID, p contracts.Pagination) (contracts.Page[models.DecisionSummaryModel], error) {
	empty := contracts.NewPage([]models.DecisionSummaryModel{}, 0, p)
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return empty, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return empty, err
	}

	where, args := latestDecisionScope(scoped)
	total, err := countLatestDecisions(ctx, ex, where, args)
	if err != nil {
		return empty, err
	}
	n := p.Normalize()
	query := latestDecisionColumns + where + latestDecisionOrder +
		fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	items, err := readLatestDecisions(ctx, ex, query, append(args, n.PageSize, p.Offset()))
	if err != nil {
		return empty, err
	}
	return contracts.NewPage(items, total, p), nil
}

// latestDecisionScope is the WHERE clause both statements share: live rows, in
// the projects the caller may see. A nil scope is system work naming no
// project, which filters nothing.
func latestDecisionScope(scoped []uuid.UUID) (string, []any) {
	clauses := []string{"deleted_at IS NULL"}
	var args []any
	if scoped != nil {
		args = append(args, uuidsToRaw(scoped))
		clauses = append(clauses, fmt.Sprintf("project_id = ANY($%d)", len(args)))
	}
	return strings.Join(clauses, " AND "), args
}

func countLatestDecisions(ctx context.Context, ex runtime.Executor, where string, args []any) (int64, error) {
	rows, err := ex.Query(ctx, fmt.Sprintf(latestDecisionCount, where), args)
	if err != nil {
		return 0, fmt.Errorf("could not count the decision keys: %w", err)
	}
	defer rows.Close()
	var total int64
	if rows.Next() {
		if err := scanInt(rows.RawValues(), &total); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("could not count the decision keys: %w", err)
	}
	return total, nil
}

func readLatestDecisions(ctx context.Context, ex runtime.Executor, query string, args []any) ([]models.DecisionSummaryModel, error) {
	rows, err := ex.Query(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("could not list the decision keys: %w", err)
	}
	defer rows.Close()
	var items []models.DecisionSummaryModel
	for rows.Next() {
		item, err := decisionSummaryFrom(rows.RawValues())
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not list the decision keys: %w", err)
	}
	return items, nil
}

// decisionSummaryFrom reads one row of latestDecisionColumns. The values are in
// PostgreSQL's binary format, and only valid until the next row: a uuid is its
// sixteen bytes, text its UTF-8, and each is copied out before it is kept.
func decisionSummaryFrom(values [][]byte) (models.DecisionSummaryModel, error) {
	if len(values) < latestSummaryColumns {
		return models.DecisionSummaryModel{}, fmt.Errorf("could not read a decision key: %d columns, want %d", len(values), latestSummaryColumns)
	}
	var id, project [16]byte
	copy(id[:], values[0])
	copy(project[:], values[1])
	var version int64
	if err := scanInt(values[4:5], &version); err != nil {
		return models.DecisionSummaryModel{}, err
	}
	summary := models.DecisionSummaryModel{
		ID:        models.UUID(id),
		ProjectID: models.UUID(project),
		Key:       string(values[2]),
		Name:      string(values[3]),
		Version:   int(version),
	}
	if required := values[5]; len(required) > 0 {
		if err := json.Unmarshal(required, &summary.RequiredDecisions); err != nil {
			return models.DecisionSummaryModel{}, fmt.Errorf("could not decode what decision %q requires: %w", summary.Key, err)
		}
	}
	return summary, nil
}
