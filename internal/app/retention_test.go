package app

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/store/idempotencyrecord"
	"github.com/gsoultan/metis/server/repositories/store/sharedcounter"
	"github.com/gsoultan/metis/server/repositories/store/webhookdelivery"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm/runtime"
)

// Three tables keep a row only for as long as it can answer something: has
// this delivery been seen, has this request been done, how much has this client
// sent this minute. Each model says a retention sweep works from its timestamp,
// and none was ever started — the deliveries had a method nothing called, the
// counters another, the idempotency records none at all. So all three grew for
// as long as the server ran, two of them keyed by whatever callers chose to
// send.
func TestARunningServerForgetsWhatCanNoLongerBeAsked(t *testing.T) {
	conn := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	a := &App{storm: conn, repo: repo, sse: sse,
		svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "retention-test", nil, nil, nil)}

	ex := testutils.Executor(conn)
	seeded := func(_ any, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	claim := func(completed bool, status int64, at time.Time) {
		t.Helper()
		record := idempotencyrecord.Create()
		record.SetRecordKey(uuid.NewString())
		record.SetRequestHash("hash")
		record.SetCompleted(completed)
		record.SetStatusCode(status)
		record.SetHeaders(runtime.JSON("{}"))
		record.SetBody([]byte{})
		record.SetCreatedAt(at)
		seeded(record.Insert(t.Context(), ex))
	}
	_, _, project := testutils.ScopedProject(t, repo)
	hook := testutils.SeedWebhook(t, conn, project)
	monthAgo, now := time.Now().Add(-30*24*time.Hour), time.Now()
	for _, at := range []time.Time{monthAgo, now} {
		delivery := webhookdelivery.Create()
		delivery.SetCreatedAt(at)
		delivery.SetUpdatedAt(at)
		delivery.SetWebhookID(hook)
		delivery.SetDeliveryID(uuid.NewString())
		delivery.SetReceivedAt(at)
		seeded(delivery.Insert(t.Context(), ex))
		claim(true, 200, at)
		counter := sharedcounter.Create()
		counter.SetScope("http-rate")
		counter.SetCounterKey(uuid.NewString())
		counter.SetReplica("replica-1")
		counter.SetWindowStart(at.Truncate(time.Minute))
		counter.SetCount(1)
		counter.SetUpdatedAt(at)
		seeded(counter.Insert(t.Context(), ex))
	}
	// A claim nobody answered, left by a replica that died a month ago.
	claim(false, 0, monthAgo)
	// A claim past the TTL and still being worked on: nothing bounds how long a
	// request runs, so an hour-old claim may still be answered.
	claim(false, 0, now.Add(-time.Hour))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a.startBackgroundWork(ctx)

	want := map[string]int64{"webhook_deliveries": 1, "idempotency_records": 2, "shared_counters": 1}
	left := map[string]int64{}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		for table, count := range map[string]func() (int64, error){
			"webhook_deliveries":  func() (int64, error) { return webhookdelivery.New().Unordered().Count(t.Context(), ex) },
			"idempotency_records": func() (int64, error) { return idempotencyrecord.New().Unordered().Count(t.Context(), ex) },
			"shared_counters":     func() (int64, error) { return sharedcounter.New().Unordered().Count(t.Context(), ex) },
		} {
			n, err := count()
			if err != nil {
				t.Fatalf("count %s: %v", table, err)
			}
			left[table] = n
		}
		if maps.Equal(left, want) {
			return
		}
	}
	t.Fatalf("ten seconds into a running server, the tables hold %v rows; only the ones that can still be asked about should be left: %v",
		left, want)
}
