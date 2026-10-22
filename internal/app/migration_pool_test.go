package app

import (
	"testing"

	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// GORM connects only to migrate.
//
// Its pool stayed open for the life of the process after the migrations, read
// by nothing but the readiness ping and a metrics collector, and held a second
// set of server sessions beside the storm pool that serves every request.
func TestTheMigrationPoolIsClosedOnceTheMigrationsFinish(t *testing.T) {
	gormDB, conn := testutils.SetupTestStore(t)
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("reach the GORM pool: %v", err)
	}
	a := &App{db: gormDB, storm: conn, repo: repositories.NewRepository(conn)}

	if err := a.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if a.db != nil {
		t.Error("the app still holds the GORM handle after migrating")
	}
	if err := sqlDB.PingContext(t.Context()); err == nil {
		t.Error("the GORM pool still answers after the migrations; it should have been closed")
	}
	if err := conn.Main().Ping(t.Context()); err != nil {
		t.Errorf("the storm pool must stay open: %v", err)
	}
}
