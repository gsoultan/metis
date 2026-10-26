package postgres_test

import (
	"errors"
	"testing"
	"time"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/tests/testutils"
)

// A wait that ended is removed, not marked. Both tables soft-delete, so a
// set-based delete through the store would only mark the rows; these two
// statements purge on purpose, and a marked row left behind is the regression.
func TestClearingANodesWaitLeavesNoRowBehind(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 4)
	repo, engine, projID, ctx := newPostgresEngine(t, db)

	if _, err := serviceimpl.NewDefinitionService(repo).CreateDefinition(ctx, paymentDefinition(projID, "order-payment-purge")); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	cleared, err := engine.StartProcess(ctx, projID, "order-payment-purge", map[string]any{"orderId": "order-1"})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	kept, err := engine.StartProcess(ctx, projID, "order-payment-purge", map[string]any{"orderId": "order-2"})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	if err := repo.Subscription().DeleteByNode(ctx, cleared, "await-payment"); err != nil {
		t.Fatalf("delete by node: %v", err)
	}

	var gone, left int64
	if err := db.Raw(`SELECT count(*) FROM event_subscriptions WHERE instance_id = ?`, cleared).Scan(&gone).Error; err != nil {
		t.Fatalf("count the cleared instance's rows: %v", err)
	}
	if gone != 0 {
		t.Errorf("%d subscription rows remain for the cleared node, deleted or not; want none", gone)
	}
	if err := db.Raw(`SELECT count(*) FROM event_subscriptions WHERE instance_id = ? AND deleted_at IS NULL`, kept).Scan(&left).Error; err != nil {
		t.Fatalf("count the other instance's rows: %v", err)
	}
	if left != 1 {
		t.Errorf("the other instance has %d live subscriptions, want its 1", left)
	}
}

// Cancelling a cutover removes it for good, and refuses one that already took
// effect.
func TestCancellingAScheduledReleaseRemovesOnlyOneStillAhead(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 4)
	repo, _, projID, ctx := newPostgresEngine(t, db)
	defs := repo.Definition()
	now := time.Now().UTC()

	if err := defs.ScheduleRelease(ctx, projID, "invoice", 1, now.Add(-time.Hour)); err != nil {
		t.Fatalf("schedule the past release: %v", err)
	}
	if err := defs.ScheduleRelease(ctx, projID, "invoice", 2, now.Add(time.Hour)); err != nil {
		t.Fatalf("schedule the future release: %v", err)
	}
	timeline, err := defs.ListReleasesForKey(ctx, projID, "invoice")
	if err != nil || len(timeline) != 2 {
		t.Fatalf("timeline: %d entries, err %v; want 2", len(timeline), err)
	}
	// Newest first: the one still ahead, then the one in effect.
	ahead, inEffect := timeline[0], timeline[1]

	if err := defs.DeleteScheduledRelease(ctx, projID, inEffect.ID.ToUUID()); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("cancelling a release in effect: err %v, want ErrNotFound", err)
	}
	if err := defs.DeleteScheduledRelease(ctx, projID, ahead.ID.ToUUID()); err != nil {
		t.Fatalf("cancel the scheduled release: %v", err)
	}
	if err := defs.DeleteScheduledRelease(ctx, projID, ahead.ID.ToUUID()); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("cancelling it twice: err %v, want ErrNotFound", err)
	}

	var rows int64
	if err := db.Raw(`SELECT count(*) FROM process_definition_releases WHERE project_id = ?`, projID).Scan(&rows).Error; err != nil {
		t.Fatalf("count releases: %v", err)
	}
	if rows != 1 {
		t.Errorf("%d release rows, deleted or not; want only the one in effect", rows)
	}
}
