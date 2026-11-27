package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/internal/pkg/envvar"
)

// ErrRestart is a run that ended so this process starts again: an
// administrator changed what it runs on the Servers page, and config.yaml
// now says the new role. The program that ran the engine starts itself
// again, with Restart, once its own work is done.
var ErrRestart = errors.New("the engine stopped so this process starts again in the role the Servers page asked for")

// RunContext runs the engine with args (the command line without the program
// name) until ctx is done, so a program that embeds the engine decides both
// what it is told and when it stops. It returns ErrRestart when it stopped
// itself to take another role.
func (a *App) RunContext(ctx context.Context, args []string) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	a.stopToChangeRole = stop
	err := a.runContext(ctx, args)
	if err == nil && a.changedRole.Load() {
		return ErrRestart
	}
	return err
}

// roleFixed is whether this process's operator set its role where the
// Servers page cannot change it: METIS_ROLE in the environment, which wins
// over config.yaml, or no config.yaml to keep a new role in.
func roleFixed() bool {
	if _, set := envvar.FromEnvironment(envRole); set {
		return true
	}
	return !config.Exists(config.DefaultConfigPath)
}

// changeRole is the heartbeat finding an administrator's ask: it keeps the
// role in config.yaml and ends the run, which drains this process's work
// the way a stop does. A role it cannot keep is not taken: the process
// carries on as it was, and says why.
func (a *App) changeRole(role string) {
	if _, err := ParseRole(role); err != nil || roleFixed() {
		log.Warn().Str("role", role).Msg("This machine was asked to change what it runs, but its operator set its role; it carries on as it is")
		return
	}
	if err := keepRole(config.DefaultConfigPath, role); err != nil {
		log.Error().Err(err).Str("role", role).Msg("This machine was asked to change what it runs, but could not keep it in config.yaml; it carries on as it is")
		return
	}
	log.Info().Str("from", string(a.role)).Str("to", role).
		Msg("An administrator changed what this machine runs; it finishes its work and starts again")
	a.changedRole.Store(true)
	if a.stopToChangeRole != nil {
		a.stopToChangeRole()
	}
}

// keepRole writes role into config.yaml's server section. The file holds
// the database's password and the keys, so it is replaced whole, never left
// half written: a new file beside it, readable by this user alone, then a
// rename over it.
func keepRole(path, role string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	cfg.Server.Role = role
	data, err := cfg.Marshal()
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	name := temp.Name()
	if err := temp.Chmod(0o600); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", path, err), temp.Close(), os.Remove(name))
	}
	if _, err := temp.Write(data); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", path, err), temp.Close(), os.Remove(name))
	}
	if err := temp.Sync(); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", path, err), temp.Close(), os.Remove(name))
	}
	if err := temp.Close(); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", path, err), os.Remove(name))
	}
	if err := os.Rename(name, path); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", path, err), os.Remove(name))
	}
	return nil
}
