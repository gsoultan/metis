package impl

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// ChangeMachineRole asks running process id to start again in role. The
// process reads the ask on its next heartbeat, finishes what it is doing,
// and starts again.
//
// Refused for a process whose operator fixed its role, and when no running
// process would be left running the work: the page cannot turn every worker
// off.
func (s *machineService) ChangeMachineRole(ctx context.Context, id uuid.UUID, role string) error {
	if err := machineAdmin(ctx); err != nil {
		return err
	}
	role = strings.TrimSpace(role)
	if !validMachineRole(role) {
		return apierr.Invalidf("a machine runs all, web or worker")
	}
	err := s.repo.RequestRole(ctx, id, role, s.now().Add(-machineRunningWithin))
	switch {
	case errors.Is(err, apierr.ErrNotFound):
		return apierr.Invalidf("that machine has not said it is running in the last two minutes; only a running machine can change what it runs")
	case errors.Is(err, repocontracts.ErrMachineRoleFixed):
		return apierr.Invalidf("that machine's operator set what it runs, with METIS_ROLE or with no config.yaml on it; change it there")
	case errors.Is(err, repocontracts.ErrNoMachineLeftWorking):
		return apierr.Invalidf("no running machine would be left running background work, so jobs, timers and deliveries would wait; switch another machine to worker first")
	case err != nil:
		return err
	}
	log.Info().Str("by", signedIn(ctx).Username).Str("machine", id.String()).Str("role", role).
		Msg("A machine was asked to change what it runs; it starts again in that role")
	return nil
}

// askedRole is the role this process was asked to take, when it differs from
// the one it has.
func (s *machineService) askedRole(ctx context.Context, id uuid.UUID, has string) (string, bool) {
	asked, err := s.repo.RequestedRole(ctx, id)
	if err != nil {
		if ctx.Err() == nil {
			log.Warn().Err(err).Msg("Could not read whether this machine was asked to change what it runs")
		}
		return "", false
	}
	if asked == "" || asked == has || !validMachineRole(asked) {
		return "", false
	}
	return asked, true
}
