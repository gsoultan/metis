package sqlconnector

import (
	"testing"
	"time"
)

func TestColumnValuesBecomeWhatAVariableHolds(t *testing.T) {
	when := time.Date(2026, 9, 24, 10, 30, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		databaseType string
		raw          any
		want         any
	}{
		"null":                           {"TEXT", nil, nil},
		"text":                           {"TEXT", "Acme", "Acme"},
		"MySQL text arrives as bytes":    {"VARCHAR", []byte("Acme"), "Acme"},
		"an integer":                     {"INT8", int64(42), 42.0},
		"an integer a float would move":  {"INT8", int64(9007199254740993), "9007199254740993"},
		"a float":                        {"FLOAT8", 1.5, 1.5},
		"a decimal":                      {"NUMERIC", []byte("5000.00"), 5000.0},
		"pgx hands NUMERIC over as text": {"NUMERIC", "5000.00", 5000.0},
		"JSON handed over as text":       {"JSONB", `{"tier":"gold"}`, map[string]any{"tier": "gold"}},
		"a decimal too long for float":   {"NUMERIC", []byte("1234567890123456.78"), "1234567890123456.78"},
		"a boolean":                      {"BOOL", true, true},
		"a date":                         {"DATE", when, "2026-09-24"},
		"a timestamp":                    {"TIMESTAMPTZ", when, "2026-09-24T10:30:00Z"},
		"JSON is parsed":                 {"JSONB", []byte(`{"tier":"gold"}`), map[string]any{"tier": "gold"}},
		"bytes are base64":               {"BYTEA", []byte{0xde, 0xad}, "3q0="},
		// SQL Server keeps the first three groups byte-swapped.
		"a SQL Server uniqueidentifier": {"UNIQUEIDENTIFIER",
			[]byte{0x67, 0x45, 0x23, 0x01, 0xab, 0x89, 0xef, 0xcd, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef},
			"01234567-89AB-CDEF-0123-456789ABCDEF"},
	} {
		t.Run(name, func(t *testing.T) {
			got := jsonValue(tc.databaseType, tc.raw)
			if m, ok := tc.want.(map[string]any); ok {
				gotMap, _ := got.(map[string]any)
				if gotMap["tier"] != m["tier"] {
					t.Fatalf("got %#v, want %#v", got, tc.want)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestSignificantDigits(t *testing.T) {
	for text, want := range map[string]int{
		"5000.00": 4, "0.00045": 2, "-12.50": 3, "1234567890123456.78": 18, "0": 0,
	} {
		if got := significantDigits(text); got != want {
			t.Errorf("significantDigits(%q) = %d, want %d", text, got, want)
		}
	}
}
