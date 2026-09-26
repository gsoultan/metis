package postgres_test

import (
	"testing"

	"github.com/google/uuid"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/tests/testutils"
)

// templatedAhead is as many subscriptions as one read of the generated store
// returns: storm starts every query with a limit of 1000.
const templatedAhead = 1000

// The correlation-key backfill repairs every subscription still holding a raw
// ${...} template, not the first thousand.
//
// It runs once, as migration 2, and read what to repair through a query the
// store caps at a thousand rows. An installation upgrading with more than a
// thousand stranded subscriptions had the rest recorded as repaired: those
// instances could never be correlated to, and the migration would not run
// again to find them.
func TestTheCorrelationBackfillRepairsEveryTemplatedSubscription(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 4)
	repo, engine, projID, ctx := newPostgresEngine(t, db)

	defSvc := serviceimpl.NewDefinitionService(repo)
	if _, err := defSvc.CreateDefinition(ctx, paymentDefinition(projID, "order-payment-many")); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	instanceID, err := engine.StartProcess(ctx, projID, "order-payment-many", map[string]any{"orderId": "order-1"})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	subs, err := repo.Subscription().ListByInstance(ctx, instanceID)
	if err != nil || len(subs) != 1 {
		t.Fatalf("expected the instance's one subscription, got %d (err %v)", len(subs), err)
	}
	if err := repo.Subscription().UpdateCorrelationKey(ctx, uuid.UUID(subs[0].ID), "${orderId}"); err != nil {
		t.Fatalf("stage the legacy key: %v", err)
	}

	// More stranded subscriptions, in the shape the pre-fix code wrote, whose
	// keys sort ahead of the one above.
	if err := db.WithContext(ctx).Exec(`
		INSERT INTO event_subscriptions (id, created_at, updated_at, project_id, instance_id, node_id, type, event_name, correlation_key)
		SELECT ('00000000-0000-7000-8000-' || lpad(n::text, 12, '0'))::uuid, now(), now(), ?, ?,
		       'await-payment', 'message', 'PaymentReceived', '${orderId}'
		  FROM generate_series(1, ?) AS n`, projID, instanceID, templatedAhead).Error; err != nil {
		t.Fatalf("seed %d templated subscriptions: %v", templatedAhead, err)
	}

	result, err := serviceimpl.BackfillMessageCorrelationKeys(ctx, repo)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if result.Scanned != templatedAhead+1 || result.Rewritten != templatedAhead+1 {
		t.Errorf("the backfill scanned %d and rewrote %d of %d templated subscriptions",
			result.Scanned, result.Rewritten, templatedAhead+1)
	}
	after, err := repo.Subscription().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("re-read the subscriptions: %v", err)
	}
	for _, sub := range after {
		if sub.ID == subs[0].ID && sub.CorrelationKey != "order-1" {
			t.Fatalf("the backfill reported success and the subscription behind %d others still holds %q",
				templatedAhead, sub.CorrelationKey)
		}
	}
}
