package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestTheEngineCollectorReportsTheBacklog(t *testing.T) {
	collector := NewEngineCollector(func(context.Context) (EngineState, error) {
		return EngineState{DueJobs: 12, OldestDueAge: 90 * time.Second, LeaseExpiredJobs: 2, OpenIncidents: 5}, nil
	})
	for name, want := range map[string]float64{
		"metis_engine_state_up":             1,
		"metis_jobs_due":                    12,
		"metis_jobs_oldest_due_age_seconds": 90,
		"metis_jobs_lease_expired":          2,
		"metis_incidents_open":              5,
	} {
		if got := gathered(t, collector, name); got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}

// A database that cannot answer must not look like an empty queue. The backlog
// series go absent and `up` says why, so an alert on the backlog cannot be
// quieted by the very outage it should be reporting.
func TestAnUnreadableBacklogIsNotReportedAsAnEmptyOne(t *testing.T) {
	collector := NewEngineCollector(func(context.Context) (EngineState, error) {
		return EngineState{}, errors.New("connection refused")
	})
	if got := gathered(t, collector, "metis_engine_state_up"); got != 0 {
		t.Fatalf("up = %v, want 0", got)
	}
	if n := testutil.CollectAndCount(collector, "metis_jobs_due", "metis_jobs_oldest_due_age_seconds"); n != 0 {
		t.Fatalf("%d backlog series reported while the backlog could not be read", n)
	}
}

// The probe gets a deadline: a scrape must not wait on a database that has
// stopped answering.
func TestTheEngineProbeHasADeadline(t *testing.T) {
	var deadline time.Time
	collector := NewEngineCollector(func(ctx context.Context) (EngineState, error) {
		deadline, _ = ctx.Deadline()
		return EngineState{}, nil
	})
	gathered(t, collector, "metis_engine_state_up")
	if deadline.IsZero() || time.Until(deadline) > engineProbeBudget {
		t.Fatalf("the probe ran with deadline %v", deadline)
	}
}
