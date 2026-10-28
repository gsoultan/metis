package postgres_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/webhookdelivery"
	"github.com/gsoultan/metis/tests/testutils"
)

// A sweep deletes a batch at a time so that the first one after an upgrade —
// which meets everything the table collected before there was a sweep — does
// not hold every row lock in one transaction. It still has to finish the job:
// stopping after the first batch would leave a table that had outgrown one
// batch per interval growing anyway.
func TestASweepRemovesMoreThanOneBatch(t *testing.T) {
	conn := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(conn)
	ctx := entities.WithSystemContext(t.Context())

	old := db.PruneBatch*2 + 1
	hook := testutils.SeedWebhook(t, conn, testutils.SeedProject(t, conn))
	now := time.Now()
	deliveries := make([]webhookdelivery.Row, 0, old+1)
	for n := range old {
		deliveries = append(deliveries, webhookdelivery.Row{ID: uuid.Must(uuid.NewV7()), CreatedAt: now, UpdatedAt: now,
			WebhookID: hook, DeliveryID: fmt.Sprintf("delivery-%d", n+1), ReceivedAt: now.Add(-30 * 24 * time.Hour)})
	}
	deliveries = append(deliveries, webhookdelivery.Row{ID: uuid.Must(uuid.NewV7()), CreatedAt: now, UpdatedAt: now,
		WebhookID: hook, DeliveryID: "recent", ReceivedAt: now})
	ex := testutils.Executor(conn)
	if _, err := webhookdelivery.InsertAll(ctx, ex, deliveries); err != nil {
		t.Fatalf("seed the deliveries: %v", err)
	}

	removed, err := repo.Webhook().ForgetDeliveriesBefore(ctx, time.Now().Add(-48*time.Hour))
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	left := testutils.RowsIn(t, conn, "webhook_deliveries")
	if removed != int64(old) || left != 1 {
		t.Fatalf("one sweep removed %d of %d old deliveries and left %d rows; want all of them gone and the recent one kept",
			removed, old, left)
	}
}
