package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gsoultan/metis/internal/pkg/envvar"
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
// The same pass re-offers external tasks stranded at zero retries; see
// reoffer. It also cuts back variable snapshots and finished jobs, but only
// when an operator has said for how long to keep them; see optInRetention.
//
// It sweeps once at start-up, because an installation that is redeployed more
// often than the interval would otherwise never sweep, and then on a timer.
// Every replica sweeps. The deletes are idempotent, so two replicas sweeping at
// once cost one of them a wait on the other's row locks, which is cheaper than
// electing a single sweeper.
func (a *App) startRetentionSweeps(ctx context.Context) {
	ctx = entities.WithSystemContext(ctx)
	keep := resolveOptInRetention()
	keep.announce()
	go func() {
		ticker := time.NewTicker(retentionSweepEvery)
		defer ticker.Stop()
		for {
			a.sweepRetention(ctx, time.Now(), keep)
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
func (a *App) sweepRetention(ctx context.Context, now time.Time, keep optInRetention) {
	forget(ctx, "shared_counters", "main", func(ctx context.Context) (int64, error) {
		return a.repo.SharedCounter().Prune(ctx, now.Add(-sharedCountKept))
	})

	// Deliveries and idempotency records are kept in the database of the port
	// the request arrived on, so a webhook posted to a staging port is
	// remembered in staging, and each environment needs its own sweep.
	a.sweepRuntime(ctx, "main", now, keep)
	for _, id := range gorms.OpenEnvironmentIDs() {
		a.sweepRuntime(db.Bind(ctx, id), id.String(), now, keep)
	}
}

func (a *App) sweepRuntime(ctx context.Context, database string, now time.Time, keep optInRetention) {
	reoffer(ctx, database, a.repo.ExternalTask().ReofferStranded)
	forget(ctx, "webhook_deliveries", database, a.svc.ForgetOldDeliveries)
	// No storm connection means idempotency records are held in the serving
	// process, which sweeps its own.
	if a.storm != nil {
		forget(ctx, "idempotency_records", database, func(ctx context.Context) (int64, error) {
			return security.ForgetIdempotencyRecords(ctx, a.storm, defaultHTTPIdempotencyTTL, now)
		})
	}
	// The two an operator opts into. Instances live in each runtime's
	// database, so their snapshots and jobs do too.
	if keep.variableSnapshots > 0 {
		forget(ctx, "variable_snapshots", database, func(ctx context.Context) (int64, error) {
			return a.repo.VariableSnapshot().ForgetCapturedBefore(ctx, now.Add(-keep.variableSnapshots))
		})
	}
	if keep.finishedJobs > 0 {
		forget(ctx, "jobs", database, func(ctx context.Context) (int64, error) {
			return a.repo.Job().ForgetFinishedBefore(ctx, now.Add(-keep.finishedJobs))
		})
	}
}

// The retention an operator opts into, in days. Unset, nothing is removed:
// these rows are history somebody may still read, unlike the three above,
// which answer a question only for minutes or hours.
const (
	envRetainVariableSnapshotsDays = "METIS_RETENTION_VARIABLE_SNAPSHOTS_DAYS"
	envRetainCompletedJobsDays     = "METIS_RETENTION_COMPLETED_JOBS_DAYS"
)

// maxRetentionDays bounds the setting so a typo of extra digits is refused
// rather than turned into a duration that overflows.
const maxRetentionDays = 36_500

// optInRetention is how long the swept-on-request tables keep a row; zero
// means the table is not swept at all.
//
// Only these two. Process instances, tasks and the audit trail are business
// records — what was decided, by whom, and when — and an engine that deleted
// them on a timer would be deleting the evidence of somebody's commitments.
type optInRetention struct {
	variableSnapshots time.Duration
	finishedJobs      time.Duration
}

func resolveOptInRetention() optInRetention {
	return optInRetention{
		variableSnapshots: retentionDays(envRetainVariableSnapshotsDays),
		finishedJobs:      retentionDays(envRetainCompletedJobsDays),
	}
}

// retentionDays reads one setting. Anything that is not a whole number of days
// from 1 up is off, and said to be: a setting that deletes rows must not take
// a guess at what was meant.
func retentionDays(name string) time.Duration {
	raw := strings.TrimSpace(envvar.Get(name))
	if raw == "" || raw == "0" {
		return 0
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days < 1 || days > maxRetentionDays {
		log.Warn().Str("setting", name).Str("value", raw).
			Msgf("Ignoring a retention setting that is not a whole number of days between 1 and %d; nothing is removed by it.", maxRetentionDays)
		return 0
	}
	return time.Duration(days) * 24 * time.Hour
}

// announce says at boot which of the opt-in sweeps are on, because each one
// deletes rows and an operator reading the log should not have to infer it.
func (k optInRetention) announce() {
	if k.variableSnapshots > 0 {
		log.Info().Str("table", "variable_snapshots").Dur("kept", k.variableSnapshots).
			Msg("Variable snapshots older than the retention period are removed")
	}
	if k.finishedJobs > 0 {
		log.Info().Str("table", "jobs").Dur("kept", k.finishedJobs).
			Msg("Completed jobs, and failed ones no open incident names, older than the retention period are removed")
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
