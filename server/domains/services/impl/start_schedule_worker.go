package impl

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// startScheduleWorker looks, at the job worker's own pace, for scheduled
// starts that have come due.
//
// Its own loop rather than a step of the job poll, so a round of starts never
// holds jobs up and a slow job round never makes a scheduled run late. It
// needs no leader: every replica runs one, and the row lock each look takes
// is what makes a scheduled time start one run.
//
// ctx is the job worker's, already marked as system work and bound to one
// runtime's database.
func (s *jobService) startScheduleWorker(ctx context.Context) {
	ticker := time.NewTicker(s.settings.PollInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			// A panic here would end the loop, and with it every schedule.
			if err := runRecovered("scheduled starts", func() error {
				s.startDueSchedules(ctx)
				return nil
			}); err != nil {
				// Logged with its stack by runRecovered; the next tick looks again.
				continue
			}
		}
	}()
}

// startDueSchedules runs one round. The loop cannot return anything, so what
// failed is logged here; each failure is also kept on its process's row.
func (s *jobService) startDueSchedules(ctx context.Context) {
	started, err := s.engine.StartDueSchedules(ctx, time.Now().UTC())
	if err != nil {
		log.Error().Err(err).Msg("Some scheduled starts could not be run; they will be tried again")
	}
	if started > 0 {
		log.Info().Int("started", started).Msg("Started scheduled runs")
	}
}
