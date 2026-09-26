package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// Resealing moves every sealed value onto the current ENCRYPTION_KEY, so the
// key it replaces — installed as ENCRYPTION_KEY_PREVIOUS for reading — can be
// removed. See docs/runbooks.md, "Rotating secrets".
//
// It finds sealed values by what they look like rather than by a list of the
// columns that hold them. A list is kept by hand, and the one column it missed
// would be data made unreadable the moment the previous key is removed. So
// every text, json, jsonb and bytea column of every table is read, in every
// database this process holds: the main one and each environment's.

// resealBatch is how many rows of one column are read at a time.
const resealBatch = 500

// resealCount is what a pass found in one column.
type resealCount struct {
	// Moved is how many values were sealed again under the current key — or,
	// when checking, how many still need it.
	Moved int
	// Current is how many were already under the current key.
	Current int
	// Unreadable is how many no configured key opens.
	Unreadable int
}

func (c *resealCount) add(other resealCount) {
	c.Moved += other.Moved
	c.Current += other.Current
	c.Unreadable += other.Unreadable
}

// candidateColumn is a column that can hold a sealed value, and how.
type candidateColumn struct {
	table, column, dataType string
}

// handleReseal runs the pass over every database and config.yaml, then exits.
// With check set it changes nothing and fails while anything is still sealed
// under a previous key — the condition for removing it.
func (a *App) handleReseal(ctx context.Context, check bool) error {
	if !crypto.IsConfigured() {
		return errors.New("no ENCRYPTION_KEY is configured, so nothing can be sealed under it")
	}
	if a.storm == nil {
		return errors.New("no database is configured yet, so there is nothing to reseal")
	}
	if !crypto.HasPreviousKeys() && !check {
		log.Warn().Msg("ENCRYPTION_KEY_PREVIOUS is not set, so only values already under the current key can be read; nothing will move")
	}

	databases := map[string]*pgxpool.Pool{"main": a.storm.Main()}
	for id, pool := range a.storm.EnvironmentPools() {
		databases["environment "+id.String()] = pool
	}
	names := make([]string, 0, len(databases))
	for name := range databases {
		names = append(names, name)
	}
	sort.Strings(names)

	var total resealCount
	for _, name := range names {
		counts, err := resealDatabase(ctx, databases[name], check)
		if err != nil {
			return fmt.Errorf("reseal the %s database: %w", name, err)
		}
		for column, count := range counts {
			event := log.Info()
			if count.Unreadable > 0 {
				event = log.Error()
			}
			event.Str("database", name).Str("column", column).
				Int("moved", count.Moved).Int("current", count.Current).Int("unreadable", count.Unreadable).
				Bool("check", check).Msg("Reseal")
			total.add(count)
		}
	}
	configCount, err := resealConfigFile(config.DefaultConfigPath, check)
	if err != nil {
		return err
	}
	total.add(configCount)

	log.Info().Int("moved", total.Moved).Int("current", total.Current).Int("unreadable", total.Unreadable).
		Bool("check", check).Msg("Reseal finished")
	// Reported, not failed. Such a value is either sealed under a key this
	// process does not have — unreadable before this ran, and no worse after —
	// or plain text that begins like a sealed value, such as a step somebody
	// named that way. The column says which; neither is made safe or unsafe by
	// removing the previous key.
	if total.Unreadable > 0 {
		log.Error().Int("unreadable", total.Unreadable).Msg("Some values that look sealed open under no configured key. " +
			"The lines above name their columns: a sealed column means data this installation cannot read, " +
			"a column of names or text means a value that only looks sealed.")
	}
	if check && total.Moved > 0 {
		return fmt.Errorf("%d value(s) are still sealed under a previous key; run `metis --reseal` "+
			"before removing ENCRYPTION_KEY_PREVIOUS", total.Moved)
	}
	return nil
}

// resealDatabase runs the pass over one database, keyed "table.column".
func resealDatabase(ctx context.Context, pool *pgxpool.Pool, check bool) (map[string]resealCount, error) {
	columns, err := candidateColumns(ctx, pool)
	if err != nil {
		return nil, err
	}
	counts := map[string]resealCount{}
	for _, column := range columns {
		count, err := resealColumn(ctx, pool, column, check)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", column.table, column.column, err)
		}
		if count != (resealCount{}) {
			counts[column.table+"."+column.column] = count
		}
	}
	return counts, nil
}

// candidateColumns lists every column of the current schema that can hold a
// sealed value.
func candidateColumns(ctx context.Context, pool *pgxpool.Pool) ([]candidateColumn, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.table_name, c.column_name, c.data_type
		  FROM information_schema.columns c
		  JOIN information_schema.tables t
		    ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		 WHERE c.table_schema = current_schema()
		   AND t.table_type = 'BASE TABLE'
		   AND c.data_type IN ('text', 'character varying', 'json', 'jsonb', 'bytea')
		 ORDER BY c.table_name, c.column_name`)
	if err != nil {
		return nil, fmt.Errorf("list the columns: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (candidateColumn, error) {
		var column candidateColumn
		err := row.Scan(&column.table, &column.column, &column.dataType)
		return column, err
	})
}

// resealColumn moves one column's sealed values, a batch at a time, in ctid
// order. Each update is conditional on the value it read, so a row the server
// changed in the meantime is left for the next run rather than overwritten.
func resealColumn(ctx context.Context, pool *pgxpool.Pool, column candidateColumn, check bool) (resealCount, error) {
	table := pgx.Identifier{column.table}.Sanitize()
	name := pgx.Identifier{column.column}.Sanitize()

	// How to find a sealed value in each kind of column, read it as text, and
	// write text back in the column's own form.
	var match, read, write string
	switch column.dataType {
	case "jsonb":
		match = fmt.Sprintf("jsonb_typeof(%[1]s) = 'string' AND %[1]s #>> '{}' LIKE 'gcm1:%%'", name)
		read, write = name+" #>> '{}'", "to_jsonb($2::text)"
	case "json":
		match = fmt.Sprintf("json_typeof(%[1]s) = 'string' AND %[1]s #>> '{}' LIKE 'gcm1:%%'", name)
		read, write = name+" #>> '{}'", "to_json($2::text)"
	case "bytea":
		match = fmt.Sprintf("substring(%s from 1 for 5) = 'gcm1:'::bytea", name)
		read, write = fmt.Sprintf("convert_from(%s, 'UTF8')", name), "convert_to($2::text, 'UTF8')"
	default:
		// Two forms in a text column: the sealed value itself, and the same
		// value JSON-encoded — the repositories seal a map as a JSON string,
		// and some of those columns are text rather than jsonb.
		match = fmt.Sprintf(`(%[1]s LIKE 'gcm1:%%' OR %[1]s LIKE '"gcm1:%%')`, name)
		read, write = name, "$2::text"
	}
	// Identifiers are from the catalogue and quoted by pgx; values are bound.
	selectBatch := fmt.Sprintf( //nolint:gosec // quoted catalogue identifiers, no input
		"SELECT ctid::text, %s FROM %s WHERE %s AND ctid > $1::tid ORDER BY ctid LIMIT %d",
		read, table, match, resealBatch)
	update := fmt.Sprintf( //nolint:gosec // quoted catalogue identifiers, no input
		"UPDATE %s SET %s = %s WHERE ctid = $1::tid AND %s = $3", table, name, write, read)

	var count resealCount
	after := "(0,0)"
	for {
		rows, err := pool.Query(ctx, selectBatch, after)
		if err != nil {
			return count, err
		}
		type sealedRow struct{ ctid, value string }
		batch, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (sealedRow, error) {
			var r sealedRow
			err := row.Scan(&r.ctid, &r.value)
			return r, err
		})
		if err != nil {
			return count, err
		}
		for _, row := range batch {
			resealed, moved, err := resealStored(row.value)
			switch {
			case errors.Is(err, crypto.ErrUnreadable):
				count.Unreadable++
			case err != nil:
				return count, err
			case !moved:
				count.Current++
			case check:
				count.Moved++
			default:
				tag, err := pool.Exec(ctx, update, row.ctid, resealed, row.value)
				if err != nil {
					return count, err
				}
				count.Moved += int(tag.RowsAffected())
			}
		}
		if len(batch) < resealBatch {
			return count, nil
		}
		after = batch[len(batch)-1].ctid
	}
}

// resealConfigFile moves config.yaml's sealed connection string onto the
// current key. The file is the one place outside a database a sealed value is
// kept, and the server cannot reach its database without reading it.
func resealConfigFile(path string, check bool) (resealCount, error) {
	var count resealCount
	if !config.Exists(path) {
		return count, nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return count, fmt.Errorf("read %s: %w", path, err)
	}
	if cfg.Database.EncryptedConnection == "" {
		return count, nil
	}
	// Stored without the prefix Encrypt adds; Reseal works on the prefixed form.
	const prefix = "gcm1:"
	resealed, moved, err := crypto.Reseal(prefix + cfg.Database.EncryptedConnection)
	switch {
	case errors.Is(err, crypto.ErrUnreadable):
		// Unlike a column, this can only be sealed data, and the server could
		// not have started without reading it.
		return count, fmt.Errorf("the connection string in %s opens under no configured key", path)
	case err != nil:
		return count, err
	case !moved:
		count.Current++
		return count, nil
	case check:
		count.Moved++
		return count, nil
	}
	cfg.Database.EncryptedConnection = resealed[len(prefix):]
	if err := cfg.Save(path); err != nil {
		return count, fmt.Errorf("write %s: %w", path, err)
	}
	count.Moved++
	log.Info().Str("file", path).Msg("Resealed the connection string in config.yaml")
	return count, nil
}

// resealStored reseals a value in either form a text column holds it: the
// sealed value, or the sealed value as a JSON string. It answers in the form
// it was given.
func resealStored(stored string) (string, bool, error) {
	if !strings.HasPrefix(stored, `"`) {
		return crypto.Reseal(stored)
	}
	var sealed string
	if err := json.Unmarshal([]byte(stored), &sealed); err != nil {
		// Begins like a JSON-encoded sealed value and is not one.
		return "", false, crypto.ErrUnreadable
	}
	resealed, moved, err := crypto.Reseal(sealed)
	if err != nil || !moved {
		return "", moved, err
	}
	encoded, err := json.Marshal(resealed)
	if err != nil {
		return "", false, err
	}
	return string(encoded), true, nil
}
