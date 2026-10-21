package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// SimulationCasesMigration is the version under which a process could keep
// its simulation test cases. 39 is the business key's.
const SimulationCasesMigration = 43

// simulationCasesDDL is frozen text: the table migration 43 made, which must
// not change when the model does. tests/migrations compares it with the storm
// model and the model is the arbiter.
//
// A case names a process by its key, as a retention period does, so it runs
// against every version including ones not yet deployed. The reference to
// projects is plain, as the periods' is. The index is a plain build: the
// table has just been created and is read by nothing yet.
var simulationCasesDDL = []string{
	`CREATE TABLE IF NOT EXISTS simulation_cases (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    project_id uuid NOT NULL,
    process_key varchar(255) NOT NULL,
    name varchar(255) NOT NULL,
    scenario jsonb NOT NULL,
    expect jsonb NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT fk_simulation_cases_project_id FOREIGN KEY (project_id) REFERENCES projects (id)
)`,
	`CREATE INDEX IF NOT EXISTS ix_simulation_cases_process ON simulation_cases (project_id, process_key, created_at, id)`,
}

// simulationCases is migration 43.
//
// The cases somebody built in the designer's simulation lived in the browser
// tab and were gone on reload, so the check that a process still does what
// it did had to be clicked through again after every change. A case, with
// what it must end with, is now a row of its process.
//
// One transaction, waited for no longer than deviationRequestsLockWait, for
// the reason migration 37 gives: the foreign key takes a brief lock on
// projects. PostgreSQL only, as 21 to 39.
func simulationCases() Migration {
	return Migration{
		Version: SimulationCasesMigration,
		Name:    "a process can keep its simulation test cases",
		Run:     addSimulationCases,
	}
}

func addSimulationCases(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" {
		return nil
	}
	err := inBoundedTransaction(ctx, db, simulationCasesDDL...)
	if lockNotAvailable(err) {
		return fmt.Errorf("projects was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every writer of it behind it, and will finish when started again once that ends: %w",
			deviationRequestsLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("create the simulation cases: %w", err)
	}
	return nil
}
