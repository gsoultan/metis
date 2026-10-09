package app

import (
	"context"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors/security"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// retentionSweepEvery is how often the tables that only ever grow are cut
// back. Their retention runs to minutes, hours and days, so a sweep more often
// than this would mostly find nothing to do.
const retentionSweepEvery = 10 * time.Minute

// sharedCountKept is how far back a rate-limit window has to start before it
// is removed: several one-minute windows, so only closed ones go, and a
// replica finishing a late exchange for the window that just closed still
// finds its row.
const sharedCountKept = 5 * time.Minute

// startRetentionSweeps cuts back the tables whose rows answer a question only
// for a while: has this webhook delivery been seen, has this request already
// been done, how much has this client sent this minute.
//
// Each of those models says a retention sweep works from its timestamp, and
// nothing ever started one. So all three grew for as long as the server ran,
// two of them keyed by whatever callers chose to send.
//
// The same pass re-offers external tasks stranded at zero retries (see
// reoffer), records the expiry of requests for a second administrator that
// nobody decided in time (see expire), and gives a ledger row written by a
// pod of the previous release the key that holds its visit (see
// fillLiveKeys).
//
// It sweeps once at start-up, because an installation that is redeployed more
// often than the interval would otherwise never sweep, and then on a timer.
// Every replica sweeps. The deletes are idempotent, so two replicas sweeping at
// once cost one of them a wait on the other's row locks, which is cheaper than
// electing a single sweeper.
func (a *App) startRetentionSweeps(ctx context.Context) {
	ctx = entities.WithSystemContext(ctx)
	go func() {
		ticker := time.NewTicker(retentionSweepEvery)
		defer ticker.Stop()
		for {
			a.sweepRetention(ctx, time.Now())
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// sweepRetention runs one pass over every table and every database that holds
// one.
func (a *App) sweepRetention(ctx context.Context, now time.Time) {
	forget(ctx, "shared_counters", "main", func(ctx context.Context) (int64, error) {
		return a.repo.SharedCounter().Prune(ctx, now.Add(-sharedCountKept))
	})

	// Deliveries and idempotency records are kept in the database of the port
	// the request arrived on, so a webhook posted to a staging port is
	// remembered in staging, and each environment needs its own sweep.
	a.sweepRuntime(ctx, "main", now)
	fillLiveKeys(ctx, "main", a.db)
	for _, id := range gorms.OpenEnvironmentIDs() {
		a.sweepRuntime(db.Bind(ctx, id), id.String(), now)
		if environment, open := gorms.EnvironmentDB(id); open {
			fillLiveKeys(ctx, id.String(), environment)
		}
	}
}

func (a *App) sweepRuntime(ctx context.Context, database string, now time.Time) {
	reoffer(ctx, database, a.repo.ExternalTask().ReofferStranded)
	forget(ctx, "webhook_deliveries", database, a.svc.ForgetOldDeliveries)
	// No storm connection means idempotency records are held in the serving
	// process, which sweeps its own.
	if a.storm != nil {
		forget(ctx, "idempotency_records", database, func(ctx context.Context) (int64, error) {
			return security.ForgetIdempotencyRecords(ctx, a.storm, defaultHTTPIdempotencyTTL, now)
		})
	}
	expire(ctx, database, func(ctx context.Context) (int64, error) {
		return a.svc.ExpireDeviationRequests(ctx, now)
	})
}

// expire records the expiry of requests for a second administrator that
// nobody decided before their deadline. Not retention, but the same shape as
// reoffer: every replica, every database, safe to run twice.
//
// The clock has already decided those requests: each reads as expired and
// cannot be approved whether or not this has run. What the pass adds is the
// record — the request and its ledger row say expired, the trail says so, and
// the step the request held can be asked for again without anybody having to
// ask. A pass can close some requests and fail on another, so it says what it
// closed and, separately, that it could not finish.
func expire(ctx context.Context, database string, run func(context.Context) (int64, error)) {
	expired, err := run(ctx)
	if expired > 0 {
		log.Info().Str("database", database).Int64("expired", expired).
			Msg("Recorded the expiry of requests waiting for a second administrator that nobody decided in time")
	}
	if err != nil {
		log.Warn().Err(err).Str("database", database).
			Msg("Could not record the expiry of requests waiting for a second administrator; they read as expired and none can be approved, " +
				"but the table says pending until a sweep succeeds.")
	}
}

// fillLiveKeys gives every ledger row that holds a visit and has no live key
// its key, on one database.
//
// Migration 34 does this once. A pod of the previous release still writes
// such rows — it knows nothing of the column — and one runs beside this
// release during a rolling upgrade, and again after a rollback. The
// application's own guard against a second act on a visit does not need the
// key: it reads the visit's row under the instance's lock. The database's
// guard, the unique index on the live key, does, and sees nothing of a row
// without one. This closes that gap within one pass of the row being written.
//
// Filling any is worth a warning, as re-offering a task is: in steady state
// there are none, and each one means a pod of another release is, or was,
// writing to this database. With none to fill it says nothing.
func fillLiveKeys(ctx context.Context, database string, ledger *gorm.DB) {
	// A wiring with no database handle (tests only) has no ledger to fill.
	if ledger == nil || ledger.Name() != "postgres" {
		return
	}
	filled, err := migrations.GiveLiveRowsTheirKey(ctx, ledger)
	if filled > 0 {
		log.Warn().Str("database", database).Int("filled", filled).
			Msg("Gave ledger rows written without it the key that holds their visit; a pod of an earlier release is, or was, writing to this database")
	}
	if err != nil {
		log.Warn().Err(err).Str("database", database).
			Msg("Could not give ledger rows written by an earlier release the key that holds their visit; " +
				"the application still refuses a second act on such a visit, and the next sweep tries again.")
	}
}

// reoffer puts back on offer the external tasks that ran out of retries with
// no incident to say so. Not retention, but the same shape: every replica, every
// database, idempotent. Repairing any is worth a warning, since in steady state
// there are none — each one was work nobody was offered and nobody was told of.
func reoffer(ctx context.Context, database string, run func(context.Context) (int64, error)) {
	reoffered, err := run(ctx)
	if err != nil {
		log.Warn().Err(err).Str("database", database).
			Msg("Could not re-offer external tasks that ran out of retries with no incident; they stay off offer until a sweep succeeds.")
		return
	}
	if reoffered > 0 {
		log.Warn().Str("database", database).Int64("reoffered", reoffered).
			Msg("Re-offered external tasks that had run out of retries with no incident to say so")
	}
}

// forget runs one sweep and says what came of it. The loop has nobody to
// return an error to, and a sweep that had stopped working would otherwise
// look exactly like one with nothing to do.
func forget(ctx context.Context, table, database string, prune func(context.Context) (int64, error)) {
	removed, err := prune(ctx)
	if err != nil {
		log.Warn().Err(err).Str("table", table).Str("database", database).Int64("removed", removed).
			Msg("A retention sweep failed; the table keeps growing until one succeeds.")
		return
	}
	if removed > 0 {
		log.Info().Str("table", table).Str("database", database).Int64("removed", removed).
			Msg("Retention sweep")
	}
}
