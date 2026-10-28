// Package tenant holds the cross-tenant isolation tests: proof that a caller
// authenticated into one organization cannot read another organization's rows,
// on every SQL engine the product supports.
//
// These run against SQLite always, and against PostgreSQL and MySQL when
// METIS_TEST_POSTGRES_DSN / METIS_TEST_MYSQL_DSN are set. The scope is built
// from SQL joins, so the dialect is part of what needs proving — SQLite
// accepting a join says nothing about how MySQL resolves the same column names.
package tenant

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/auditentry"
	"github.com/gsoultan/metis/server/repositories/store/connector"
	"github.com/gsoultan/metis/server/repositories/store/connectorinstance"
	"github.com/gsoultan/metis/server/repositories/store/decisiondefinition"
	"github.com/gsoultan/metis/server/repositories/store/decisionrelease"
	"github.com/gsoultan/metis/server/repositories/store/deployment"
	"github.com/gsoultan/metis/server/repositories/store/externaltask"
	"github.com/gsoultan/metis/server/repositories/store/form"
	"github.com/gsoultan/metis/server/repositories/store/notification"
	"github.com/gsoultan/metis/server/repositories/store/organization"
	"github.com/gsoultan/metis/server/repositories/store/processdefinition"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	projectstore "github.com/gsoultan/metis/server/repositories/store/project"
	"github.com/gsoultan/metis/server/repositories/store/resource"
	"github.com/gsoultan/metis/server/repositories/store/subscription"
	taskstore "github.com/gsoultan/metis/server/repositories/store/task"
	"github.com/gsoultan/storm/runtime"

	"github.com/gsoultan/metis/internal/pkg/features"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/metis/tests/testutils"
)

const (
	sharedUserID        = "user-in-both-inboxes"
	sharedTopic         = "shared-topic"
	sharedSignal        = "order.cancelled"
	sharedFormKey       = "expense-form"
	sharedConnKey       = "connector-template"
	sharedDefinitionKey = "invoice-approval"
	sharedDecisionKey   = "discount-policy"

	// testMaxConns keeps the server-backed pools small; these tests are
	// sequential and a wide pool only slows the schema setup down.
	testMaxConns = 2
)

// forEachDialect runs body against the engine the product supports.
// The name is kept because every caller says it and there is nothing clearer
// to call "run this against a database".
func forEachDialect(t *testing.T, body func(t *testing.T, db *db.Conn)) {
	t.Helper()

	// One engine. This ran each case against SQLite and then PostgreSQL, back
	// when both were supported and the SQLite pass was the only one that ever
	// ran — the PostgreSQL DSN was set nowhere. Both helpers now open the same
	// engine, so running twice would assert the same thing twice.
	engines := []struct {
		name string
		open func(*testing.T) *db.Conn
	}{
		{"postgres", func(t *testing.T) *db.Conn { return testutils.SetupPostgresDB(t, testMaxConns) }},
	}

	for _, engine := range engines {
		t.Run(engine.name, func(t *testing.T) {
			body(t, engine.open(t))
		})
	}
}

// tenantFixture is two organizations that own one of everything, so every
// assertion below can ask the same question: reading as A, do I ever see B?
type tenantFixture struct {
	orgA, orgB           uuid.UUID
	projectA, projectB   uuid.UUID
	instanceA, instanceB uuid.UUID

	auditA, auditB            uuid.UUID
	formA, formB              uuid.UUID
	deploymentA, deploymentB  uuid.UUID
	resourceA, resourceB      uuid.UUID
	subscriptionA, subB       uuid.UUID
	externalTaskA, extB       uuid.UUID
	connectorInstA, connInstB uuid.UUID
	connectorID               uuid.UUID
	notificationA, notifB     uuid.UUID
	systemNotification        uuid.UUID
	definitionA, definitionB  uuid.UUID
	decisionA, decisionB      uuid.UUID
	taskA, taskB              uuid.UUID
}

// ctxAsA returns a context carrying organization A as the active tenant, which
// is what the auth interceptor injects on a real request.
func (f tenantFixture) ctxAsA(t *testing.T) context.Context {
	t.Helper()
	return entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: f.orgA.String()})
}

// seedTenantFixture writes one row per tenant-owned table for each of two
// organizations. Where a filter exists (form key, signal name, topic, user id)
// both organizations deliberately use the same value, so a read that returns
// the wrong tenant's row cannot be explained away as a filter mismatch.
func seedTenantFixture(t *testing.T, db *db.Conn) tenantFixture {
	t.Helper()

	f := tenantFixture{
		orgA: uuid.New(), orgB: uuid.New(),
		projectA: uuid.New(), projectB: uuid.New(),
		instanceA: uuid.New(), instanceB: uuid.New(),
		auditA: uuid.New(), auditB: uuid.New(),
		formA: uuid.New(), formB: uuid.New(),
		deploymentA: uuid.New(), deploymentB: uuid.New(),
		resourceA: uuid.New(), resourceB: uuid.New(),
		subscriptionA: uuid.New(), subB: uuid.New(),
		externalTaskA: uuid.New(), extB: uuid.New(),
		connectorInstA: uuid.New(), connInstB: uuid.New(),
		connectorID:   uuid.New(),
		notificationA: uuid.New(), notifB: uuid.New(),
		systemNotification: uuid.New(),
		definitionA:        uuid.New(), definitionB: uuid.New(),
		decisionA: uuid.New(), decisionB: uuid.New(),
		taskA: uuid.New(), taskB: uuid.New(),
	}

	ctx, ex := t.Context(), testutils.Executor(db)
	now := time.Now()
	insert := func(_ int64, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	project := func(v uuid.UUID) runtime.Null[[16]byte] { return runtime.Null[[16]byte]{V: v, Valid: true} }
	empty, none := runtime.JSON("[]"), runtime.JSON("{}")

	insert(organization.InsertAll(ctx, ex, []organization.Row{
		{ID: f.orgA, CreatedAt: now, UpdatedAt: now, Name: "Org A"},
		{ID: f.orgB, CreatedAt: now, UpdatedAt: now, Name: "Org B"},
	}))
	insert(projectstore.InsertAll(ctx, ex, []projectstore.Row{
		{ID: f.projectA, CreatedAt: now, UpdatedAt: now, OrganizationID: f.orgA, Name: "Project A"},
		{ID: f.projectB, CreatedAt: now, UpdatedAt: now, OrganizationID: f.orgB, Name: "Project B"},
	}))
	insert(connector.InsertAll(ctx, ex, []connector.Row{
		{ID: f.connectorID, CreatedAt: now, UpdatedAt: now, Key: sharedConnKey, Name: "Shared template", Properties: empty},
	}))

	// Definitions and decisions share a key across organizations, which
	// nothing prevents — key is unique per project, not globally.
	insert(processdefinition.InsertAll(ctx, ex, []processdefinition.Row{
		{ID: f.definitionA, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectA, Key: sharedDefinitionKey, Name: "def A", Version: 1, Nodes: empty, Flows: empty},
		{ID: f.definitionB, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectB, Key: sharedDefinitionKey, Name: "def B", Version: 2, Nodes: empty, Flows: empty},
	}))
	decision := func(id, project uuid.UUID, name string, version int64) decisiondefinition.Row {
		return decisiondefinition.Row{ID: id, CreatedAt: now, UpdatedAt: now, ProjectID: project, Key: sharedDecisionKey, Name: name, Version: version,
			RequiredDecisions: empty, Inputs: empty, Outputs: empty, Rules: empty, Tests: empty}
	}
	insert(decisiondefinition.InsertAll(ctx, ex, []decisiondefinition.Row{
		decision(f.decisionA, f.projectA, "dec A", 1),
		decision(f.decisionB, f.projectB, "dec B", 2),
	}))
	// Each is its key's live version, so a live lookup has an answer in
	// both organizations to get wrong.
	insert(decisionrelease.InsertAll(ctx, ex, []decisionrelease.Row{
		{ID: uuid.New(), CreatedAt: now, UpdatedAt: now, ProjectID: f.projectA, DecisionKey: sharedDecisionKey, Version: 1, ActivateAt: now.Add(-time.Hour)},
		{ID: uuid.New(), CreatedAt: now, UpdatedAt: now, ProjectID: f.projectB, DecisionKey: sharedDecisionKey, Version: 2, ActivateAt: now.Add(-time.Hour)},
	}))
	instance := func(id, project, definition uuid.UUID) processinstance.Row {
		return processinstance.Row{ID: id, CreatedAt: now, UpdatedAt: now, ProjectID: project, DefinitionID: definition,
			Status: string(models.ProcessActive), Tokens: empty, CompletedNodes: empty, CompensatedNodes: empty, MultiInstance: none, Joins: none}
	}
	insert(processinstance.InsertAll(ctx, ex, []processinstance.Row{
		instance(f.instanceA, f.projectA, f.definitionA),
		instance(f.instanceB, f.projectB, f.definitionB),
	}))

	insert(auditentry.InsertAll(ctx, ex, []auditentry.Row{
		{ID: f.auditA, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectA, InstanceID: f.instanceA, Message: "a", Data: none},
		{ID: f.auditB, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectB, InstanceID: f.instanceB, Message: "b", Data: none},
	}))
	insert(form.InsertAll(ctx, ex, []form.Row{
		{ID: f.formA, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectA, Key: sharedFormKey, Fields: none},
		{ID: f.formB, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectB, Key: sharedFormKey, Fields: none},
	}))
	insert(deployment.InsertAll(ctx, ex, []deployment.Row{
		{ID: f.deploymentA, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectA, Name: "dep A"},
		{ID: f.deploymentB, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectB, Name: "dep B"},
	}))
	insert(resource.InsertAll(ctx, ex, []resource.Row{
		{ID: f.resourceA, CreatedAt: now, UpdatedAt: now, DeploymentID: f.deploymentA, Name: "res A"},
		{ID: f.resourceB, CreatedAt: now, UpdatedAt: now, DeploymentID: f.deploymentB, Name: "res B"},
	}))
	insert(subscription.InsertAll(ctx, ex, []subscription.Row{
		{ID: f.subscriptionA, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectA, InstanceID: f.instanceA, Type: string(models.SubscriptionSignal), EventName: sharedSignal},
		{ID: f.subB, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectB, InstanceID: f.instanceB, Type: string(models.SubscriptionSignal), EventName: sharedSignal},
	}))
	insert(externaltask.InsertAll(ctx, ex, []externaltask.Row{
		{ID: f.externalTaskA, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectA, InstanceID: f.instanceA, DefinitionID: f.definitionA, Topic: sharedTopic, Retries: 3, Variables: none},
		{ID: f.extB, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectB, InstanceID: f.instanceB, DefinitionID: f.definitionB, Topic: sharedTopic, Retries: 3, Variables: none},
	}))
	insert(connectorinstance.InsertAll(ctx, ex, []connectorinstance.Row{
		{ID: f.connectorInstA, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectA, ConnectorID: f.connectorID, Name: "conn A"},
		{ID: f.connInstB, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectB, ConnectorID: f.connectorID, Name: "conn B"},
	}))
	insert(taskstore.InsertAll(ctx, ex, []taskstore.Row{
		{ID: f.taskA, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectA, InstanceID: f.instanceA, Name: "task A", Status: string(models.TaskUnclaimed), CandidateUsers: empty, CandidateGroups: empty},
		{ID: f.taskB, CreatedAt: now, UpdatedAt: now, ProjectID: f.projectB, InstanceID: f.instanceB, Name: "task B", Status: string(models.TaskUnclaimed), CandidateUsers: empty, CandidateGroups: empty},
	}))
	// Both organizations address a notification to the same user id, plus
	// one system notification that belongs to no project at all.
	insert(notification.InsertAll(ctx, ex, []notification.Row{
		{ID: f.notificationA, CreatedAt: now, UpdatedAt: now, UserID: sharedUserID, ProjectID: project(f.projectA), Title: "a"},
		{ID: f.notifB, CreatedAt: now, UpdatedAt: now, UserID: sharedUserID, ProjectID: project(f.projectB), Title: "b"},
		{ID: f.systemNotification, CreatedAt: now, UpdatedAt: now, UserID: sharedUserID, Title: "system"},
	}))
	return f
}

// TestTenantIsolation_ListsExcludeOtherTenants asserts that every list-shaped
// read on a tenant-owned table returns the caller's rows and nothing else, even
// when the other organization holds a row matching the same filter.
func TestTenantIsolation_ListsExcludeOtherTenants(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *db.Conn) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		tests := []struct {
			name string
			read func() ([]uuid.UUID, error)
			want []uuid.UUID
		}{
			{
				name: "audit of another tenant's project",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewAuditRepository(db).ListByProject(ctx, f.projectB, 1000)
					return idsOf(rows, func(m models.AuditModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "audit of another tenant's instance",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewAuditRepository(db).ListByInstance(ctx, f.instanceB)
					return idsOf(rows, func(m models.AuditModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "audit of own project",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewAuditRepository(db).ListByProject(ctx, f.projectA, 1000)
					return idsOf(rows, func(m models.AuditModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.auditA},
			},
			{
				name: "forms across all projects",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewFormRepository(db).ListByProject(ctx, uuid.Nil)
					return idsOf(rows, func(m models.FormModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.formA},
			},
			{
				name: "deployments across all projects",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewDeploymentRepository(db).ListByProject(ctx, uuid.Nil)
					return idsOf(rows, func(m models.DeploymentModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.deploymentA},
			},
			{
				name: "deployment resources of another tenant's deployment",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewDeploymentRepository(db).ListResources(ctx, f.deploymentB)
					return idsOf(rows, func(m models.ResourceModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "signal correlation cannot cross tenants",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewSubscriptionRepository(db).FindSignals(ctx, f.projectB, sharedSignal)
					return idsOf(rows, func(m models.Subscription) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "subscriptions of another tenant's instance",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewSubscriptionRepository(db).ListByInstance(ctx, f.instanceB)
					return idsOf(rows, func(m models.Subscription) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "external tasks of another tenant's instance",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewExternalTaskRepository(db).ListByProcessInstance(ctx, f.instanceB)
					return idsOf(rows, func(m *models.ExternalTaskModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				// Both organizations publish work on the same topic name, which
				// nothing prevents. A worker polling as A must never be handed
				// B's task, and both tasks are unlocked and eligible here.
				name: "worker long-poll on a topic both tenants use",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewExternalTaskRepository(db).FetchAndLock(ctx, sharedTopic, "worker-a", 10, 30_000)
					return idsOf(rows, func(m *models.ExternalTaskModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.externalTaskA},
			},
			{
				name: "connector instances of another tenant's project",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewConnectorInstanceRepository(db).ListByProject(ctx, f.projectB)
					return idsOf(rows, func(m models.ConnectorInstance) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "notifications keep system messages and drop the other tenant's",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewNotificationRepository(db).ListByUser(ctx, sharedUserID)
					return idsOf(rows, func(m models.NotificationModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.notificationA, f.systemNotification},
			},
			{
				name: "a page of notifications keeps system messages and drops the other tenant's",
				read: func() ([]uuid.UUID, error) {
					page, err := pg.NewNotificationRepository(db).
						ListByUserPaged(ctx, sharedUserID, contracts.Pagination{})
					return idsOf(page.Items, func(m models.NotificationModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.notificationA, f.systemNotification},
			},
			// Both organizations hold a decision under the same key; the other
			// one's is the newer version, and each is live in its own project,
			// so a list that picked a row regardless of tenant would answer
			// with it.
			{
				name: "decision keys of another tenant's project",
				read: func() ([]uuid.UUID, error) {
					page, err := pg.NewDecisionRepository(db).ListKeysByProject(ctx, f.projectB, "", contracts.Pagination{})
					return idsOf(page.Items, func(m models.DecisionSummaryModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "decision keys across all projects",
				read: func() ([]uuid.UUID, error) {
					page, err := pg.NewDecisionRepository(db).ListKeysByProject(ctx, uuid.Nil, "", contracts.Pagination{})
					return idsOf(page.Items, func(m models.DecisionSummaryModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.decisionA},
			},
			{
				name: "a decision search in another tenant's project",
				read: func() ([]uuid.UUID, error) {
					page, err := pg.NewDecisionRepository(db).ListByProjectPaged(ctx, f.projectB, "dec", contracts.Pagination{})
					return idsOf(page.Items, func(m models.DecisionDefinitionModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "a decision search across all projects",
				read: func() ([]uuid.UUID, error) {
					page, err := pg.NewDecisionRepository(db).ListByProjectPaged(ctx, uuid.Nil, "dec", contracts.Pagination{})
					return idsOf(page.Items, func(m models.DecisionDefinitionModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.decisionA},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got, err := tc.read()
				if err != nil {
					t.Fatalf("read: %v", err)
				}
				assertSameIDs(t, got, tc.want)
			})
		}
	})
}

// TestTenantIsolation_GetByIDDeniesOtherTenants asserts that holding another
// organization's row ID is not enough to read the row: every scoped Get answers
// "not found" rather than handing the record over.
func TestTenantIsolation_GetByIDDeniesOtherTenants(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *db.Conn) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		tests := []struct {
			name string
			read func() error
		}{
			{"form", func() error { _, err := pg.NewFormRepository(db).Get(ctx, f.formB); return err }},
			{"form by key", func() error {
				_, err := pg.NewFormRepository(db).GetByKey(ctx, f.projectB, sharedFormKey)
				return err
			}},
			{"deployment", func() error {
				_, err := pg.NewDeploymentRepository(db).Get(ctx, f.deploymentB)
				return err
			}},
			{"deployment resource", func() error {
				_, err := pg.NewDeploymentRepository(db).GetResource(ctx, f.resourceB)
				return err
			}},
			{"external task", func() error {
				_, err := pg.NewExternalTaskRepository(db).Get(ctx, f.extB)
				return err
			}},
			{"connector instance", func() error {
				_, err := pg.NewConnectorInstanceRepository(db).Get(ctx, f.connInstB)
				return err
			}},
			{"connector instance by project and connector", func() error {
				_, err := pg.NewConnectorInstanceRepository(db).GetByProjectAndConnector(ctx, f.projectB, f.connectorID)
				return err
			}},
			{"task", func() error { _, err := pg.NewTaskRepository(db).Get(ctx, f.taskB); return err }},
			{"process instance", func() error {
				_, err := pg.NewProcessRepository(db).Get(ctx, f.instanceB)
				return err
			}},
			{"process instance for update", func() error {
				_, err := pg.NewProcessRepository(db).GetForUpdate(ctx, f.instanceB)
				return err
			}},
			{"process definition", func() error {
				_, err := pg.NewDefinitionRepository(db).Get(ctx, f.definitionB)
				return err
			}},
			{"decision", func() error {
				_, err := pg.NewDecisionRepository(db).Get(ctx, f.decisionB)
				return err
			}},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.read()
				if !isNotFound(err) {
					t.Fatalf("reading another tenant's row: got err %v, want a not-found", err)
				}
			})
		}
	})
}

// TestTenantIsolation_KeyLookupsStayInTenant covers the lookups that resolve a
// business key rather than a UUID. Keys are unique per project, so both
// organizations hold one under the same name and only the caller's may answer.
func TestTenantIsolation_KeyLookupsStayInTenant(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *db.Conn) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		t.Run("definition by key resolves to own project", func(t *testing.T) {
			// B's row has the higher version, so an unscoped "latest wins"
			// lookup returns B.
			got, err := pg.NewDefinitionRepository(db).GetByKey(ctx, sharedDefinitionKey)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if uuid.UUID(got.ID) != f.definitionA {
				t.Fatalf("got definition %v, want own %v", uuid.UUID(got.ID), f.definitionA)
			}
		})

		t.Run("definition by key and version denies another tenant's version", func(t *testing.T) {
			_, err := pg.NewDefinitionRepository(db).GetByKeyAndVersion(ctx, sharedDefinitionKey, 2)
			if !isNotFound(err) {
				t.Fatalf("got %v, want a not-found", err)
			}
		})

		t.Run("live decision by key resolves to own project", func(t *testing.T) {
			got, err := pg.NewDecisionRepository(db).GetLiveByKey(ctx, f.projectA, sharedDecisionKey)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if uuid.UUID(got.ID) != f.decisionA {
				t.Fatalf("got decision %v, want own %v", uuid.UUID(got.ID), f.decisionA)
			}
		})

		// The timeline is scoped as the versions are: naming the other
		// tenant's project reads neither its live version nor its table.
		t.Run("live decision in another tenant's project is not found", func(t *testing.T) {
			_, err := pg.NewDecisionRepository(db).GetLiveByKey(ctx, f.projectB, sharedDecisionKey)
			if !isNotFound(err) {
				t.Fatalf("got %v, want a not-found", err)
			}
			_, err = pg.NewDecisionRepository(db).GetRelease(ctx, f.projectB, sharedDecisionKey)
			if !isNotFound(err) {
				t.Fatalf("release: got %v, want a not-found", err)
			}
		})

		// Naming the other tenant's project outright is no way round the scope.
		t.Run("decision by key and version denies another tenant's version", func(t *testing.T) {
			_, err := pg.NewDecisionRepository(db).GetByKeyAndVersion(ctx, f.projectB, sharedDecisionKey, 2)
			if !isNotFound(err) {
				t.Fatalf("got %v, want a not-found", err)
			}
		})
	})
}

// TestTenantIsolation_WritesDenyOtherTenants asserts that a write aimed at
// another organization's row is refused *and* leaves the row alone. Returning an
// error is not enough on its own — GORM's Save ignores a preceding Where, so a
// scope written that way would report failure while the UPDATE still landed.
func TestTenantIsolation_WritesDenyOtherTenants(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *db.Conn) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		tests := []struct {
			name string
			// write attempts the mutation as tenant A against tenant B's row.
			write func() error
			// unchanged reports whether B's row still holds its seeded value.
			unchanged func() bool
		}{
			{
				name:  "delete another tenant's form",
				write: func() error { return pg.NewFormRepository(db).Delete(ctx, f.formB) },
				unchanged: func() bool {
					return stillThere(t)(form.New().IDEq(f.formB).One(t.Context(), testutils.Executor(db)))
				},
			},
			{
				name:  "delete another tenant's definition",
				write: func() error { return pg.NewDefinitionRepository(db).Delete(ctx, f.definitionB) },
				unchanged: func() bool {
					return stillThere(t)(processdefinition.New().IDEq(f.definitionB).One(t.Context(), testutils.Executor(db)))
				},
			},
			{
				name:  "delete another tenant's decision",
				write: func() error { return pg.NewDecisionRepository(db).Delete(ctx, f.decisionB) },
				unchanged: func() bool {
					return stillThere(t)(decisiondefinition.New().IDEq(f.decisionB).One(t.Context(), testutils.Executor(db)))
				},
			},
			{
				name: "delete another tenant's connector instance",
				write: func() error {
					return pg.NewConnectorInstanceRepository(db).Delete(ctx, f.connInstB)
				},
				unchanged: func() bool {
					return stillThere(t)(connectorinstance.New().IDEq(f.connInstB).One(t.Context(), testutils.Executor(db)))
				},
			},
			{
				name: "repoint another tenant's connector instance",
				write: func() error {
					return pg.NewConnectorInstanceRepository(db).Update(ctx, models.ConnectorInstance{
						Base:      models.Base{ID: models.FromUUID(f.connInstB)},
						ProjectID: models.FromUUID(f.projectA),
						Name:      "stolen",
					})
				},
				unchanged: func() bool {
					m := reload[connectorinstance.Row](t)(connectorinstance.New().IDEq(f.connInstB).One(t.Context(), testutils.Executor(db)))
					return m.Name == "conn B" && uuid.UUID(m.ProjectID) == f.projectB
				},
			},
			{
				name: "delete another tenant's notification",
				// Its own recipient's name, so only the organization's scope stands in the way.
				write: func() error {
					return pg.NewNotificationRepository(db).Delete(ctx, f.notifB, sharedUserID)
				},
				unchanged: func() bool {
					return stillThere(t)(notification.New().IDEq(f.notifB).One(t.Context(), testutils.Executor(db)))
				},
			},
			{
				name: "mark another tenant's notification read",
				// Its own recipient's name, so only the organization's scope stands in the way.
				write: func() error {
					return pg.NewNotificationRepository(db).MarkAsRead(ctx, f.notifB, sharedUserID)
				},
				unchanged: func() bool {
					m := reload[notification.Row](t)(notification.New().IDEq(f.notifB).One(t.Context(), testutils.Executor(db)))
					return !m.IsRead
				},
			},
			{
				name:  "delete another tenant's subscription",
				write: func() error { return pg.NewSubscriptionRepository(db).Delete(ctx, f.subB) },
				unchanged: func() bool {
					return stillThere(t)(subscription.New().IDEq(f.subB).One(t.Context(), testutils.Executor(db)))
				},
			},
			{
				name: "redirect another tenant's correlation key",
				write: func() error {
					return pg.NewSubscriptionRepository(db).UpdateCorrelationKey(ctx, f.subB, "hijacked")
				},
				unchanged: func() bool {
					m := reload[subscription.Row](t)(subscription.New().IDEq(f.subB).One(t.Context(), testutils.Executor(db)))
					return m.CorrelationKey.V != "hijacked"
				},
			},
			{
				name: "resolve another tenant's external task",
				write: func() error {
					return pg.NewExternalTaskRepository(db).Update(ctx, &models.ExternalTaskModel{
						Base:      models.Base{ID: models.FromUUID(f.extB)},
						ProjectID: models.FromUUID(f.projectA),
						Topic:     "stolen",
					})
				},
				unchanged: func() bool {
					m := reload[externaltask.Row](t)(externaltask.New().IDEq(f.extB).One(t.Context(), testutils.Executor(db)))
					return m.Topic == sharedTopic && uuid.UUID(m.ProjectID) == f.projectB
				},
			},
			{
				name:  "delete another tenant's external task",
				write: func() error { return pg.NewExternalTaskRepository(db).Delete(ctx, f.extB) },
				unchanged: func() bool {
					return stillThere(t)(externaltask.New().IDEq(f.extB).One(t.Context(), testutils.Executor(db)))
				},
			},
			{
				name: "move another tenant's task status",
				write: func() error {
					return pg.NewTaskRepository(db).UpdateStatus(ctx, f.taskB, models.TaskCompleted)
				},
				unchanged: func() bool {
					m := reload[taskstore.Row](t)(taskstore.New().IDEq(f.taskB).One(t.Context(), testutils.Executor(db)))
					return m.Status == string(models.TaskUnclaimed)
				},
			},
			{
				name: "rewrite another tenant's task",
				write: func() error {
					return pg.NewTaskRepository(db).Update(ctx, models.TaskModel{
						Base:      models.Base{ID: models.FromUUID(f.taskB)},
						ProjectID: models.FromUUID(f.projectA),
						Name:      "stolen",
					})
				},
				unchanged: func() bool {
					m := reload[taskstore.Row](t)(taskstore.New().IDEq(f.taskB).One(t.Context(), testutils.Executor(db)))
					return m.Name == "task B" && uuid.UUID(m.ProjectID) == f.projectB
				},
			},
			{
				name: "rewrite another tenant's process instance",
				write: func() error {
					return pg.NewProcessRepository(db).Update(ctx, models.ProcessInstanceModel{
						Base:      models.Base{ID: models.FromUUID(f.instanceB)},
						ProjectID: models.FromUUID(f.projectA),
						Status:    models.ProcessFailed,
					})
				},
				unchanged: func() bool {
					m := reload[processinstance.Row](t)(processinstance.New().IDEq(f.instanceB).One(t.Context(), testutils.Executor(db)))
					return m.Status == string(models.ProcessActive) && uuid.UUID(m.ProjectID) == f.projectB
				},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				if err := tc.write(); !isNotFound(err) {
					t.Errorf("write: got err %v, want a not-found", err)
				}
				if !tc.unchanged() {
					t.Fatal("the write was refused but the row changed anyway")
				}
			})
		}
	})
}

// TestTenantIsolation_OwnWritesStillSucceed is the counterweight: the guard must
// not turn the caller's own writes into not-found.
func TestTenantIsolation_OwnWritesStillSucceed(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *db.Conn) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		writes := []struct {
			name  string
			write func() error
		}{
			{"mark own notification read", func() error {
				return pg.NewNotificationRepository(db).MarkAsRead(ctx, f.notificationA, sharedUserID)
			}},
			{"mark a system notification read", func() error {
				return pg.NewNotificationRepository(db).MarkAsRead(ctx, f.systemNotification, sharedUserID)
			}},
			{"mark whole inbox read", func() error {
				return pg.NewNotificationRepository(db).MarkAllAsRead(ctx, sharedUserID)
			}},
			{"own task status", func() error {
				return pg.NewTaskRepository(db).UpdateStatus(ctx, f.taskA, models.TaskClaimed)
			}},
			{"own correlation key", func() error {
				return pg.NewSubscriptionRepository(db).UpdateCorrelationKey(ctx, f.subscriptionA, "resolved")
			}},
			{"own connector instance", func() error {
				// Load before saving, the way the service layer does. A model
				// built from scratch has a zero CreatedAt, which MySQL rejects
				// in strict mode — that is a Save footgun, not a scope failure.
				repo := pg.NewConnectorInstanceRepository(db)
				m, err := repo.Get(ctx, f.connectorInstA)
				if err != nil {
					return err
				}
				m.Name = "renamed"
				return repo.Update(ctx, m)
			}},
			{"own form deleted", func() error {
				return pg.NewFormRepository(db).Delete(ctx, f.formA)
			}},
		}

		for _, tc := range writes {
			t.Run(tc.name, func(t *testing.T) {
				if err := tc.write(); err != nil {
					t.Errorf("own write refused: %v", err)
				}
			})
		}

		// Marking the whole inbox read must not have reached the other tenant's
		// notification, which shares the user id.
		other := reload[notification.Row](t)(notification.New().IDEq(f.notifB).One(t.Context(), testutils.Executor(db)))
		if other.IsRead {
			t.Error("MarkAllAsRead crossed into the other tenant's inbox")
		}
	})
}

// TestTenantIsolation_ProjectsAreScoped covers the table every other scope joins
// *through*, and which nothing scoped as a result: unscoped, List returned every
// organization's projects to anyone authenticated.
func TestTenantIsolation_ProjectsAreScoped(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *db.Conn) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)
		// Projects have moved to storm; the scoping property is the same and
		// is asserted through the same contract.
		repo := pg.NewProjectRepository(db)

		t.Run("list returns only the caller's projects", func(t *testing.T) {
			rows, err := repo.List(ctx)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			assertSameIDs(t, idsOf(rows, func(m models.ProjectModel) uuid.UUID { return uuid.UUID(m.ID) }),
				[]uuid.UUID{f.projectA})
		})

		t.Run("naming another organization returns nothing", func(t *testing.T) {
			rows, err := repo.ListByOrganization(ctx, f.orgB)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			assertSameIDs(t, idsOf(rows, func(m models.ProjectModel) uuid.UUID { return uuid.UUID(m.ID) }), nil)
		})

		t.Run("get denies another tenant's project", func(t *testing.T) {
			if _, err := repo.Get(ctx, f.projectB); !errors.Is(err, apierr.ErrNotFound) {
				t.Fatalf("got %v, want %v", err, apierr.ErrNotFound)
			}
		})

		t.Run("get allows the caller's own", func(t *testing.T) {
			if _, err := repo.Get(ctx, f.projectA); err != nil {
				t.Fatalf("own project: %v", err)
			}
		})

		t.Run("delete denies another tenant's project", func(t *testing.T) {
			if err := repo.Delete(ctx, f.projectB); !errors.Is(err, apierr.ErrNotFound) {
				t.Errorf("got %v, want %v", err, apierr.ErrNotFound)
			}
			if !stillThere(t)(projectstore.New().IDEq(f.projectB).One(t.Context(), testutils.Executor(db))) {
				t.Fatal("the delete was refused but the project is gone")
			}
		})

		t.Run("create denies planting into another organization", func(t *testing.T) {
			err := repo.Create(ctx, models.ProjectModel{
				Base:           models.Base{ID: models.FromUUID(uuid.New())},
				OrganizationID: models.FromUUID(f.orgB),
				Name:           "planted",
			})
			if !errors.Is(err, apierr.ErrNotFound) {
				t.Fatalf("got %v, want %v", err, apierr.ErrNotFound)
			}
		})
	})
}

// TestTenantIsolation_CreateDeniesForeignProject is the create-side counterpart
// of the read scope. Every create names its parent project, and the request body
// is the caller's to choose — so without this check a caller could plant rows in
// another organization's project.
func TestTenantIsolation_CreateDeniesForeignProject(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *db.Conn) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		foreign := models.FromUUID(f.projectB)
		newID := func() models.Base { return models.Base{ID: models.FromUUID(uuid.New())} }

		tests := []struct {
			name   string
			create func() error
		}{
			{"definition", func() error {
				return pg.NewDefinitionRepository(db).Create(ctx,
					models.ProcessDefinitionModel{Base: newID(), ProjectID: foreign, Key: "planted"})
			}},
			{"decision", func() error {
				return pg.NewDecisionRepository(db).Create(ctx,
					models.DecisionDefinitionModel{Base: newID(), ProjectID: foreign, Key: "planted"})
			}},
			{"form", func() error {
				return pg.NewFormRepository(db).Create(ctx,
					models.FormModel{Base: newID(), ProjectID: foreign, Key: "planted"})
			}},
			{"deployment", func() error {
				return pg.NewDeploymentRepository(db).Create(ctx,
					models.DeploymentModel{Base: newID(), ProjectID: foreign, Name: "planted"})
			}},
			{"task", func() error {
				return pg.NewTaskRepository(db).Create(ctx,
					models.TaskModel{Base: newID(), ProjectID: foreign, Name: "planted"})
			}},
			{"process instance", func() error {
				_, err := pg.NewProcessRepository(db).Create(ctx,
					models.ProcessInstanceModel{Base: newID(), ProjectID: foreign, Status: models.ProcessActive})
				return err
			}},
			{"connector instance", func() error {
				_, err := pg.NewConnectorInstanceRepository(db).Create(ctx,
					models.ConnectorInstance{Base: newID(), ProjectID: foreign, Name: "planted"})
				return err
			}},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				if err := tc.create(); !isNotFound(err) {
					t.Fatalf("creating into another tenant's project: got %v, want a not-found", err)
				}
			})
		}
	})
}

// TestTenantIsolation_CreateIntoOwnProjectSucceeds is the counterweight, so the
// guard cannot pass by refusing everything.
func TestTenantIsolation_CreateIntoOwnProjectSucceeds(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *db.Conn) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		own := models.FromUUID(f.projectA)
		if err := pg.NewFormRepository(db).Create(ctx, models.FormModel{
			Base: models.Base{ID: models.FromUUID(uuid.New())}, ProjectID: own, Key: "mine",
		}); err != nil {
			t.Errorf("create into own project: %v", err)
		}
		if err := pg.NewTaskRepository(db).Create(ctx, models.TaskModel{
			Base: models.Base{ID: models.FromUUID(uuid.New())}, ProjectID: own, InstanceID: models.FromUUID(f.instanceA), Name: "mine",
		}); err != nil {
			t.Errorf("create task into own project: %v", err)
		}
	})
}

// stillThere reports whether a store's read found the row, not deleted. The
// store is read outside the tenant scope — these assertions check the database,
// not the repository.
func stillThere(t *testing.T) func(any, bool, error) bool {
	return func(_ any, found bool, err error) bool {
		t.Helper()
		if err != nil {
			t.Fatalf("existence check: %v", err)
		}
		return found
	}
}

// reload is the row a store's read found, which must be there.
func reload[R any](t *testing.T) func(R, bool, error) R {
	return func(row R, found bool, err error) R {
		t.Helper()
		if err != nil || !found {
			t.Fatalf("reload: %v (found=%v)", err, found)
		}
		return row
	}
}

// TestTenantIsolation_OwnRowsStillReadable guards the other direction: the scope
// must not hide the caller's own data. A join that returned nothing for everyone
// would satisfy every assertion above.
func TestTenantIsolation_OwnRowsStillReadable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *db.Conn) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		reads := []struct {
			name string
			read func() error
		}{
			{"form", func() error { _, err := pg.NewFormRepository(db).Get(ctx, f.formA); return err }},
			{"form by key", func() error {
				_, err := pg.NewFormRepository(db).GetByKey(ctx, f.projectA, sharedFormKey)
				return err
			}},
			{"deployment", func() error {
				_, err := pg.NewDeploymentRepository(db).Get(ctx, f.deploymentA)
				return err
			}},
			{"deployment resource", func() error {
				_, err := pg.NewDeploymentRepository(db).GetResource(ctx, f.resourceA)
				return err
			}},
			{"external task", func() error {
				_, err := pg.NewExternalTaskRepository(db).Get(ctx, f.externalTaskA)
				return err
			}},
			{"connector instance", func() error {
				_, err := pg.NewConnectorInstanceRepository(db).Get(ctx, f.connectorInstA)
				return err
			}},
			{"connector instance by project and connector", func() error {
				_, err := pg.NewConnectorInstanceRepository(db).GetByProjectAndConnector(ctx, f.projectA, f.connectorID)
				return err
			}},
		}
		for _, tc := range reads {
			t.Run(tc.name, func(t *testing.T) {
				if err := tc.read(); err != nil {
					t.Errorf("own row unreadable: %v", err)
				}
			})
		}

		t.Run("own deployment resources", func(t *testing.T) {
			rows, err := pg.NewDeploymentRepository(db).ListResources(ctx, f.deploymentA)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			assertSameIDs(t, idsOf(rows, func(m models.ResourceModel) uuid.UUID { return uuid.UUID(m.ID) }), []uuid.UUID{f.resourceA})
		})

		t.Run("own signals", func(t *testing.T) {
			rows, err := pg.NewSubscriptionRepository(db).FindSignals(ctx, f.projectA, sharedSignal)
			if err != nil {
				t.Fatalf("find: %v", err)
			}
			assertSameIDs(t, idsOf(rows, func(m models.Subscription) uuid.UUID { return uuid.UUID(m.ID) }), []uuid.UUID{f.subscriptionA})
		})
	})
}

// TestTenantIsolation_NoTenantContextReadsEverything pins the documented
// fail-open behaviour of the scope helpers: the engine and its background
// workers run with no request context, and must keep seeing the whole
// installation. Changing that is a deliberate decision, not a silent one.
// What a context with no identity gets, which depends on the flag — and both
// answers are worth pinning, because the whole point of
// METIS_FEATURE_STRICT_TENANT_SCOPE is to change this one behaviour.
//
// Off (the shipped default), an unscoped read sees everything. That is the
// fail-open default SECURITY.md names as a deliberate decision, and a test
// asserting it is how anyone notices if it silently changes.
//
// On, the same read sees nothing. Asserting only the first would make this
// suite fail under the flag for the right reason and the wrong-looking one —
// it would read as a regression rather than as the feature working.
func TestTenantIsolation_NoTenantContextReadsWhatTheFlagAllows(t *testing.T) {
	strict := features.Enabled(features.StrictTenantScope)

	forEachDialect(t, func(t *testing.T, db *db.Conn) {
		f := seedTenantFixture(t, db)
		ctx := t.Context()

		forms, err := pg.NewFormRepository(db).ListByProject(ctx, uuid.Nil)
		if err != nil {
			t.Fatalf("list forms: %v", err)
		}
		wantForms := []uuid.UUID{f.formA, f.formB}
		if strict {
			wantForms = nil
		}
		assertSameIDs(t, idsOf(forms, func(m models.FormModel) uuid.UUID { return uuid.UUID(m.ID) }), wantForms)

		notifications, err := pg.NewNotificationRepository(db).ListByUser(ctx, sharedUserID)
		if err != nil {
			t.Fatalf("list notifications: %v", err)
		}
		wantNotifications := []uuid.UUID{f.notificationA, f.notifB, f.systemNotification}
		if strict {
			// Not empty: the system notification survives, and deliberately.
			// notifications.project_id is nullable, so the scope is a LEFT JOIN
			// with an IS NULL rather than an inner join — an inner one would
			// erase installation-wide messages, which belong to everybody
			// rather than to nobody. The two tenants' notifications go; this
			// one stays.
			wantNotifications = []uuid.UUID{f.systemNotification}
		}
		assertSameIDs(t, idsOf(notifications, func(m models.NotificationModel) uuid.UUID { return uuid.UUID(m.ID) }),
			wantNotifications)

		// The bell's count is scoped as the list is, in both cases. Every
		// fixture notification is unread, so it counts what the list shows.
		unread, err := pg.NewNotificationRepository(db).CountUnreadByUser(ctx, sharedUserID)
		if err != nil {
			t.Fatalf("count unread notifications: %v", err)
		}
		if unread != int64(len(wantNotifications)) {
			t.Errorf("counted %d unread notifications; the list shows %d", unread, len(wantNotifications))
		}

		// And the page the bell opens holds the same notifications.
		page, err := pg.NewNotificationRepository(db).
			ListByUserPaged(ctx, sharedUserID, contracts.Pagination{})
		if err != nil {
			t.Fatalf("page notifications: %v", err)
		}
		assertSameIDs(t, idsOf(page.Items, func(m models.NotificationModel) uuid.UUID { return uuid.UUID(m.ID) }),
			wantNotifications)
		if page.Total != int64(len(wantNotifications)) {
			t.Errorf("the page says there are %d notifications; the list shows %d", page.Total, len(wantNotifications))
		}
	})
}

// TestTenantIsolation_UnreadCountIsScopedAsTheListIs counts, as organization A,
// the notifications the list shows A: its own and the system message, and not
// B's, which is addressed to the same user id. Every fixture notification is
// unread.
func TestTenantIsolation_UnreadCountIsScopedAsTheListIs(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *db.Conn) {
		f := seedTenantFixture(t, db)
		unread, err := pg.NewNotificationRepository(db).CountUnreadByUser(f.ctxAsA(t), sharedUserID)
		if err != nil {
			t.Fatalf("count unread notifications: %v", err)
		}
		if unread != 2 {
			t.Errorf("as A, counted %d unread notifications; A's own and the system message make 2", unread)
		}
	})
}

// idsOf projects a result set down to the IDs the assertions compare.
func idsOf[T any](rows []T, id func(T) uuid.UUID) []uuid.UUID {
	if len(rows) == 0 {
		return nil
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		out = append(out, id(row))
	}
	return out
}

// assertSameIDs compares two ID sets without depending on row order.
func assertSameIDs(t *testing.T, got, want []uuid.UUID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d rows %v, want %d %v", len(got), got, len(want), want)
	}
	seen := make(map[uuid.UUID]struct{}, len(got))
	for _, id := range got {
		seen[id] = struct{}{}
	}
	for _, id := range want {
		if _, ok := seen[id]; !ok {
			t.Fatalf("missing %v in %v", id, got)
		}
	}
}

// isNotFound reports the answer a repository gives a row it will not show:
// apierr.ErrNotFound, whether the row is missing or another tenant's.
func isNotFound(err error) bool {
	return errors.Is(err, apierr.ErrNotFound)
}
