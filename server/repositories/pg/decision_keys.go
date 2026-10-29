package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store"
	"github.com/gsoultan/metis/server/repositories/store/decisiondefinition"
	"github.com/gsoultan/metis/server/repositories/store/decisionrelease"
)

// ListKeysByProject returns one page of a project's decision keys, one row
// each, ordered by key, narrowed by search when it is not empty.
//
// Each key is shown as its live version, and as its newest when none is live
// so a key in that state is listed rather than hidden; a live version that is
// gone counts as none. That is the declared Shown join, whose CTEs read the
// keys' versions and their release timeline: it leaves the table columns out,
// because a list names decisions and has no use for their lines or examples.
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

	shown, versions, released := decisionKeyScope(scoped, search, time.Now().UTC())
	total, err := store.DecisionDefinitionShown(shown, versions, released).Count(ctx, ex)
	if err != nil {
		return empty, fmt.Errorf("could not count the decision keys: %w", err)
	}
	n := p.Normalize()
	rows, err := store.DecisionDefinitionShown(shown.Limit(int64(n.PageSize)).Offset(int64(p.Offset())), versions, released).All(ctx, ex)
	if err != nil {
		return empty, fmt.Errorf("could not list the decision keys: %w", err)
	}
	items := make([]models.DecisionSummaryModel, 0, len(rows))
	for _, row := range rows {
		item, err := decisionSummaryFrom(row)
		if err != nil {
			return empty, err
		}
		items = append(items, item)
	}
	return contracts.NewPage(items, total, p), nil
}

// decisionKeyScope is the three reads the Shown join narrows: the rows shown,
// the versions each key's newest is read from, and the release timeline —
// in the projects the caller may see, with the entries whose moment has come,
// the rows shown matching the search. A nil scope is system work naming no
// project, which filters nothing.
func decisionKeyScope(scoped []uuid.UUID, search string, now time.Time) (decisiondefinition.Query, decisiondefinition.Query, decisionrelease.Query) {
	shown, versions := decisiondefinition.New(), decisiondefinition.New()
	released := decisionrelease.New().Where(decisionrelease.ActivateAt.Lte(now))
	if scoped != nil {
		projects := uuidsToRaw(scoped)
		shown = shown.Where(decisiondefinition.ProjectID.In(projects...))
		versions = versions.Where(decisiondefinition.ProjectID.In(projects...))
		released = released.Where(decisionrelease.ProjectID.In(projects...))
	}
	if pattern, ok := containsPattern(search); ok {
		shown = shown.Any(decisiondefinition.Name.ILike(pattern), decisiondefinition.Key.ILike(pattern))
	}
	return shown, versions, released
}

// decisionSummaryFrom reads one Shown row. The last change is the later of
// the newest save and the moment the live version was released.
func decisionSummaryFrom(row decisiondefinition.ShownRow) (models.DecisionSummaryModel, error) {
	summary := models.DecisionSummaryModel{
		ID:            models.UUID(row.ID),
		ProjectID:     models.UUID(row.ProjectID),
		Key:           row.Key,
		Name:          row.Name,
		Version:       int(row.Version),
		HitPolicy:     row.HitPolicy,
		LiveVersion:   int(row.LiveVersion.V),
		NewestVersion: int(row.Newest.V),
		LastChangedAt: row.SavedAt.V,
	}
	if at, ok := row.ActivatedAt.Get(); ok && at.After(summary.LastChangedAt) {
		summary.LastChangedAt = at
	}
	if len(row.RequiredDecisions) > 0 {
		if err := json.Unmarshal(row.RequiredDecisions, &summary.RequiredDecisions); err != nil {
			return models.DecisionSummaryModel{}, fmt.Errorf("could not decode what decision %q requires: %w", summary.Key, err)
		}
	}
	return summary, nil
}
