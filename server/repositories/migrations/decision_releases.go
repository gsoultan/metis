package migrations

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// LiveDecisionVersionsMigration is the version "which version of a decision is
// live" is recorded under. Named once, here, so the test that rewinds it and
// the list that runs it cannot disagree about which row they mean.
const LiveDecisionVersionsMigration = 26

// liveDecisionVersions is migration 26.
//
// Saving a decision adds a version instead of rewriting one, and the version an
// evaluation that names none reads is the one recorded on a release timeline
// rather than simply the highest. An installation upgrading has no timeline, so
// this records one entry per decision key: the version that evaluates today —
// the highest one not deleted — live since it was saved. What an evaluation
// returns does not change.
//
// Transactional, because an upgrade that recorded some keys and not others
// would leave the rest with no live version. And idempotent: a key that already
// has an entry is left alone, so running it again adds nothing and never
// overrules a version somebody has made live since.
func liveDecisionVersions(models []any) Migration {
	return Migration{
		Version:       LiveDecisionVersionsMigration,
		Name:          "which version of a decision is live",
		Transactional: true,
		Run: func(ctx context.Context, db *gorm.DB) error {
			model, err := modelForTable(db, models, "decision_releases")
			if err != nil {
				return err
			}
			if !db.Migrator().HasTable(model) {
				if err := db.AutoMigrate(model); err != nil {
					return fmt.Errorf("create decision_releases: %w", err)
				}
			}
			return recordLiveDecisionVersions(ctx, db, time.Now().UTC())
		},
	}
}

// liveDecisionVersion is the version of one decision key that evaluates today.
type liveDecisionVersion struct {
	ProjectID string    `gorm:"column:project_id"`
	Key       string    `gorm:"column:key"`
	Version   int       `gorm:"column:version"`
	SavedAt   time.Time `gorm:"column:created_at"`
}

// newestDecisionVersions finds, for every decision key with no timeline entry,
// its highest version that has not been deleted.
//
// Not deleted, because that is what "the version that evaluates today" was: the
// store never returns a deleted row, so a key whose newest version was deleted
// evaluated the one below it. A key with every version deleted evaluated
// nothing, and gets no entry.
const newestDecisionVersions = `
	SELECT d.project_id, d."key", d.version, d.created_at
	  FROM decision_definitions d
	 WHERE d.deleted_at IS NULL
	   AND d.version = (SELECT MAX(v.version) FROM decision_definitions v
	                     WHERE v.project_id = d.project_id AND v."key" = d."key"
	                       AND v.deleted_at IS NULL)
	   AND NOT EXISTS (SELECT 1 FROM decision_releases r
	                    WHERE r.project_id = d.project_id AND r.decision_key = d."key"
	                      AND r.deleted_at IS NULL)`

// recordLiveDecisionVersions writes the timeline entry each of those keys is
// missing.
//
// In force since the version was saved, rather than since the upgrade: that is
// when it became the version evaluations read, and the timeline is the record
// of when each version took over. A save stamped later than now — a clock that
// was ahead — is recorded as now, so the entry is in force the moment the
// upgrade finishes rather than at some point after it.
func recordLiveDecisionVersions(ctx context.Context, db *gorm.DB, now time.Time) error {
	var newest []liveDecisionVersion
	if err := db.WithContext(ctx).Raw(newestDecisionVersions).Scan(&newest).Error; err != nil {
		return fmt.Errorf("read the decision versions that evaluate today: %w", err)
	}
	for _, live := range newest {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("generate a release id: %w", err)
		}
		since := live.SavedAt.UTC()
		if since.IsZero() || since.After(now) {
			since = now
		}
		if err := db.WithContext(ctx).Exec(`
			INSERT INTO decision_releases (id, created_at, updated_at, project_id, decision_key, version, activate_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id.String(), now, now, live.ProjectID, live.Key, live.Version, since).Error; err != nil {
			return fmt.Errorf("record version %d of %q as live: %w", live.Version, live.Key, err)
		}
	}
	return nil
}
