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
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// Three tables keep a row only for as long as it can answer something: has
// this delivery been seen, has this request been done, how much has this client
// sent this minute. Each model says a retention sweep works from its timestamp,
// and none was ever started — the deliveries had a method nothing called, the
// counters another, the idempotency records none at all. So all three grew for
// as long as the server ran, two of them keyed by whatever callers chose to
// send.
func TestARunningServerForgetsWhatCanNoLongerBeAsked(t *testing.T) {
	gormDB := testutils.SetupTestDB(t)
	conn := testutils.StormConn(gormDB)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	a := &App{db: gormDB, storm: conn, repo: repo, sse: sse,
		svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "retention-test", nil, nil, nil, func(*gorm.DB) {})}

	seed := func(stmt string, args ...any) {
		t.Helper()
		if err := gormDB.WithContext(t.Context()).Exec(stmt, args...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	monthAgo, now := time.Now().Add(-30*24*time.Hour), time.Now()
	for _, at := range []time.Time{monthAgo, now} {
		seed(`INSERT INTO webhook_deliveries (id, created_at, updated_at, webhook_id, delivery_id, received_at)
		      VALUES (?, ?, ?, ?, ?, ?)`, uuid.New(), at, at, uuid.New(), uuid.NewString(), at)
		seed(`INSERT INTO idempotency_records (record_key, request_hash, completed, status_code, created_at)
		      VALUES (?, 'hash', true, 200, ?)`, uuid.NewString(), at)
		seed(`INSERT INTO shared_counters (scope, counter_key, replica, window_start, count, updated_at)
		      VALUES ('http-rate', ?, 'replica-1', ?, 1, ?)`, uuid.NewString(), at.Truncate(time.Minute), at)
	}
	// A claim nobody answered, left by a replica that died a month ago.
	seed(`INSERT INTO idempotency_records (record_key, request_hash, completed, status_code, created_at)
	      VALUES (?, 'hash', false, 0, ?)`, uuid.NewString(), monthAgo)
	// A claim past the TTL and still being worked on: nothing bounds how long a
	// request runs, so an hour-old claim may still be answered.
	seed(`INSERT INTO idempotency_records (record_key, request_hash, completed, status_code, created_at)
	      VALUES (?, 'hash', false, 0, ?)`, uuid.NewString(), now.Add(-time.Hour))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a.startBackgroundWork(ctx)

	want := map[string]int64{"webhook_deliveries": 1, "idempotency_records": 2, "shared_counters": 1}
	left := map[string]int64{}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		for table := range want {
			var n int64
			if err := gormDB.WithContext(t.Context()).Raw("SELECT count(*) FROM " + table).Scan(&n).Error; err != nil {
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
