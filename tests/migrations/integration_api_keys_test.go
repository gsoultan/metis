package migrations_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm"
)

// A database made before integration API keys is given the table by step 58, in
// the shape the model declares.
func TestTheIntegrationKeyStepBringsAnOlderDatabaseToTheModel(t *testing.T) {
	pool := testutils.EmptySchema(t)
	ctx := t.Context()
	if _, err := migrations.Run(ctx, pool, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE integration_api_keys`); err != nil {
		t.Fatalf("make the database one from before integration API keys: %v", err)
	}
	result, err := migrations.Run(ctx, pool, migrations.Steps())
	if err != nil {
		t.Fatalf("run the shipped steps: %v", err)
	}
	if !slices.Contains(result.Applied, 58) {
		t.Fatalf("result %+v; want step 58 applied", result)
	}
	want, err := storm.Build(model.All()...)
	if err != nil {
		t.Fatalf("build the model: %v", err)
	}
	if drift := driftFrom(t, pool, want); len(drift) != 0 {
		t.Errorf("after step 58 the database differs from the model:\n%s", strings.Join(drift, "\n"))
	}
}
