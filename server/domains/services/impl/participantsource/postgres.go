package participantsource

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gsoultan/metis/server/domains/logic/userimport"
	"github.com/gsoultan/metis/server/domains/services/impl/sqlconnector"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
)

// PostgresConfig describes a directory held in another database.
type PostgresConfig struct {
	// DSN is the connection string. It carries a password, so it is encrypted
	// at rest and never returned to a browser.
	DSN string
	// Query must return the columns the other sources use: username, and
	// optionally display_name, email, groups and active.
	//
	// Alias them if the source table calls them something else —
	// `SELECT login AS username` — because the vocabulary is the same across
	// every source deliberately, and translating here would make each source
	// grow its own idea of what a participant is.
	Query string
}

// queryTimeout bounds a directory query.
//
// It runs against somebody else's database on a schedule, so a query that has
// become slow must fail rather than hold a sync open indefinitely — and a
// scheduled sync that never returns is one that never runs again.
const queryTimeout = 30 * time.Second

// applicationName is how a directory read shows in the source database's
// pg_stat_activity, so whoever runs it can tell what is connecting.
const applicationName = "metis-directory"

// PostgresSource reads a directory from a query against another database.
//
// This is the most dangerous of the three, and worth being explicit about: it
// runs operator-supplied SQL against an operator-supplied database. There is no
// way to make that safe by validating the query — anything expressive enough to
// select a directory is expressive enough to do something else — so the
// controls are elsewhere: configuring a source is administrative, the DSN is
// encrypted at rest, and the query is only ever run against the database that
// same configuration names.
//
// What it deliberately does not do is run against Metis's own database. A
// source is a connection to somewhere else; pointing one at ourselves would
// turn a directory sync into arbitrary SQL against our own tables. Fetch
// enforces that, and keeps the lookup connector's other rules: allowed hosts
// only, one statement, read-only.
type PostgresSource struct{ Config PostgresConfig }

func NewPostgresSource(config PostgresConfig) *PostgresSource {
	return &PostgresSource{Config: config}
}

// Fetch runs the query and reads the rows it returned.
func (s *PostgresSource) Fetch(ctx context.Context) (userimport.Result, error) {
	query := strings.TrimSpace(s.Config.Query)
	if query == "" {
		return userimport.Result{}, fmt.Errorf("this source has no query")
	}
	if strings.TrimSpace(s.Config.DSN) == "" {
		return userimport.Result{}, fmt.Errorf("this source names no database")
	}

	// The same rules a lookup's connection keeps, whatever the string asks
	// for: only hosts the operator allows, one statement per query, read-only
	// by default. Configuring a source is administrative, but on a shared
	// installation a project's administrator is not whoever runs it, and
	// without these one project could read, or write, any database the server
	// can reach.
	config, err := sqlconnector.PreparePostgres(s.Config.DSN, applicationName)
	if err != nil {
		return userimport.Result{}, fmt.Errorf("the source database is not allowed: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	// A single connection rather than a pool: a directory sync is one query,
	// occasionally, and a pool would hold connections open against somebody
	// else's database between runs.
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return userimport.Result{}, fmt.Errorf("the source database could not be reached: %w", err)
	}
	defer func() {
		// WithoutCancel: the query's own context may already be done, and a
		// close that inherits a cancelled one leaves the connection to be
		// reaped by the far side instead of closed politely.
		if err := conn.Close(context.WithoutCancel(ctx)); err != nil {
			log.Warn().Err(err).Msg("Could not close the connection to a participant directory")
		}
	}()

	// Not Metis's own database. The note on PostgresSource said so and nothing
	// enforced it: a source pointed at ourselves turned a directory sync into
	// arbitrary SQL against every organization's tables. Asked of the
	// database, not compared by address, because an alias or a pooler defeats
	// a comparison and does not change what the login can read.
	if err := sqlconnector.RefuseOwnPostgres(ctx, conn); err != nil {
		return userimport.Result{}, fmt.Errorf("the source database was refused: %w", err)
	}

	// Begun read-only as well as defaulting to it, and with the time limit set
	// on the server, so a query that runs away is ended there rather than only
	// abandoned here.
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return userimport.Result{}, fmt.Errorf("could not start a read-only transaction: %w", err)
	}
	defer func() {
		// Never committed: a directory read has nothing to keep.
		if err := tx.Rollback(context.WithoutCancel(ctx)); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Warn().Err(err).Msg("Could not end a participant directory read")
		}
	}()
	if _, err := tx.Exec(ctx, sqlconnector.StatementTimeout(queryTimeout.Milliseconds())); err != nil {
		return userimport.Result{}, fmt.Errorf("could not limit the query's time: %w", err)
	}

	rows, err := tx.Query(ctx, query)
	if err != nil {
		return userimport.Result{}, fmt.Errorf("the query was refused: %w", err)
	}
	defer rows.Close()

	columns := make([]string, 0, len(rows.FieldDescriptions()))
	for _, field := range rows.FieldDescriptions() {
		columns = append(columns, strings.ToLower(field.Name))
	}

	var records []userimport.Record
	for line := 1; rows.Next(); line++ {
		if len(records) > userimport.MaxRows {
			return userimport.Result{}, fmt.Errorf(
				"the query returned more than %d rows; narrow it", userimport.MaxRows)
		}
		values, err := rows.Values()
		if err != nil {
			return userimport.Result{}, fmt.Errorf("could not read row %d: %w", line, err)
		}
		records = append(records, userimport.Record{Line: line, Fields: fieldsOf(columns, values)})
	}
	if err := rows.Err(); err != nil {
		// pgx defers a statement's error to here, so a query that failed
		// part-way through arrives when the rows are drained rather than when
		// Query returned.
		return userimport.Result{}, fmt.Errorf("the query failed while reading: %w", err)
	}

	return userimport.FromRecords(records), nil
}

// fieldsOf maps one result row onto the shared vocabulary, ignoring columns
// nothing understands — the same way an unknown CSV column or JSON key is
// ignored, because a real directory table has columns that mean nothing here.
func fieldsOf(columns []string, values []any) map[string]string {
	fields := make(map[string]string, len(columns))
	for i, name := range columns {
		if i >= len(values) || !userimport.Understands(name) {
			continue
		}
		fields[name] = userimport.Stringify(values[i])
	}
	return fields
}
