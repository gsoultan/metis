package app

import (
	"fmt"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/internal/pkg/envvar"
)

// envRole names what this process does of the engine's work. Read once, at
// start, from the environment or else config.yaml's server.role.
const envRole = config.EnvRole

// Role is what one process of an installation does. Every process serves the
// API on the same database; the role says whether it also runs the work that
// acts on its own: job workers, timers, outgoing webhooks, batch operations,
// directory syncs, retention sweeps, upgrade checks and the RabbitMQ bridges.
type Role string

const (
	// RoleAll serves the API and runs the work. The default, and what a single
	// machine runs.
	RoleAll Role = "all"
	// RoleWeb serves the API and runs none of the work, so the machines that
	// answer people are not the ones busy with jobs. Something with RoleAll
	// or RoleWorker has to run beside it, or nothing started is ever worked.
	RoleWeb Role = "web"
	// RoleWorker runs the work. It still serves the API, for its health checks
	// and for the program that embeds it; that program decides whether anyone
	// else reaches it.
	RoleWorker Role = "worker"
)

// RunsWork reports whether a process in this role claims and runs work.
func (r Role) RunsWork() bool { return r != RoleWeb }

// roleFromEnvironment reads METIS_ROLE, after useConfigFile has put
// config.yaml's server section under the environment. An unknown value stops
// the engine: a typo would otherwise be a machine that silently does, or does
// not do, the installation's work.
//
// The operator's alone: never a value saved on the Settings page, which an
// administrator could use to turn every worker off from a browser.
func roleFromEnvironment() (Role, error) {
	value, _ := envvar.FromOperator(envRole)
	return ParseRole(value)
}

// ConfiguredRole is the role a process started here would take: METIS_ROLE
// from the environment, or else server.role in config.yaml. It changes
// nothing, so a program embedding the engine can decide what to serve before
// the engine starts.
func ConfiguredRole() (Role, error) {
	if value, set := envvar.FromEnvironment(envRole); set {
		return ParseRole(value)
	}
	if !config.Exists(config.DefaultConfigPath) {
		return RoleAll, nil
	}
	cfg, err := config.Load(config.DefaultConfigPath)
	if err != nil {
		return "", fmt.Errorf("read %s for the role: %w", config.DefaultConfigPath, err)
	}
	return ParseRole(cfg.Server.Role)
}

// ParseRole reads a role as METIS_ROLE spells it. Empty is RoleAll.
func ParseRole(value string) (Role, error) {
	switch role := Role(strings.ToLower(strings.TrimSpace(value))); role {
	case "", RoleAll:
		return RoleAll, nil
	case RoleWeb, RoleWorker:
		return role, nil
	default:
		return "", fmt.Errorf("%s=%q is not a role: use all, web or worker", envRole, value)
	}
}
