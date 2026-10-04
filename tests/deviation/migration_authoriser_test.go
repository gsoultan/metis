package deviation_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/endpoints/definition"
	"github.com/gsoultan/metis/server/endpoints/process"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// twoVersionsOfOneStep deploys start → step → end twice under one key, with an
// instance waiting at the step on the first version.
func (h *deviationHarness) twoVersionsOfOneStep(t *testing.T) (v1, v2, instanceID uuid.UUID) {
	t.Helper()
	instanceID = h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	instance, err := h.svc.GetInstance(h.tenantContext(), instanceID)
	if err != nil || instance.Definition == nil {
		t.Fatalf("read the instance: %v", err)
	}
	v1 = instance.Definition.ID
	first, err := h.svc.GetDefinition(h.tenantContext(), v1)
	if err != nil {
		t.Fatalf("read v1: %v", err)
	}
	second := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: first.Key, Name: first.Name,
		Nodes: first.Nodes, Flows: first.Flows,
	}
	if v2, err = h.svc.CreateDefinition(h.tenantContext(), second); err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	return v1, v2, instanceID
}

// TestAMigrationByAnAccountWithNoNameIsRefusedAsAnActivationIs.
//
// Root cause: the migration endpoint dropped the caller without a word when
// the signed-in account had no username, and the service then recorded the
// decision as made by "System" — the server. Starting a step inside an ad-hoc
// sub-process refuses such an account, because a person acted and the ledger
// cannot say who. A migration refuses it too, with the same status, for a
// preview as for an apply, and nothing is decided.
func TestAMigrationByAnAccountWithNoNameIsRefusedAsAnActivationIs(t *testing.T) {
	h := newDeviationHarness(t)
	v1, v2, instanceID := h.twoVersionsOfOneStep(t)
	research := h.startResearch(t)
	apply := false

	for _, name := range []string{"", "   "} {
		nameless := context.WithValue(h.tenantContext(), pkgauth.UserContextKey,
			entities.User{ID: uuid.Must(uuid.NewV7()), Username: name, Roles: []string{entities.RoleAdmin}})

		activated, err := process.MakeActivateAdHocTaskEndpoint(h.svc)(nameless, process.ActivateAdHocTaskRequest{
			InstanceID: research.String(), SubProcessNodeID: "research", TaskNodeID: "call-customer",
		})
		if err != nil {
			t.Fatalf("activate: %v", err)
		}
		activation := activated.(process.ActivateAdHocTaskResponse).Err
		if activation == nil {
			t.Fatalf("an activation by a signed-in account named %q was made; the comparison below would prove nothing", name)
		}

		for _, dryRun := range []*bool{nil, &apply} {
			reply, err := definition.MakeMigrateInstancesEndpoint(h.svc)(nameless, definition.MigrateInstancesRequest{
				SourceDefinitionID: v1.String(), TargetDefinitionID: v2.String(), DryRun: dryRun,
				NodeActions: map[string]servicecontracts.NodeAction{
					"step": {Kind: servicecontracts.NodeActionHold, Reason: "the approver has left"},
				},
			})
			if err != nil {
				t.Fatalf("migrate: %v", err)
			}
			migration := reply.(definition.MigrateInstancesResponse)
			if migration.Err == nil {
				t.Errorf("a migration (dry run %v) by a signed-in account named %q was accepted, applied=%v",
					dryRun == nil, name, migration.Applied)
				continue
			}
			if got, want := common.CodeFrom(migration.Err), common.CodeFrom(activation); got != want || got != http.StatusInternalServerError {
				t.Errorf("the migration's refusal is a %d and the activation's a %d; want both the server's fault (500): %v",
					got, want, migration.Err)
			}
		}
	}

	rows, err := h.svc.ListInstanceDeviations(h.tenantContext(), instanceID)
	if err != nil || len(rows) != 0 {
		t.Errorf("the ledger after the refused migrations: %+v (err %v), want nothing", rows, err)
	}
	if incidents, err := h.svc.ListIncidents(h.tenantContext(), instanceID); err != nil || len(incidents) != 0 {
		t.Errorf("%d incident(s) were raised by the refused migrations (err %v)", len(incidents), err)
	}
	instance, err := h.svc.GetInstance(h.tenantContext(), instanceID)
	if err != nil || instance.Definition == nil || instance.Definition.ID != v1 {
		t.Errorf("the instance is on %v after the refused migrations (err %v), want the version it started on", instance.Definition, err)
	}
}
