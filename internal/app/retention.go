package app

import (
	"context"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors/security"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/rs/zerolog/log"
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
	for _, id := range gorms.OpenEnvironmentIDs() {
		a.sweepRuntime(db.Bind(ctx, id), id.String(), now)
	}
}

func (a *App) sweepRuntime(ctx context.Context, database string, now time.Time) {
	forget(ctx, "webhook_deliveries", database, a.svc.ForgetOldDeliveries)
	// No storm connection means idempotency records are held in the serving
	// process, which sweeps its own.
	if a.storm != nil {
		forget(ctx, "idempotency_records", database, func(ctx context.Context) (int64, error) {
			return security.ForgetIdempotencyRecords(ctx, a.storm, defaultHTTPIdempotencyTTL, now)
		})
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
