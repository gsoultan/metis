package app

import (
	"fmt"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/envvar"
)

// envRole names what this process does of the engine's work. Read once, at
// start.
const envRole = "METIS_ROLE"

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

// roleFromEnvironment reads METIS_ROLE. An unknown value stops the engine:
// a typo would otherwise be a machine that silently does, or does not do,
// the installation's work.
func roleFromEnvironment() (Role, error) {
	return ParseRole(envvar.Get(envRole))
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
