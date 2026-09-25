package sqlconnector

import (
	"context"
	"testing"

	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/tests/testutils"
)

// BenchmarkALookup is what keeping a pool per database is worth. "on a kept
// pool" is a lookup as the engine runs it after the first. "opening a pool each
// time" connects, checks the login cannot see Metis's tables, and runs the same
// lookup — the cost of connecting per lookup, which is what participantsource's
// PostgresSource does for a sync that runs once an hour. Against a local
// server the gap is the handshake alone; across a network it grows with the
// round trip.
func BenchmarkALookup(b *testing.B) {
	target := testutils.PostgresLookupDatabase(b, customersTable...)
	config := postgresConfig(target.DSN)
	req := servicecontracts.ConnectorRequest{
		Statement:      "SELECT tier FROM customers WHERE id = :customer_id",
		Params:         map[string]any{"customer_id": 7.0},
		ResultVariable: "customer",
	}
	ctx := context.Background()

	b.Run("on a kept pool", func(b *testing.B) {
		e := testExecutor()
		if _, err := e.ExecuteRequest(ctx, config, req); err != nil {
			b.Fatal(err)
		}
		for b.Loop() {
			if _, err := e.ExecuteRequest(ctx, config, req); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("opening a pool each time", func(b *testing.B) {
		for b.Loop() {
			e := testExecutor()
			if _, err := e.ExecuteRequest(ctx, config, req); err != nil {
				b.Fatal(err)
			}
			e.pools.cache.Clear()
		}
	})
}
