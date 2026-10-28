package app

import (
	"testing"

	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// Migrating at boot goes through the pool that serves every request, and a
// database already at the baseline is left as it is.
//
// There used to be a second pool here, GORM's, opened to migrate and closed
// after. There is one now, so the thing to prove is that migrating does not
// take it away.
func TestMigratingAtBootKeepsTheServingPool(t *testing.T) {
	_, conn := testutils.SetupTestStore(t)
	a := &App{storm: conn, repo: repositories.NewRepository(conn)}

	if err := a.migrate(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := conn.Main().Ping(t.Context()); err != nil {
		t.Errorf("the storm pool must stay open: %v", err)
	}
}
