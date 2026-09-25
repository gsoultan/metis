package sqlconnector

import (
	"strconv"
	"strings"
	"time"

	"github.com/gsoultan/metis/internal/pkg/envvar"
)

// The knobs, and their defaults.
const (
	// maxConnsEnv caps the connections one Metis node holds to one database.
	// Across a cluster the total is this times the nodes, which is the number
	// to agree with whoever runs that database.
	maxConnsEnv     = "METIS_SQL_LOOKUP_MAX_CONNS"
	defaultMaxConns = 4

	// maxPoolsEnv caps how many databases one node keeps a pool open to.
	maxPoolsEnv     = "METIS_SQL_LOOKUP_MAX_POOLS"
	defaultMaxPools = 32

	// An idle pool gives its connections back, so a node that has stopped
	// looking something up stops holding connections on somebody else's
	// server. A connection is replaced after a while regardless, since
	// something between here and there can quietly break one.
	poolIdleTime = 2 * time.Minute
	poolLifetime = 30 * time.Minute
)

// poolSettings are the pool knobs, read once.
type poolSettings struct {
	maxConns int
	maxPools int
}

func resolvePoolSettings() poolSettings {
	return poolSettings{
		maxConns: positiveEnv(maxConnsEnv, defaultMaxConns),
		maxPools: positiveEnv(maxPoolsEnv, defaultMaxPools),
	}
}

func positiveEnv(name string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(envvar.Get(name)))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
