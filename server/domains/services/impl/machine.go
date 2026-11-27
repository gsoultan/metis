package impl

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

const (
	// MachineHeartbeatEvery is how often a process says it is running.
	MachineHeartbeatEvery = 15 * time.Second
	// machineRunningWithin is how recently a process must have said so to be
	// left alone: a few heartbeats missed is a busy or restarting process,
	// not a gone one.
	machineRunningWithin = 2 * time.Minute
	// machineForgottenAfter is how long a quiet process is kept on the list,
	// so somebody who looks the next morning still sees what stopped.
	machineForgottenAfter = 7 * 24 * time.Hour
)

type machineService struct {
	repo     repocontracts.MachineRepository
	settings MachineSettings
	now      func() time.Time
}

// NewMachineService returns the Servers page's service over repo. settings
// is what a machine added from the page is given; nil, and none is accepted.
func NewMachineService(repo repocontracts.MachineRepository, settings MachineSettings) contracts.MachineService {
	return &machineService{repo: repo, settings: settings, now: time.Now}
}

// machineAdmin is the caller when they administer every organization: which
// machines run the installation is about all of them.
func machineAdmin(ctx context.Context) error {
	caller := signedIn(ctx)
	if caller == nil || caller.Username == "" || !slices.Contains(caller.Roles, entities.RoleAdmin) {
		return apierr.Forbiddenf("only an administrator of every organization can see the machines running this server")
	}
	return nil
}

func (s *machineService) ListMachines(ctx context.Context) ([]entities.Machine, error) {
	if err := machineAdmin(ctx); err != nil {
		return nil, err
	}
	return s.repo.List(ctx)
}

func (s *machineService) ForgetMachine(ctx context.Context, id uuid.UUID) error {
	if err := machineAdmin(ctx); err != nil {
		return err
	}
	err := s.repo.Forget(ctx, id, s.now().Add(-machineRunningWithin))
	if errors.Is(err, apierr.ErrNotFound) {
		return apierr.Invalidf("that machine said it was running in the last two minutes, or is not on the list; only one that stopped can be removed")
	}
	return err
}

func (s *machineService) StartMachineHeartbeat(ctx context.Context, self entities.Machine, changeRole func(role string)) {
	if self.ID == uuid.Nil {
		self.ID = uuid.New()
	}
	if self.StartedAt.IsZero() {
		self.StartedAt = s.now()
	}
	go func() {
		ticker := time.NewTicker(MachineHeartbeatEvery)
		defer ticker.Stop()
		failing := false
		beats := 0
		for {
			self.LastSeenAt = s.now()
			err := s.repo.Heartbeat(ctx, self)
			switch {
			case err != nil && !failing && ctx.Err() == nil:
				failing = true
				log.Warn().Err(err).Msg("Could not record that this machine is running; the Servers page shows it as quiet until this works")
			case err == nil && failing:
				failing = false
				log.Info().Msg("Recording that this machine is running works again")
			}
			// About once an hour, not on every beat: any machine may do it,
			// and doing it twice removes nothing more.
			if beats%240 == 0 {
				if _, err := s.repo.ForgetQuiet(ctx, s.now().Add(-machineForgottenAfter)); err != nil && ctx.Err() == nil {
					log.Warn().Err(err).Msg("Could not remove the machines quiet for a week")
				}
				if _, err := s.repo.ForgetEndedJoins(ctx, s.now().Add(-machineJoinsKeptFor)); err != nil && ctx.Err() == nil {
					log.Warn().Err(err).Msg("Could not remove the machine joins that ended a day ago")
				}
			}
			// Asked once; the process is on its way out after that.
			if changeRole != nil && err == nil {
				if role, asked := s.askedRole(ctx, self.ID, self.Role); asked {
					changeRole(role)
					changeRole = nil
				}
			}
			beats++
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
