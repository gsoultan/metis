package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/storm/runtime"
)

// Raw SQL because the generated store reads every column of one table and has
// no DISTINCT ON or GROUP BY, and this read is one row per key across two
// tables: the key's versions, and its release timeline. It leaves the table
// columns out — a list names decisions and has no use for their lines or
// examples.
//
// Each key is shown as its live version, and as its newest when none is live
// so a key in that state is listed rather than hidden. A live version that is
// gone counts as none. The WHERE clauses (%[1]s on the versions, %[2]s on the
// timeline, %[3]s on the row shown) are filled in by decisionKeyScope.
const decisionKeyRows = `
	WITH keys AS (
		SELECT project_id, "key", MAX(version) AS newest, MAX(created_at) AS saved_at
		  FROM decision_definitions
		 WHERE %[1]s
		 GROUP BY project_id, "key"
	), live AS (
		SELECT DISTINCT ON (project_id, decision_key) project_id, decision_key, version, activate_at
		  FROM decision_releases
		 WHERE %[2]s
		 ORDER BY project_id, decision_key, activate_at DESC
	), shown AS (
		SELECT d.id, d.project_id, d."key", d.name, d.version,
		       COALESCE(d.required_decisions::text, '') AS required, d.hit_policy,
		       COALESCE(lv.version, 0) AS live_version, k.newest,
		       GREATEST(k.saved_at, COALESCE(lv_release.activate_at, k.saved_at)) AS last_changed
		  FROM keys k
		  LEFT JOIN live lv_release
		    ON lv_release.project_id = k.project_id AND lv_release.decision_key = k."key"
		  LEFT JOIN decision_definitions lv
		    ON lv.project_id = k.project_id AND lv."key" = k."key"
		   AND lv.version = lv_release.version AND lv.deleted_at IS NULL
		  JOIN decision_definitions d
		    ON d.project_id = k.project_id AND d."key" = k."key" AND d.deleted_at IS NULL
		   AND d.version = COALESCE(lv.version, k.newest)
		 WHERE %[3]s
	)`

const (
	decisionKeyColumns = ` SELECT id, project_id, "key", name, version, required, hit_policy, live_version, newest, last_changed FROM shown`
	decisionKeyOrder   = ` ORDER BY "key", project_id`
	decisionKeyCount   = ` SELECT COUNT(*) FROM shown`
)

// decisionKeyColumnCount is how many columns decisionKeyColumns selects.
const decisionKeyColumnCount = 10

// ListKeysByProject returns one page of a project's decision keys, one row
// each, ordered by key, narrowed by search when it is not empty.
//
// Scoped the way ListByProjectPaged is: to the requested project when the
// caller may see it, to the caller's projects when it names none, and to
// nothing when it may see nothing. search matches the shown version's name or
// the key, ignoring case, with its wildcards escaped.
func (r *decisionRepository) ListKeysByProject(ctx context.Context, projectID uuid.UUID, search string, p contracts.Pagination) (contracts.Page[models.DecisionSummaryModel], error) {
	empty := contracts.NewPage([]models.DecisionSummaryModel{}, 0, p)
	scoped, visible, err := r.scopedProjects(ctx, projectID)
	if err != nil || !visible {
		return empty, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return empty, err
	}

	rows, args := decisionKeyScope(scoped, search, time.Now().UTC())
	total, err := countDecisionKeys(ctx, ex, rows+decisionKeyCount, args)
	if err != nil {
		return empty, err
	}
	n := p.Normalize()
	query := rows + decisionKeyColumns + decisionKeyOrder +
		fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	items, err := readDecisionKeys(ctx, ex, query, append(args, n.PageSize, p.Offset()))
	if err != nil {
		return empty, err
	}
	return contracts.NewPage(items, total, p), nil
}

// decisionKeyScope fills in decisionKeyRows: rows not deleted, in the projects
// the caller may see, with timeline entries whose moment has come, matching
// the search. A nil scope is system work naming no project, which filters
// nothing.
func decisionKeyScope(scoped []uuid.UUID, search string, now time.Time) (string, []any) {
	versions := []string{"deleted_at IS NULL"}
	timeline := []string{"deleted_at IS NULL"}
	shown := []string{"TRUE"}
	args := []any{now}
	timeline = append(timeline, "activate_at <= $1")
	if scoped != nil {
		args = append(args, uuidsToRaw(scoped))
		versions = append(versions, fmt.Sprintf("project_id = ANY($%d)", len(args)))
		timeline = append(timeline, fmt.Sprintf("project_id = ANY($%d)", len(args)))
	}
	if pattern, ok := containsPattern(search); ok {
		args = append(args, pattern)
		shown = []string{fmt.Sprintf(`(d.name ILIKE $%[1]d OR d."key" ILIKE $%[1]d)`, len(args))}
	}
	return fmt.Sprintf(decisionKeyRows,
		strings.Join(versions, " AND "), strings.Join(timeline, " AND "), strings.Join(shown, " AND ")), args
}

func countDecisionKeys(ctx context.Context, ex runtime.Executor, query string, args []any) (int64, error) {
	rows, err := ex.Query(ctx, query, args)
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

func readDecisionKeys(ctx context.Context, ex runtime.Executor, query string, args []any) ([]models.DecisionSummaryModel, error) {
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

// decisionSummaryFrom reads one row of decisionKeyColumns. The values are in
// PostgreSQL's binary format, and only valid until the next row: a uuid is its
// sixteen bytes, text its UTF-8, and each is copied out before it is kept.
func decisionSummaryFrom(values [][]byte) (models.DecisionSummaryModel, error) {
	if len(values) < decisionKeyColumnCount {
		return models.DecisionSummaryModel{}, fmt.Errorf("could not read a decision key: %d columns, want %d", len(values), decisionKeyColumnCount)
	}
	var id, project [16]byte
	copy(id[:], values[0])
	copy(project[:], values[1])
	var version, live, newest int64
	for _, column := range []struct {
		raw  []byte
		into *int64
	}{{values[4], &version}, {values[7], &live}, {values[8], &newest}} {
		if err := scanInt([][]byte{column.raw}, column.into); err != nil {
			return models.DecisionSummaryModel{}, err
		}
	}
	summary := models.DecisionSummaryModel{
		ID:            models.UUID(id),
		ProjectID:     models.UUID(project),
		Key:           string(values[2]),
		Name:          string(values[3]),
		Version:       int(version),
		HitPolicy:     string(values[6]),
		LiveVersion:   int(live),
		NewestVersion: int(newest),
	}
	if len(values[9]) > 0 {
		summary.LastChangedAt = runtime.Timestamptz(values[9])
	}
	if required := values[5]; len(required) > 0 {
		if err := json.Unmarshal(required, &summary.RequiredDecisions); err != nil {
			return models.DecisionSummaryModel{}, fmt.Errorf("could not decode what decision %q requires: %w", summary.Key, err)
		}
	}
	return summary, nil
}
