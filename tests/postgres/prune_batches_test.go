package postgres_test

import (
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/tests/testutils"
)

// A sweep deletes a batch at a time so that the first one after an upgrade —
// which meets everything the table collected before there was a sweep — does
// not hold every row lock in one transaction. It still has to finish the job:
// stopping after the first batch would leave a table that had outgrown one
// batch per interval growing anyway.
func TestASweepRemovesMoreThanOneBatch(t *testing.T) {
	gormDB := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(gormDB))
	ctx := entities.WithSystemContext(t.Context())

	old := db.PruneBatch*2 + 1
	if err := gormDB.WithContext(ctx).Exec(`
		INSERT INTO webhook_deliveries (id, created_at, updated_at, webhook_id, delivery_id, received_at)
		SELECT gen_random_uuid(), now(), now(), gen_random_uuid(), 'delivery-' || n, now() - interval '30 days'
		  FROM generate_series(1, ?) AS n`, old).Error; err != nil {
		t.Fatalf("seed old deliveries: %v", err)
	}
	if err := gormDB.WithContext(ctx).Exec(`
		INSERT INTO webhook_deliveries (id, created_at, updated_at, webhook_id, delivery_id, received_at)
		VALUES (gen_random_uuid(), now(), now(), gen_random_uuid(), 'recent', now())`).Error; err != nil {
		t.Fatalf("seed a recent delivery: %v", err)
	}

	removed, err := repo.Webhook().ForgetDeliveriesBefore(ctx, time.Now().Add(-48*time.Hour))
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	var left int64
	if err := gormDB.WithContext(ctx).Raw(`SELECT count(*) FROM webhook_deliveries`).Scan(&left).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if removed != int64(old) || left != 1 {
		t.Fatalf("one sweep removed %d of %d old deliveries and left %d rows; want all of them gone and the recent one kept",
			removed, old, left)
	}
}
