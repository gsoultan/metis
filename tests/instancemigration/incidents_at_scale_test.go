package instancemigration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/incident"
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

	later := time.Now().Add(time.Minute)
	newer := make([]incident.Row, storeReadCap)
	for i := range newer {
		newer[i] = incident.Row{
			ID: uuid.New(), CreatedAt: later, UpdatedAt: later, InstanceID: instance.ID,
			NodeID: "callPartner", Error: "the partner timed out", Status: string(models.IncidentResolved),
			ResolvedAt: some(later.Add(time.Minute)),
		}
	}
	if _, err := incident.InsertAll(f.ctx, f.ex(), newer); err != nil {
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
	n, err := incident.New().InstanceIDEq(instanceID).NodeIDEq(nodeID).StatusEq(string(models.IncidentOpen)).Count(f.ctx, f.ex())
	if err != nil {
		t.Fatalf("count the open incidents: %v", err)
	}
	return int(n)
}
