package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

const day = 24 * time.Hour

// periodSweep is a heldSweep with a second process, payroll, beside claims,
// so a test can give one process a period and watch the other keep the
// installation's.
type periodSweep struct {
	heldSweep
	payroll uuid.UUID
}

func newPeriodSweep(t *testing.T, installationDays string) periodSweep {
	t.Helper()
	h := newHeldSweep(t)
	t.Setenv(envRetainFinishedInstancesDays, installationDays)
	h.keep = resolveOptInRetention()
	payroll := uuid.Must(uuid.NewV7())
	if err := h.repo.Definition().Create(h.tenant, models.ProcessDefinitionModel{
		Base: models.Base{ID: models.FromUUID(payroll)}, ProjectID: models.FromUUID(h.project),
		Key: "payroll", Name: "Payroll", Version: 1,
	}); err != nil {
		t.Fatalf("seed the second definition: %v", err)
	}
	return periodSweep{heldSweep: h, payroll: payroll}
}

// finishedAgo seeds an instance of definition that completed age ago.
func (p periodSweep) finishedAgo(definition uuid.UUID, age time.Duration) uuid.UUID {
	p.t.Helper()
	id := uuid.Must(uuid.NewV7())
	if _, err := p.repo.Process().Create(p.tenant, models.ProcessInstanceModel{
		Base: models.Base{ID: models.FromUUID(id)}, ProjectID: models.FromUUID(p.project),
		DefinitionID: models.FromUUID(definition), Status: models.ProcessCompleted,
	}); err != nil {
		p.t.Fatalf("seed an instance: %v", err)
	}
	p.exec(`UPDATE process_instances SET updated_at = ? WHERE id = ?`, time.Now().Add(-age), id)
	return id
}

// period gives claims a period of days, or returns it to the installation's
// setting with nil.
func (p periodSweep) period(ctx context.Context, days *int) error {
	_, err := p.repo.RetentionPeriod().Set(ctx, entities.RetentionPeriod{
		Project: &entities.Project{ID: p.project}, ProcessKey: "claims", Days: days,
		Reason: "records schedule 4.2", SetBy: "ada", SetByID: uuid.Must(uuid.NewV7()),
	})
	return err
}

func (p periodSweep) setPeriod(days *int) {
	p.t.Helper()
	if err := p.period(p.tenant, days); err != nil {
		p.t.Fatalf("set the period: %v", err)
	}
}

func days(n int) *int { return &n }

// expect checks which of the instances a sweep left.
func (p periodSweep) expect(kept, removed map[string]uuid.UUID) {
	p.t.Helper()
	for name, id := range kept {
		if !p.left(id) {
			p.t.Errorf("the sweep removed %s", name)
		}
	}
	for name, id := range removed {
		if p.left(id) {
			p.t.Errorf("the sweep kept %s", name)
		}
	}
}

// A process given a longer period than the installation's keeps its
// instances for that long, every other process keeps the installation's, and
// a process returned to the installation's setting is swept by it again.
func TestAProcessKeepsItsFinishedInstancesForItsOwnPeriod(t *testing.T) {
	p := newPeriodSweep(t, "30")
	p.setPeriod(days(365))
	claimsInside := p.finishedAgo(p.definition, 100*day)
	claimsPast := p.finishedAgo(p.definition, 400*day)
	payrollPast := p.finishedAgo(p.payroll, 100*day)
	payrollInside := p.finishedAgo(p.payroll, 10*day)
	p.sweep()
	p.expect(
		map[string]uuid.UUID{"a claim inside its process's year": claimsInside, "a payroll run inside the installation's thirty days": payrollInside},
		map[string]uuid.UUID{"a claim past its process's year": claimsPast, "a payroll run past the installation's thirty days": payrollPast},
	)

	p.setPeriod(nil)
	p.sweep()
	p.expect(nil, map[string]uuid.UUID{"a claim past the installation's period once its process followed it again": claimsInside})
}

// A shorter period than the installation's removes that process's instances
// sooner, and no other process's.
func TestAProcessGivenAShorterPeriodIsSweptSooner(t *testing.T) {
	p := newPeriodSweep(t, "30")
	p.setPeriod(days(7))
	claim := p.finishedAgo(p.definition, 10*day)
	payroll := p.finishedAgo(p.payroll, 10*day)
	p.sweep()
	p.expect(
		map[string]uuid.UUID{"a payroll run inside the installation's thirty days": payroll},
		map[string]uuid.UUID{"a claim past its process's seven days": claim},
	)
}

// With no installation period, only the processes given one are swept: the
// rest are kept however old, as they were before any period was set.
func TestWithNoInstallationPeriodOnlyProcessesGivenOneAreSwept(t *testing.T) {
	p := newPeriodSweep(t, "")
	ancientPayroll := p.finishedAgo(p.payroll, 3000*day)
	ancientClaim := p.finishedAgo(p.definition, 3000*day)
	p.sweep()
	p.expect(map[string]uuid.UUID{"a payroll run with no period anywhere": ancientPayroll, "a claim with no period anywhere": ancientClaim}, nil)

	p.setPeriod(days(365))
	p.sweep()
	p.expect(
		map[string]uuid.UUID{"a payroll run, whose process has no period while the installation has none": ancientPayroll},
		map[string]uuid.UUID{"a claim past its process's year": ancientClaim},
	)
}

// A period being set when a sweep batch starts is waited for, as a hold is:
// the batch reads the periods once, as it starts, and would otherwise remove
// instances a period made longer while it ran was set to keep.
func TestASweepWaitsForAPeriodBeingSet(t *testing.T) {
	p := newPeriodSweep(t, "30")
	claim := p.finishedAgo(p.definition, 100*day)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	set, commit, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	release := sync.OnceFunc(func() { close(commit) })
	defer release()
	go func() {
		done <- p.repo.UnitOfWork().Do(p.tenant, func(ctx context.Context) error {
			if err := p.period(ctx, days(365)); err != nil {
				return err
			}
			close(set)
			<-commit
			return nil
		})
	}()
	select {
	case <-set:
	case err := <-done:
		t.Fatalf("set the period: %v", err)
	}

	swept := make(chan struct{})
	go func() {
		p.app.sweepRetention(entities.WithSystemContext(ctx), time.Now(), p.keep)
		close(swept)
	}()
	select {
	case <-swept:
		t.Fatal("the sweep ran while a period was being set instead of waiting for it")
	case <-time.After(300 * time.Millisecond):
	}
	release()
	if err := <-done; err != nil {
		t.Fatalf("set the period: %v", err)
	}
	select {
	case <-swept:
	case <-ctx.Done():
		t.Fatal("the sweep did not finish once the period was set")
	}
	if !p.left(claim) {
		t.Fatal("the sweep removed a claim the year its process was given while it waited keeps")
	}
}
