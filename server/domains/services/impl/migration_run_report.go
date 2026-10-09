package impl

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// What a request's outcome says of its run.
const (
	// outcomeChanged and outcomePassedOver are how many instances the run
	// acted on and how many it left alone. A run's report always has both,
	// and nothing else writes either: an outcome with outcomeChanged is one a
	// run reported.
	outcomeChanged    = "changed"
	outcomePassedOver = "passed_over"
	// outcomeError is why a request is interrupted, in a sentence of the
	// server's own.
	outcomeError = "error"
	// outcomeReportedAfterSweep marks a report written over the sweep's
	// "interrupted": the run outlived the time it was given, and finished.
	outcomeReportedAfterSweep = "reported_after_sweep"
)

// runOutcome is what a run's report writes as its request's outcome: how
// many instances it acted on and how many it passed over, and — for a run
// that failed — why, in a sentence of the server's own.
//
// The failure's own words are not stored. An outcome is kept in clear and
// read by every administrator of the organization, and what a run fails on
// can carry anything: a gateway's condition, a constraint's values. They go
// to the server's log (traceApprovedRunStopped). The one failure told in its
// own words is the gate's refusal, whose words are this server's and name
// only the request and instances by id.
func runOutcome(result entities.MigrationResult, runErr error) map[string]any {
	outcome := map[string]any{outcomeChanged: result.Changed, outcomePassedOver: len(result.PassedOver)}
	if runErr == nil {
		return outcome
	}
	var refused gateRefusal
	if errors.As(runErr, &refused) {
		outcome[outcomeError] = "the run was refused before it moved anything: " + refused.why
		return outcome
	}
	outcome[outcomeError] = fmt.Sprintf("the run stopped on a failure after it had acted on %d instance(s); what it had done by then stands", result.Changed)
	return outcome
}

// reportedByARun reports whether a request's stored outcome is a run's own
// report, as against the sweep's mark or an approval's note.
func reportedByARun(outcome map[string]any) bool {
	_, reported := outcome[outcomeChanged]
	return reported
}

// reportRun is step C: it writes what an approved run did on its request,
// and answers the request as it then is.
//
// One unit of work, on a context the caller cannot cancel — the run has
// happened, and its report is owed whatever became of the call — holding the
// request's row. That row is what orders this report and the sweep: each
// takes it, and the sweep passes by a row that is held.
//
// A request still approved becomes applied, or interrupted with what the run
// reached. One the sweep marked interrupted while the run was still going —
// the run outlived the time it was given — is written over: the run knows
// what happened, the sweep knew only that a deadline had passed. That is
// decided from the stored outcome under the lock: only the sweep's mark is
// written over. A request that already carries a run's report is left as it
// is, and so is one at any other status; both are said loudly, because
// neither should be possible — this runs once, for the call that approved.
//
// What a self-approval rested on is carried from the stored outcome into the
// report: the report replaces the outcome, and that record must outlive it.
//
// A failure here is logged loudly and does not hide what the run did: the
// request is then answered as it could last be read, and reads interrupted
// once its window closes.
func (s *migrationService) reportRun(
	ctx context.Context,
	approved entities.DeviationRequest,
	result entities.MigrationResult,
	runErr error,
	began time.Time,
) entities.DeviationRequest {
	ctx = context.WithoutCancel(ctx)
	requests := s.repo.DeviationRequest()
	to := entities.DeviationRequestApplied
	if runErr != nil {
		to = entities.DeviationRequestInterrupted
	}
	reported := approved
	err := s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		stored, err := requests.GetForUpdateWithoutDocuments(txCtx, approved.ID)
		if err != nil {
			return err
		}
		outcome := runOutcome(result, runErr)
		maps.Copy(outcome, carriedSelfApproval(stored.Outcome))
		switch {
		case stored.Status == entities.DeviationRequestApproved:
		case stored.Status == entities.DeviationRequestInterrupted && !reportedByARun(stored.Outcome):
			outcome[outcomeReportedAfterSweep] = true
			log.Warn().Str("request", approved.ID.String()).Dur("run_took", time.Since(began)).Str("reported", string(to)).
				Msg("An approved migration was still running when the time it is given had passed, and was marked interrupted. " +
					"It has now finished and its own report stands over that mark.")
		default:
			reported = stored
			log.Error().Str("request", approved.ID.String()).Str("status", string(stored.Status)).Str("would_report", string(to)).
				Msg("An approved migration's run came to report and found its request already closed by something else. " +
					"The request was left as it is; what the run did stands and is not on the request.")
			return nil
		}
		reported, err = requests.Transition(txCtx, approved.ID, stored.Status, repocontracts.DeviationRequestChange{Status: to, Outcome: outcome})
		return err
	})
	if err != nil {
		log.Error().Err(err).Str("request", approved.ID.String()).Str("would_report", string(to)).
			Int("changed", result.Changed).Int("passed_over", len(result.PassedOver)).
			Msg("An approved migration ran and its report could not be written on its request. What the run did stands. " +
				"The request still says approved, and will read interrupted once the time a run is given has passed.")
		if read, readErr := requests.GetReadable(ctx, approved.ID); readErr == nil {
			return read
		}
		return approved
	}
	return reported
}

// traceApprovedRunStopped says in the server's log that a migration a second
// administrator approved was run and failed, with the failure in its own
// words — which the request's outcome does not keep.
//
// The line names the request, who asked and who approved, by account as well
// as by name, and how far the run got. It is the only place the failure's
// own words are written.
func traceApprovedRunStopped(request entities.DeviationRequest, result entities.MigrationResult, runErr error) {
	log.Error().Err(runErr).Str("request", request.ID.String()).
		Str("requested_by", request.RequestedBy).Str("requested_by_id", request.RequestedByID.String()).
		Str("approved_by", request.DecidedBy).Str("approved_by_id", request.DecidedByID.String()).
		Int("changed", result.Changed).Int("passed_over", len(result.PassedOver)).
		Msg("An approved migration stopped part-way. Its request reads interrupted; what it had done stands, " +
			"and what remains has to be asked for again.")
}
