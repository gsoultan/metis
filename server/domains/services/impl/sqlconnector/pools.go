package sqlconnector

import (
	"context"
	"database/sql"
	"errors"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"

	"github.com/gsoultan/metis/internal/pkg/lru"
)

// pools holds one connection pool per database a lookup reads.
//
// A lookup runs on the engine's path, once per instance per step, at the job
// pool's concurrency. Connecting for each one would spend a connection
// handshake of every lookup's budget and open a new session on somebody else's
// server for every step that runs. So pools are kept, keyed by a hash of the
// connection, bounded in number, and each closed as it is dropped.
//
// This is the opposite of participantsource's PostgresSource, which connects
// for each sync. That is one query an hour; this is on the hot path. The idle
// time in pool_settings.go gives back what PostgresSource's choice protects: a
// node that is not looking anything up holds no connections there.
type pools struct {
	cache    *lru.Cache[string, *sql.DB]
	opening  singleflight.Group
	settings poolSettings
	hosts    hostPolicy
}

func newPools(settings poolSettings, hosts hostPolicy) *pools {
	return &pools{
		cache:    lru.NewWithEviction(settings.maxPools, closeDropped),
		settings: settings,
		hosts:    hosts,
	}
}

var errUnexpectedPool = errors.New("a lookup's connection pool could not be opened")

// get returns the pool for a connection, opening it at most once however many
// lookups ask for it at the same moment.
func (p *pools) get(ctx context.Context, conn connection) (*sql.DB, error) {
	key := conn.poolKey()
	if db, held := p.cache.Get(key); held {
		return db, nil
	}
	opened, err, _ := p.opening.Do(key, func() (any, error) {
		if db, held := p.cache.Get(key); held {
			return db, nil
		}
		db, err := p.open(ctx, conn)
		if err != nil {
			return nil, err
		}
		p.cache.Put(key, db)
		return db, nil
	})
	if err != nil {
		return nil, err
	}
	db, ok := opened.(*sql.DB)
	if !ok {
		return nil, errUnexpectedPool
	}
	return db, nil
}

func (p *pools) open(ctx context.Context, conn connection) (*sql.DB, error) {
	db, err := conn.dialect.open(conn.dsn, p.hosts)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(p.settings.maxConns)
	db.SetMaxIdleConns(p.settings.maxConns)
	db.SetConnMaxIdleTime(poolIdleTime)
	db.SetConnMaxLifetime(poolLifetime)
	if err := refuseOwnDatabase(ctx, db); err != nil {
		closeDropped("", db)
		return nil, err
	}
	return db, nil
}

// closeDropped closes a pool in the background: Close waits for the queries
// already running on it, and the caller dropping it should not.
func closeDropped(_ string, db *sql.DB) {
	go func() {
		if err := db.Close(); err != nil {
			log.Debug().Err(err).Msg("A dropped lookup pool did not close cleanly")
		}
	}()
}
