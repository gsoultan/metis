package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// storageProbe measures what the run left behind on disk: the database's size,
// and the tables that grew the most, before and after.
//
// It reads PostgreSQL's own accounting, so it counts indexes and TOAST along
// with the rows — the size an operator pays for, not the size of the data.
type storageProbe struct {
	conn *pgx.Conn
}

const (
	databaseSizeQuery = `SELECT pg_database_size(current_database())`
	tableSizesQuery   = `SELECT c.relname, pg_total_relation_size(c.oid)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'r' AND n.nspname = 'public'`
)

func openStorageProbe(ctx context.Context, dsn string) (*storageProbe, error) {
	if dsn == "" {
		return nil, nil
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connecting to measure storage: %w", err)
	}
	return &storageProbe{conn: conn}, nil
}

func (p *storageProbe) snapshot(ctx context.Context) (snapshot, error) {
	out := snapshot{tables: make(map[string]int64)}
	if err := p.conn.QueryRow(ctx, databaseSizeQuery).Scan(&out.database); err != nil {
		return out, fmt.Errorf("reading the database size: %w", err)
	}
	rows, err := p.conn.Query(ctx, tableSizesQuery)
	if err != nil {
		return out, fmt.Errorf("reading table sizes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var size int64
		if err := rows.Scan(&name, &size); err != nil {
			return out, err
		}
		out.tables[name] = size
	}
	return out, rows.Err()
}

func (p *storageProbe) close(ctx context.Context) {
	if p != nil {
		_ = p.conn.Close(ctx)
	}
}
