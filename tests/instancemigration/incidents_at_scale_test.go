package instancemigration

import (
	"testing"

	"github.com/google/uuid"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// An instance's incidents are listed in full, and a migration that holds an
// instance again finds the hold it raised before, however many incidents came
// after it.
//
// Both read the instance's incidents newest first through a query the store
// caps at a thousand rows. Behind a thousand newer incidents — a service call
// failing every few minutes for a few days raises that many — the incident
// list stopped short, and the migration, not finding its earlier hold, raised
// the same one again on every run.
func TestAnInstancesIncidentsAreSeenPastTheFirstThousand(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)
	hold := []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionHold, Reason: "ask the account manager"},
		}),
		servicecontracts.WithActor("dita"),
	}
	if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, hold...); err != nil {
		t.Fatalf("hold: %v", err)
	}
	instance := f.onlyInstance(t)

	if err := f.db.WithContext(f.ctx).Exec(`
		INSERT INTO incidents (id, created_at, updated_at, instance_id, node_id, error, status, resolved_at)
		SELECT gen_random_uuid(), now() + interval '1 minute', now() + interval '1 minute', ?,
		       'callPartner', 'the partner timed out', ?, now() + interval '2 minutes'
		  FROM generate_series(1, ?) AS n`, instance.ID, string(models.IncidentResolved), storeReadCap).Error; err != nil {
		t.Fatalf("seed %d newer incidents: %v", storeReadCap, err)
	}

	incidents, err := f.svc.ListIncidents(f.ctx, instance.ID)
	if err != nil {
		t.Fatalf("list incidents: %v", err)
	}
	if len(incidents) != storeReadCap+1 {
		t.Errorf("the instance lists %d of its %d incidents", len(incidents), storeReadCap+1)
	}

	if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, hold...); err != nil {
		t.Fatalf("hold again: %v", err)
	}
	if holds := f.openIncidentsOn(t, instance.ID, "opsApprove"); holds != 1 {
		t.Fatalf("holding the instance a second time left %d open incidents on the held step; the first hold is still open", holds)
	}
}

// openIncidentsOn counts in the database, not through the code under test.
func (f *fixture) openIncidentsOn(t *testing.T, instanceID uuid.UUID, nodeID string) int {
	t.Helper()
	var n int
	if err := f.db.WithContext(f.ctx).Raw(
		`SELECT count(*) FROM incidents WHERE instance_id = ? AND node_id = ? AND status = ? AND deleted_at IS NULL`,
		instanceID, nodeID, string(models.IncidentOpen)).Scan(&n).Error; err != nil {
		t.Fatalf("count the open incidents: %v", err)
	}
	return n
}
