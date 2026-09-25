package sqlconnector

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEachServerGetsItsOwnPlaceholders(t *testing.T) {
	statement := "SELECT * FROM t WHERE a = :id OR b = :id OR c = :name;"
	params := map[string]any{"id": 7.0, "name": "Acme"}

	for _, tc := range []struct {
		dialect   dialect
		wantQuery string
		wantArgs  []any
	}{
		{postgresDialect{}, "SELECT * FROM t WHERE a = $1 OR b = $1 OR c = $2", []any{int64(7), "Acme"}},
		// ? cannot be reused, so a name used twice takes its value twice.
		{mysqlDialect{}, "SELECT * FROM t WHERE a = ? OR b = ? OR c = ?", []any{int64(7), int64(7), "Acme"}},
		{sqlServerDialect{}, "SELECT * FROM t WHERE a = @metis_p1 OR b = @metis_p1 OR c = @metis_p2",
			[]any{sql.Named("metis_p1", int64(7)), sql.Named("metis_p2", "Acme")}},
	} {
		query, args, err := bind(statement, params, tc.dialect)
		if err != nil {
			t.Fatalf("%s: %v", tc.dialect.label(), err)
		}
		if query != tc.wantQuery {
			t.Errorf("%s query = %q, want %q", tc.dialect.label(), query, tc.wantQuery)
		}
		if !reflect.DeepEqual(args, tc.wantArgs) {
			t.Errorf("%s args = %#v, want %#v", tc.dialect.label(), args, tc.wantArgs)
		}
	}
}

// A value is only ever a parameter. One that looks like SQL is still a value.
func TestAValueIsNeverWrittenIntoTheQuery(t *testing.T) {
	hostile := "x' OR '1'='1'; DROP TABLE customers; --"
	query, args, err := bind("SELECT * FROM customers WHERE name = :name", map[string]any{"name": hostile}, postgresDialect{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(query, "DROP") || strings.Contains(query, "OR '1'") {
		t.Fatalf("the value reached the query text: %q", query)
	}
	if len(args) != 1 || args[0] != hostile {
		t.Fatalf("the value was not passed as a parameter: %#v", args)
	}
}

func TestOnlyRealParametersAreReplaced(t *testing.T) {
	query, args, err := bind("SELECT ':name', created_at::date FROM t WHERE id = :id", map[string]any{"id": "7"}, postgresDialect{})
	if err != nil {
		t.Fatal(err)
	}
	if query != "SELECT ':name', created_at::date FROM t WHERE id = $1" || len(args) != 1 {
		t.Fatalf("query = %q, args = %v", query, args)
	}
}

func TestParameterValues(t *testing.T) {
	when := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		value   any
		want    any
		refused bool
	}{
		"text":                         {value: "C-7", want: "C-7"},
		"a whole number is an integer": {value: 42.0, want: int64(42)},
		"a fraction stays a fraction":  {value: 4.5, want: 4.5},
		"true or false":                {value: true, want: true},
		"a date":                       {value: when, want: when},
		"no value":                     {value: nil, refused: true},
		"a list":                       {value: []any{"a", "b"}, refused: true},
		"a set of named values":        {value: map[string]any{"a": 1.0}, refused: true},
		"a duration":                   {value: time.Minute, refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			_, args, err := bind("SELECT :v", map[string]any{"v": tc.value}, postgresDialect{})
			if tc.refused {
				if err == nil {
					t.Fatalf("%#v was bound", tc.value)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(args[0], tc.want) {
				t.Fatalf("bound %#v, want %#v", args[0], tc.want)
			}
		})
	}
}

func TestAParameterTheStepDoesNotSupplyIsRefusedNotNull(t *testing.T) {
	_, _, err := bind("SELECT * FROM t WHERE id = :customer_id", map[string]any{"customerId": "7"}, postgresDialect{})
	if err == nil || !strings.Contains(err.Error(), ":customer_id") {
		t.Fatalf("a missing parameter was not named in the refusal: %v", err)
	}
}
