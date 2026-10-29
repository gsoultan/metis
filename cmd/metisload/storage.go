package main

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/pgstat"
	"github.com/gsoultan/storm/runtime/pgxdrv"
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

// storageSchema is the schema whose tables are measured.
const storageSchema = "public"

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
	ex := pgxdrv.Conn{C: p.conn}
	database, err := pgstat.DatabaseSize(ctx, ex)
	if err != nil {
		return snapshot{}, fmt.Errorf("reading the database size: %w", err)
	}
	tables, err := pgstat.TableSizes(ctx, ex, storageSchema)
	if err != nil {
		return snapshot{}, fmt.Errorf("reading table sizes: %w", err)
	}
	return snapshot{database: database, tables: tables}, nil
}

func (p *storageProbe) close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	return p.conn.Close(ctx)
}
