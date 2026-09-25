package sqlconnector

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// unnamedColumn is what PostgreSQL calls a column nobody named.
const unnamedColumn = "?column?"

// column is one result column: the key it gets, and the type that says how to
// read its values.
type column struct {
	name         string
	databaseType string
}

// columnsOf names a result's columns, refusing a set that cannot become one
// map per row: a column with no name, or two with the same name, where the
// second would silently replace the first.
func columnsOf(rows *sql.Rows) ([]column, error) {
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	columns := make([]column, len(types))
	seen := make(map[string]bool, len(types))
	for i, ct := range types {
		name := ct.Name()
		if strings.TrimSpace(name) == "" || name == unnamedColumn {
			return nil, fmt.Errorf("column %d of the lookup's result has no name; give it one with AS", i+1)
		}
		if seen[name] {
			return nil, fmt.Errorf("two columns of the lookup's result are both called %q; name them apart with AS", name)
		}
		seen[name] = true
		columns[i] = column{name: name, databaseType: ct.DatabaseTypeName()}
	}
	return columns, nil
}

// rowsRead is what a lookup brought back, and whether it stopped at a limit.
type rowsRead struct {
	rows      []map[string]any
	bytes     int
	truncated bool
}

// readRows reads until the result ends or a limit is reached. A row that would
// take the result past the byte limit is not included: the variables are
// stored on the instance, so an unbounded result is load on Metis's own
// database, not only on the one being read.
func readRows(rows *sql.Rows, limits resultLimits) (rowsRead, error) {
	columns, err := columnsOf(rows)
	if err != nil {
		return rowsRead{}, err
	}
	raw := make([]any, len(columns))
	targets := make([]any, len(columns))
	for i := range raw {
		targets[i] = &raw[i]
	}
	var read rowsRead
	for rows.Next() {
		if len(read.rows) == limits.rows {
			read.truncated = true
			return read, nil
		}
		if err := rows.Scan(targets...); err != nil {
			return rowsRead{}, err
		}
		row, size, err := rowOf(columns, raw)
		if err != nil {
			return rowsRead{}, err
		}
		if read.bytes+size > limits.bytes {
			read.truncated = true
			return read, nil
		}
		read.rows = append(read.rows, row)
		read.bytes += size
	}
	return read, rows.Err()
}

func rowOf(columns []column, raw []any) (map[string]any, int, error) {
	row := make(map[string]any, len(columns))
	for i, c := range columns {
		row[c.name] = jsonValue(c.databaseType, raw[i])
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		return nil, 0, fmt.Errorf("a row of the lookup's result could not be stored: %w", err)
	}
	return row, len(encoded), nil
}
