package selfupgrade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// recover is the way back after the service was stopped. With the schema
// where it was, the previous release is reinstalled and started. With the
// schema moved, the previous release cannot read it: metis is left stopped
// and the backup is the way back, which is a person's decision, never this
// program's.
func (r *run) recover(ctx context.Context, previous verified, schemaBefore int64, cause error) (Status, error) {
	p := r.plan
	r.step("failed", cause.Error())
	_ = p.Service.Stop(ctx)

	schemaAfter, err := p.Database.SchemaVersion(ctx)
	if err != nil || schemaAfter != schemaBefore {
		detail := fmt.Sprintf("%s did not come up after the database moved from schema %d", p.To, schemaBefore)
		if err != nil {
			detail = fmt.Sprintf("%s did not come up, and the database could not be read to tell whether it migrated", p.To)
		}
		return r.finish(OutcomeNeedsRestore, "stopped",
			fmt.Errorf("%s. Metis is stopped. Restore %s, then reinstall %s: %w", detail, r.status.Backup, r.status.From, cause))
	}

	if err := p.Installer.Install(ctx, previous.pkgPath); err != nil {
		return r.finish(OutcomeNeedsRestore, "stopped", errors.Join(cause, fmt.Errorf("reinstall %s: %w", r.status.From, err)))
	}
	if err := p.Service.Start(ctx); err != nil {
		return r.finish(OutcomeNeedsRestore, "stopped", errors.Join(cause, fmt.Errorf("start %s again: %w", r.status.From, err)))
	}
	if err := r.waitFor(ctx, r.status.From); err != nil {
		return r.finish(OutcomeNeedsRestore, "stopped", errors.Join(cause, err))
	}
	return r.finish(OutcomeRolledBack, "rolled back", fmt.Errorf("%s did not come up and nothing had migrated, so %s is serving again: %w", p.To, r.status.From, cause))
}

// waitFor polls until the server answers ready with the version wanted.
func (r *run) waitFor(ctx context.Context, version string) error {
	p := r.plan
	deadline := time.Now().Add(p.StartTimeout)
	every := p.PollEvery
	if every <= 0 {
		every = 2 * time.Second
	}
	for {
		if got, err := p.Server.Version(ctx); err == nil && got == version && p.Server.Ready(ctx) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not answer ready within %s", version, p.StartTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(every):
		}
	}
}

func (r *run) step(name, detail string) {
	at := r.plan.now()
	r.status.Step = name
	r.status.Steps = append(r.status.Steps, StepRecord{Name: name, Detail: detail, At: at})
	r.save()
}

func (r *run) refuse(step string, err error) (Status, error) {
	// The step leads the message, so whoever reads it knows how far it got.
	return r.finish(OutcomeRefused, step, fmt.Errorf("%s: %w", step, err))
}

func (r *run) finish(outcome Outcome, step string, err error) (Status, error) {
	r.status.Outcome = outcome
	r.status.Step = step
	if err != nil {
		r.status.Detail = err.Error()
	}
	r.save()
	return r.status, err
}

func (r *run) save() {
	r.status.UpdatedAt = r.plan.now()
	// A status that cannot be written does not stop the upgrade: the steps
	// are what matter, and they are in the journal too.
	path := filepath.Join(r.plan.Dir, "status.json")
	if writeStatus(path, r.status) == nil && r.plan.StatusGID > 0 {
		_ = os.Chown(path, -1, r.plan.StatusGID)
	}
}

// lock takes an exclusive lock on path, so two upgrades never overlap.
func lock(path string) (func(), error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, errors.New("another upgrade is already running")
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
