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
	"github.com/gsoultan/metis/server/repositories/payloads"
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

// payloadSweepGrace is how long an instance's payload directory is left
// alone after it last changed, whatever the database says: longer than any
// transaction that creates an instance.
const payloadSweepGrace = time.Hour

// startRetentionSweeps cuts back the tables whose rows answer a question only
// for a while: has this webhook delivery been seen, has this request already
// been done, how much has this client sent this minute.
//
// Each of those models says a retention sweep works from its timestamp, and
// nothing ever started one. So all three grew for as long as the server ran,
// two of them keyed by whatever callers chose to send.
//
// The same pass re-offers external tasks stranded at zero retries (see
// reoffer), records what the clock has decided about requests for a second
// administrator (see expire: those nobody decided in time have expired, and
// approved ones whose run never reported were interrupted).
// It also cuts back variable snapshots and finished jobs, but only when an
// operator has said for how long to keep them; see optInRetention.
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
		var announced optInRetention
		for i := 0; ; i++ {
			// Read again each pass: the periods can be changed on the Settings
			// page, and a new one should not wait for a restart. Said again
			// whenever it changes, because each one deletes rows.
			keep := resolveOptInRetention()
			if i == 0 || keep != announced {
				keep.announce()
				announced = keep
			}
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
	if a.storm == nil {
		return
	}
	for id := range a.storm.EnvironmentPools() {
		a.sweepRuntime(db.Bind(ctx, id), id.String(), now, keep)
	}
}

func (a *App) sweepRuntime(ctx context.Context, database string, now time.Time, keep optInRetention) {
	reoffer(ctx, database, a.repo.ExternalTask().ReofferStranded)
	forget(ctx, "webhook_deliveries", database, a.svc.ForgetOldDeliveries)
	// A worker is kept by the tasks' database, so each runtime forgets its own.
	forget(ctx, "external_task_workers", database, func(ctx context.Context) (int64, error) {
		return a.repo.ExternalTaskWorker().ForgetIdle(ctx, now.Add(-entities.WorkerForgottenAfter))
	})
	forget(ctx, "outgoing_webhook_deliveries", database, a.svc.ForgetOldOutgoingDeliveries)
	// No storm connection means idempotency records are held in the serving
	// process, which sweeps its own.
	if a.storm != nil {
		forget(ctx, "idempotency_records", database, func(ctx context.Context) (int64, error) {
			return security.ForgetIdempotencyRecords(ctx, a.storm, defaultHTTPIdempotencyTTL, now)
		})
	}
	// The ones an operator opts into. Instances live in each runtime's
	// database, so their snapshots and jobs do too.
	if keep.variableSnapshots > 0 {
		forget(ctx, "variable_snapshots", database, func(ctx context.Context) (int64, error) {
			return a.repo.VariableSnapshot().ForgetCapturedBefore(ctx, now.Add(-keep.variableSnapshots))
		})
	}
	// A removed instance's kept values go after it: those of instances the
	// retention sweep removed, and of instances whose creation was rolled back.
	// The hour leaves alone an instance being created now, whose row is not
	// committed yet.
	if payloads.Enabled() {
		forget(ctx, "payloads", database, func(ctx context.Context) (int64, error) {
			removed, err := payloads.SweepGone(ctx, now.Add(-payloadSweepGrace), a.repo.Process().Existing)
			return int64(removed), err
		})
	}
	if keep.finishedJobs > 0 {
		forget(ctx, "jobs", database, func(ctx context.Context) (int64, error) {
			return a.repo.Job().ForgetFinishedBefore(ctx, now.Add(-keep.finishedJobs))
		})
	}
	// Main database only. Finished instances are archived, not removed: see
	// ArchiveFinished. The deviation ledger, which decides whether an
	// instance is kept, lives there; an environment's database has none, so
	// nothing there could say an instance is still needed.
	//
	// It runs whether or not the installation has a period: an administrator
	// may have given one process a period of its own, and the sweep then
	// removes that process's instances and no other's. With no period
	// anywhere it reads one row and archives nothing.
	if database == "main" {
		forget(ctx, "process_instances", database, func(ctx context.Context) (int64, error) {
			return a.repo.Process().ArchiveFinished(ctx, now, keep.finishedInstances)
		})
		if keep.archive > 0 {
			forget(ctx, "archived_instances", database, func(ctx context.Context) (int64, error) {
				return a.repo.Archive().ForgetArchivedBefore(ctx, now.Add(-keep.archive))
			})
		}
	}
	expire(ctx, database, func(ctx context.Context) (entities.SweptRequests, error) {
		return a.svc.SweepDeviationRequests(ctx, now)
	})
}

// The retention an operator sets, in days. Unset, variable snapshots, jobs
// and archived instances are kept: these rows are history somebody may still
// read, unlike the three above, which answer a question only for minutes or
// hours. Finished instances are archived after defaultArchiveAfterDays unless
// set otherwise; archiving removes nothing, it moves an instance out of the
// tables the engine works in.
const (
	envRetainVariableSnapshotsDays = "METIS_RETENTION_VARIABLE_SNAPSHOTS_DAYS"
	envRetainCompletedJobsDays     = "METIS_RETENTION_COMPLETED_JOBS_DAYS"
	envRetainFinishedInstancesDays = "METIS_RETENTION_FINISHED_INSTANCES_DAYS"
	envRetainArchiveDays           = "METIS_RETENTION_ARCHIVE_DAYS"
)

// defaultArchiveAfterDays is how long a finished instance stays live when
// METIS_RETENTION_FINISHED_INSTANCES_DAYS is not set. 0 keeps every one live.
const defaultArchiveAfterDays = 90

// maxRetentionDays bounds the setting so a typo of extra digits is refused
// rather than turned into a duration that overflows.
const maxRetentionDays = 36_500

// optInRetention is how long the swept-on-request tables keep a row; zero
// means the table is not swept at all.
//
// Finished instances are business records — what was decided, and when — so
// they are archived, not removed, and then with care: see
// ProcessRepository.ArchiveFinished, which keeps live any instance a deviation
// was recorded or asked for on. Archived instances are removed only when an
// operator has said for how long to keep the archive.
type optInRetention struct {
	variableSnapshots time.Duration
	finishedJobs      time.Duration
	finishedInstances time.Duration
	archive           time.Duration
}

func resolveOptInRetention() optInRetention {
	finished := retentionDays(envRetainFinishedInstancesDays)
	if raw, set := envvar.Lookup(envRetainFinishedInstancesDays); !set || strings.TrimSpace(raw) == "" {
		finished = defaultArchiveAfterDays * 24 * time.Hour
	}
	return optInRetention{
		variableSnapshots: retentionDays(envRetainVariableSnapshotsDays),
		finishedJobs:      retentionDays(envRetainCompletedJobsDays),
		finishedInstances: finished,
		archive:           retentionDays(envRetainArchiveDays),
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
	if k.finishedInstances > 0 {
		log.Info().Str("table", "process_instances").Dur("kept", k.finishedInstances).
			Msg("Completed and cancelled instances older than the retention period are archived with their tasks, " +
				"service calls, incidents, notifications, variable history and audit trail, and can be opened from the project's archive; " +
				"any instance with a deviation, an open incident or a retention hold stays live")
	}
	if k.archive > 0 {
		log.Info().Str("table", "archived_instances").Dur("kept", k.archive).
			Msg("Archived instances older than the archive's retention period are removed")
	}
}

// expire writes down what the clock has decided about the requests for a
// second administrator. Not retention, but the same shape as reoffer: every
// replica, every database, safe to run twice.
//
// Two things, said apart because they are different facts. A request that
// still waited past its deadline has expired: nobody decided it. An approved
// request whose run did not report back in the time a run is given was
// interrupted: somebody did decide, and the run then stopped — a server that
// went down mid-run leaves exactly this, so it is worth a warning.
//
// The clock has already decided each of them: it reads as expired, or as
// interrupted, and cannot be approved or run under whether or not this has
// run. What the pass adds is the record — the request says so, a waive's
// ledger row and trail say so, and what the request held can be asked for
// again without anybody having to ask. A pass can close some requests and
// fail on another, so it says what it closed and, separately, that it could
// not finish.
func expire(ctx context.Context, database string, run func(context.Context) (entities.SweptRequests, error)) {
	swept, err := run(ctx)
	if swept.Expired > 0 {
		log.Info().Str("database", database).Int64("expired", swept.Expired).
			Msg("Recorded the expiry of requests waiting for a second administrator that nobody decided in time")
	}
	if swept.Interrupted > 0 {
		log.Warn().Str("database", database).Int64("interrupted", swept.Interrupted).
			Msg("Recorded that approved requests for a second administrator were interrupted: each had been approved, and no run of it " +
				"reported back in the time one is given. What such a run had done stands; what remains has to be asked for again.")
	}
	if err != nil {
		log.Warn().Err(err).Str("database", database).
			Msg("Could not record everything the clock has decided about requests for a second administrator; each reads as expired " +
				"or interrupted and none can be approved or run under, but its row says otherwise until a sweep succeeds.")
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
