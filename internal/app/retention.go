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
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// retentionSweepEvery is how often the tables that only ever grow are cut
// back. Their retention runs to minutes, hours and days, so a sweep more often
// than this would mostly find nothing to do.
const retentionSweepEvery = 10 * time.Minute

// liveKeyFillFor is how long after this process started its sweeps a pass
// still looks for ledger rows written without their live key (fillLiveKeys).
//
// Only a pod of the previous release writes such a row, and one runs beside
// this release only around a rolling upgrade or a rollback — each of which
// restarts every pod of this release, and so starts this hour again. Looking
// for them is a search of the ledger that no index serves and that finds
// nothing, almost always; made on every pass for ever it would be read load
// that grows with the ledger, on every database, from every replica. An hour
// is several times a rollout. A keyless row written later than that stays
// keyless until the next restart; the application's own guard, which reads a
// visit's row under the instance's lock, does not need the key.
const liveKeyFillFor = time.Hour

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
// reoffer), records what the clock has decided about requests for a second
// administrator (see expire: those nobody decided in time have expired, and
// approved ones whose run never reported were interrupted), and — for the
// first hour after it starts — gives a ledger row written by a pod of the
// previous release the key that holds its visit (see fillLiveKeys,
// liveKeyFillFor).
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
	keep := resolveOptInRetention()
	keep.announce()
	a.sweepsBegan = time.Now()
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
	fill := a.stillFillsLiveKeys(now)
	a.sweepRuntime(ctx, "main", now, keep)
	if fill {
		fillLiveKeys(ctx, "main", a.db)
	}
	for _, id := range gorms.OpenEnvironmentIDs() {
		a.sweepRuntime(db.Bind(ctx, id), id.String(), now, keep)
		if environment, open := gorms.EnvironmentDB(id); open && fill {
			fillLiveKeys(ctx, id.String(), environment)
		}
	}
}

// stillFillsLiveKeys reports whether a pass at now is within liveKeyFillFor
// of this process starting its sweeps. A process that never started them has
// no such hour, and fills nothing.
func (a *App) stillFillsLiveKeys(now time.Time) bool {
	return !a.sweepsBegan.IsZero() && now.Before(a.sweepsBegan.Add(liveKeyFillFor))
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
	expire(ctx, database, func(ctx context.Context) (entities.SweptRequests, error) {
		return a.svc.SweepDeviationRequests(ctx, now)
	})
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

// fillLiveKeys gives every ledger row that holds a visit and has no live key
// its key, on one database.
//
// Migration 34 does this once. A pod of the previous release still writes
// such rows — it knows nothing of the column — and one runs beside this
// release during a rolling upgrade, and again after a rollback. The
// application's own guard against a second act on a visit does not need the
// key: it reads the visit's row under the instance's lock. The database's
// guard, the unique index on the live key, does, and sees nothing of a row
// without one. This closes that gap within one pass of the row being written
// — for as long as passes look (liveKeyFillFor): the search is a read of the
// ledger's live rows that no index serves, so it is not made for ever.
//
// Filling any is worth a warning, as re-offering a task is: in steady state
// there are none, and each one means a pod of another release is, or was,
// writing to this database. With none to fill it says nothing.
//
// One failure is named apart from the rest. The index is unique, so a keyless
// row cannot be given its key while another live row already holds that visit
// of that instance — and two live rows for one visit is the very thing the
// key exists to make impossible: one act recorded twice, or two acts made.
// Nothing in the product leaves that behind; if a pod of the previous release
// did, it is said as what it is, at the level of an error, and left for a
// person: no pass can choose which of the two rows is the true one.
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
	switch {
	case err == nil:
	case migrations.VisitHeldTwice(err):
		log.Error().Err(err).Str("database", database).
			Msg("Two live ledger rows hold one visit of one instance: a row written without its live key cannot be given it, because another row " +
				"already holds that visit. One act was recorded twice, or two were made. The fill stops at such a pair, so any other row of this " +
				"database written without its key was not given it either. Read both rows of that instance and close the one that is not true; " +
				"until then every pass of the first hour after a start says this again, and fills nothing behind the pair.")
	default:
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
