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

	"github.com/gsoultan/metis/internal/pkg/features"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
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
func forEachDialect(t *testing.T, body func(t *testing.T, db *gorm.DB)) {
	t.Helper()

	// One engine. This ran each case against SQLite and then PostgreSQL, back
	// when both were supported and the SQLite pass was the only one that ever
	// ran — the PostgreSQL DSN was set nowhere. Both helpers now open the same
	// engine, so running twice would assert the same thing twice.
	engines := []struct {
		name string
		open func(*testing.T) *gorm.DB
	}{
		{"postgres", func(t *testing.T) *gorm.DB { return testutils.SetupPostgresDB(t, testMaxConns) }},
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
func seedTenantFixture(t *testing.T, db *gorm.DB) tenantFixture {
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

	id := func(v uuid.UUID) models.Base { return models.Base{ID: models.FromUUID(v)} }

	seed := []any{
		&models.OrganizationModel{Base: id(f.orgA), Name: "Org A"},
		&models.OrganizationModel{Base: id(f.orgB), Name: "Org B"},
		&models.ProjectModel{Base: id(f.projectA), OrganizationID: models.FromUUID(f.orgA), Name: "Project A"},
		&models.ProjectModel{Base: id(f.projectB), OrganizationID: models.FromUUID(f.orgB), Name: "Project B"},
		&models.Connector{Base: id(f.connectorID), Key: sharedConnKey, Name: "Shared template"},

		&models.AuditModel{Base: id(f.auditA), ProjectID: models.FromUUID(f.projectA), InstanceID: models.FromUUID(f.instanceA), Message: "a"},
		&models.AuditModel{Base: id(f.auditB), ProjectID: models.FromUUID(f.projectB), InstanceID: models.FromUUID(f.instanceB), Message: "b"},

		&models.FormModel{Base: id(f.formA), ProjectID: models.FromUUID(f.projectA), Key: sharedFormKey},
		&models.FormModel{Base: id(f.formB), ProjectID: models.FromUUID(f.projectB), Key: sharedFormKey},

		&models.DeploymentModel{Base: id(f.deploymentA), ProjectID: models.FromUUID(f.projectA), Name: "dep A"},
		&models.DeploymentModel{Base: id(f.deploymentB), ProjectID: models.FromUUID(f.projectB), Name: "dep B"},
		&models.ResourceModel{Base: id(f.resourceA), DeploymentID: models.FromUUID(f.deploymentA), Name: "res A"},
		&models.ResourceModel{Base: id(f.resourceB), DeploymentID: models.FromUUID(f.deploymentB), Name: "res B"},

		&models.Subscription{Base: id(f.subscriptionA), ProjectID: models.FromUUID(f.projectA), InstanceID: models.FromUUID(f.instanceA), Type: models.SubscriptionSignal, EventName: sharedSignal},
		&models.Subscription{Base: id(f.subB), ProjectID: models.FromUUID(f.projectB), InstanceID: models.FromUUID(f.instanceB), Type: models.SubscriptionSignal, EventName: sharedSignal},

		&models.ExternalTaskModel{Base: id(f.externalTaskA), ProjectID: models.FromUUID(f.projectA), ProcessInstanceID: models.FromUUID(f.instanceA), Topic: sharedTopic, Retries: 3},
		&models.ExternalTaskModel{Base: id(f.extB), ProjectID: models.FromUUID(f.projectB), ProcessInstanceID: models.FromUUID(f.instanceB), Topic: sharedTopic, Retries: 3},

		&models.ConnectorInstance{Base: id(f.connectorInstA), ProjectID: models.FromUUID(f.projectA), ConnectorID: models.FromUUID(f.connectorID), Name: "conn A"},
		&models.ConnectorInstance{Base: id(f.connInstB), ProjectID: models.FromUUID(f.projectB), ConnectorID: models.FromUUID(f.connectorID), Name: "conn B"},

		// Definitions and decisions share a key across organizations, which
		// nothing prevents — key is unique per project, not globally.
		&models.ProcessDefinitionModel{Base: id(f.definitionA), ProjectID: models.FromUUID(f.projectA), Key: sharedDefinitionKey, Name: "def A", Version: 1},
		&models.ProcessDefinitionModel{Base: id(f.definitionB), ProjectID: models.FromUUID(f.projectB), Key: sharedDefinitionKey, Name: "def B", Version: 2},
		&models.DecisionDefinitionModel{Base: id(f.decisionA), ProjectID: models.FromUUID(f.projectA), Key: sharedDecisionKey, Name: "dec A", Version: 1},
		&models.DecisionDefinitionModel{Base: id(f.decisionB), ProjectID: models.FromUUID(f.projectB), Key: sharedDecisionKey, Name: "dec B", Version: 2},
		// Each is its key's live version, so a live lookup has an answer in
		// both organizations to get wrong.
		&models.DecisionReleaseModel{Base: id(uuid.New()), ProjectID: models.FromUUID(f.projectA), DecisionKey: sharedDecisionKey, Version: 1, ActivateAt: time.Now().Add(-time.Hour)},
		&models.DecisionReleaseModel{Base: id(uuid.New()), ProjectID: models.FromUUID(f.projectB), DecisionKey: sharedDecisionKey, Version: 2, ActivateAt: time.Now().Add(-time.Hour)},

		&models.ProcessInstanceModel{Base: id(f.instanceA), ProjectID: models.FromUUID(f.projectA), DefinitionID: models.FromUUID(f.definitionA), Status: models.ProcessActive},
		&models.ProcessInstanceModel{Base: id(f.instanceB), ProjectID: models.FromUUID(f.projectB), DefinitionID: models.FromUUID(f.definitionB), Status: models.ProcessActive},

		&models.TaskModel{Base: id(f.taskA), ProjectID: models.FromUUID(f.projectA), InstanceID: models.FromUUID(f.instanceA), Name: "task A", Status: models.TaskUnclaimed},
		&models.TaskModel{Base: id(f.taskB), ProjectID: models.FromUUID(f.projectB), InstanceID: models.FromUUID(f.instanceB), Name: "task B", Status: models.TaskUnclaimed},

		// Both organizations address a notification to the same user id, plus
		// one system notification that belongs to no project at all.
		&models.NotificationModel{Base: id(f.notificationA), UserID: sharedUserID, ProjectID: models.FromUUIDPtr(&f.projectA), Title: "a"},
		&models.NotificationModel{Base: id(f.notifB), UserID: sharedUserID, ProjectID: models.FromUUIDPtr(&f.projectB), Title: "b"},
		&models.NotificationModel{Base: id(f.systemNotification), UserID: sharedUserID, Title: "system"},
	}
	for _, row := range seed {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}
	return f
}

// TestTenantIsolation_ListsExcludeOtherTenants asserts that every list-shaped
// read on a tenant-owned table returns the caller's rows and nothing else, even
// when the other organization holds a row matching the same filter.
func TestTenantIsolation_ListsExcludeOtherTenants(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
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
					rows, err := pg.NewAuditRepository(testutils.StormConn(db)).ListByProject(ctx, f.projectB)
					return idsOf(rows, func(m models.AuditModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "audit of another tenant's instance",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewAuditRepository(testutils.StormConn(db)).ListByInstance(ctx, f.instanceB)
					return idsOf(rows, func(m models.AuditModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "audit of own project",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewAuditRepository(testutils.StormConn(db)).ListByProject(ctx, f.projectA)
					return idsOf(rows, func(m models.AuditModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.auditA},
			},
			{
				name: "forms across all projects",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewFormRepository(testutils.StormConn(db)).ListByProject(ctx, uuid.Nil)
					return idsOf(rows, func(m models.FormModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.formA},
			},
			{
				name: "deployments across all projects",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewDeploymentRepository(testutils.StormConn(db)).ListByProject(ctx, uuid.Nil)
					return idsOf(rows, func(m models.DeploymentModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.deploymentA},
			},
			{
				name: "deployment resources of another tenant's deployment",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewDeploymentRepository(testutils.StormConn(db)).ListResources(ctx, f.deploymentB)
					return idsOf(rows, func(m models.ResourceModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "signal correlation cannot cross tenants",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewSubscriptionRepository(testutils.StormConn(db)).FindSignals(ctx, f.projectB, sharedSignal)
					return idsOf(rows, func(m models.Subscription) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "subscriptions of another tenant's instance",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewSubscriptionRepository(testutils.StormConn(db)).ListByInstance(ctx, f.instanceB)
					return idsOf(rows, func(m models.Subscription) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "external tasks of another tenant's instance",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewExternalTaskRepository(testutils.StormConn(db)).ListByProcessInstance(ctx, f.instanceB)
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
					rows, err := pg.NewExternalTaskRepository(testutils.StormConn(db)).FetchAndLock(ctx, sharedTopic, "worker-a", 10, 30_000)
					return idsOf(rows, func(m *models.ExternalTaskModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.externalTaskA},
			},
			{
				name: "connector instances of another tenant's project",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewConnectorInstanceRepository(testutils.StormConn(db)).ListByProject(ctx, f.projectB)
					return idsOf(rows, func(m models.ConnectorInstance) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "notifications keep system messages and drop the other tenant's",
				read: func() ([]uuid.UUID, error) {
					rows, err := pg.NewNotificationRepository(testutils.StormConn(db)).ListByUser(ctx, sharedUserID)
					return idsOf(rows, func(m models.NotificationModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.notificationA, f.systemNotification},
			},
			// Both organizations hold a decision under the same key; the other
			// one's is the newer version, so a summary that picked the newest
			// row regardless of tenant would answer with it.
			{
				name: "decision keys of another tenant's project",
				read: func() ([]uuid.UUID, error) {
					page, err := pg.NewDecisionRepository(testutils.StormConn(db)).ListLatestByProject(ctx, f.projectB, contracts.Pagination{})
					return idsOf(page.Items, func(m models.DecisionSummaryModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "decision keys across all projects",
				read: func() ([]uuid.UUID, error) {
					page, err := pg.NewDecisionRepository(testutils.StormConn(db)).ListLatestByProject(ctx, uuid.Nil, contracts.Pagination{})
					return idsOf(page.Items, func(m models.DecisionSummaryModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.decisionA},
			},
			{
				name: "a decision search in another tenant's project",
				read: func() ([]uuid.UUID, error) {
					page, err := pg.NewDecisionRepository(testutils.StormConn(db)).ListByProjectPaged(ctx, f.projectB, "dec", contracts.Pagination{})
					return idsOf(page.Items, func(m models.DecisionDefinitionModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "a decision search across all projects",
				read: func() ([]uuid.UUID, error) {
					page, err := pg.NewDecisionRepository(testutils.StormConn(db)).ListByProjectPaged(ctx, uuid.Nil, "dec", contracts.Pagination{})
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
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		tests := []struct {
			name string
			read func() error
		}{
			{"form", func() error { _, err := pg.NewFormRepository(testutils.StormConn(db)).Get(ctx, f.formB); return err }},
			{"form by key", func() error {
				_, err := pg.NewFormRepository(testutils.StormConn(db)).GetByKey(ctx, f.projectB, sharedFormKey)
				return err
			}},
			{"deployment", func() error {
				_, err := pg.NewDeploymentRepository(testutils.StormConn(db)).Get(ctx, f.deploymentB)
				return err
			}},
			{"deployment resource", func() error {
				_, err := pg.NewDeploymentRepository(testutils.StormConn(db)).GetResource(ctx, f.resourceB)
				return err
			}},
			{"external task", func() error {
				_, err := pg.NewExternalTaskRepository(testutils.StormConn(db)).Get(ctx, f.extB)
				return err
			}},
			{"connector instance", func() error {
				_, err := pg.NewConnectorInstanceRepository(testutils.StormConn(db)).Get(ctx, f.connInstB)
				return err
			}},
			{"connector instance by project and connector", func() error {
				_, err := pg.NewConnectorInstanceRepository(testutils.StormConn(db)).GetByProjectAndConnector(ctx, f.projectB, f.connectorID)
				return err
			}},
			{"task", func() error { _, err := pg.NewTaskRepository(testutils.StormConn(db)).Get(ctx, f.taskB); return err }},
			{"process instance", func() error {
				_, err := pg.NewProcessRepository(testutils.StormConn(db)).Get(ctx, f.instanceB)
				return err
			}},
			{"process instance for update", func() error {
				_, err := pg.NewProcessRepository(testutils.StormConn(db)).GetForUpdate(ctx, f.instanceB)
				return err
			}},
			{"process definition", func() error {
				_, err := pg.NewDefinitionRepository(testutils.StormConn(db)).Get(ctx, f.definitionB)
				return err
			}},
			{"decision", func() error {
				_, err := pg.NewDecisionRepository(testutils.StormConn(db)).Get(ctx, f.decisionB)
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
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		t.Run("definition by key resolves to own project", func(t *testing.T) {
			// B's row has the higher version, so an unscoped "latest wins"
			// lookup returns B.
			got, err := pg.NewDefinitionRepository(testutils.StormConn(db)).GetByKey(ctx, sharedDefinitionKey)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if uuid.UUID(got.ID) != f.definitionA {
				t.Fatalf("got definition %v, want own %v", uuid.UUID(got.ID), f.definitionA)
			}
		})

		t.Run("definition by key and version denies another tenant's version", func(t *testing.T) {
			_, err := pg.NewDefinitionRepository(testutils.StormConn(db)).GetByKeyAndVersion(ctx, sharedDefinitionKey, 2)
			if !isNotFound(err) {
				t.Fatalf("got %v, want a not-found", err)
			}
		})

		t.Run("live decision by key resolves to own project", func(t *testing.T) {
			got, err := pg.NewDecisionRepository(testutils.StormConn(db)).GetLiveByKey(ctx, f.projectA, sharedDecisionKey)
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
			_, err := pg.NewDecisionRepository(testutils.StormConn(db)).GetLiveByKey(ctx, f.projectB, sharedDecisionKey)
			if !isNotFound(err) {
				t.Fatalf("got %v, want a not-found", err)
			}
			_, err = pg.NewDecisionRepository(testutils.StormConn(db)).GetRelease(ctx, f.projectB, sharedDecisionKey)
			if !isNotFound(err) {
				t.Fatalf("release: got %v, want a not-found", err)
			}
		})

		// Naming the other tenant's project outright is no way round the scope.
		t.Run("decision by key and version denies another tenant's version", func(t *testing.T) {
			_, err := pg.NewDecisionRepository(testutils.StormConn(db)).GetByKeyAndVersion(ctx, f.projectB, sharedDecisionKey, 2)
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
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
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
				write: func() error { return pg.NewFormRepository(testutils.StormConn(db)).Delete(ctx, f.formB) },
				unchanged: func() bool {
					return rowExists(t, db, &models.FormModel{}, "forms", f.formB)
				},
			},
			{
				name:  "delete another tenant's definition",
				write: func() error { return pg.NewDefinitionRepository(testutils.StormConn(db)).Delete(ctx, f.definitionB) },
				unchanged: func() bool {
					return rowExists(t, db, &models.ProcessDefinitionModel{}, "process_definitions", f.definitionB)
				},
			},
			{
				name:  "delete another tenant's decision",
				write: func() error { return pg.NewDecisionRepository(testutils.StormConn(db)).Delete(ctx, f.decisionB) },
				unchanged: func() bool {
					return rowExists(t, db, &models.DecisionDefinitionModel{}, "decision_definitions", f.decisionB)
				},
			},
			{
				name: "delete another tenant's connector instance",
				write: func() error {
					return pg.NewConnectorInstanceRepository(testutils.StormConn(db)).Delete(ctx, f.connInstB)
				},
				unchanged: func() bool {
					return rowExists(t, db, &models.ConnectorInstance{}, "connector_instances", f.connInstB)
				},
			},
			{
				name: "repoint another tenant's connector instance",
				write: func() error {
					return pg.NewConnectorInstanceRepository(testutils.StormConn(db)).Update(ctx, models.ConnectorInstance{
						Base:      models.Base{ID: models.FromUUID(f.connInstB)},
						ProjectID: models.FromUUID(f.projectA),
						Name:      "stolen",
					})
				},
				unchanged: func() bool {
					var m models.ConnectorInstance
					if err := db.First(&m, "id = ?", models.FromUUID(f.connInstB)).Error; err != nil {
						t.Fatalf("reload connector instance: %v", err)
					}
					return m.Name == "conn B" && uuid.UUID(m.ProjectID) == f.projectB
				},
			},
			{
				name:  "delete another tenant's notification",
				write: func() error { return pg.NewNotificationRepository(testutils.StormConn(db)).Delete(ctx, f.notifB) },
				unchanged: func() bool {
					return rowExists(t, db, &models.NotificationModel{}, "notifications", f.notifB)
				},
			},
			{
				name:  "mark another tenant's notification read",
				write: func() error { return pg.NewNotificationRepository(testutils.StormConn(db)).MarkAsRead(ctx, f.notifB) },
				unchanged: func() bool {
					var m models.NotificationModel
					if err := db.First(&m, "id = ?", models.FromUUID(f.notifB)).Error; err != nil {
						t.Fatalf("reload notification: %v", err)
					}
					return !m.IsRead
				},
			},
			{
				name:  "delete another tenant's subscription",
				write: func() error { return pg.NewSubscriptionRepository(testutils.StormConn(db)).Delete(ctx, f.subB) },
				unchanged: func() bool {
					return rowExists(t, db, &models.Subscription{}, "event_subscriptions", f.subB)
				},
			},
			{
				name: "redirect another tenant's correlation key",
				write: func() error {
					return pg.NewSubscriptionRepository(testutils.StormConn(db)).UpdateCorrelationKey(ctx, f.subB, "hijacked")
				},
				unchanged: func() bool {
					var m models.Subscription
					if err := db.First(&m, "id = ?", models.FromUUID(f.subB)).Error; err != nil {
						t.Fatalf("reload subscription: %v", err)
					}
					return m.CorrelationKey != "hijacked"
				},
			},
			{
				name: "resolve another tenant's external task",
				write: func() error {
					return pg.NewExternalTaskRepository(testutils.StormConn(db)).Update(ctx, &models.ExternalTaskModel{
						Base:      models.Base{ID: models.FromUUID(f.extB)},
						ProjectID: models.FromUUID(f.projectA),
						Topic:     "stolen",
					})
				},
				unchanged: func() bool {
					var m models.ExternalTaskModel
					if err := db.First(&m, "id = ?", models.FromUUID(f.extB)).Error; err != nil {
						t.Fatalf("reload external task: %v", err)
					}
					return m.Topic == sharedTopic && uuid.UUID(m.ProjectID) == f.projectB
				},
			},
			{
				name:  "delete another tenant's external task",
				write: func() error { return pg.NewExternalTaskRepository(testutils.StormConn(db)).Delete(ctx, f.extB) },
				unchanged: func() bool {
					return rowExists(t, db, &models.ExternalTaskModel{}, "external_tasks", f.extB)
				},
			},
			{
				name: "move another tenant's task status",
				write: func() error {
					return pg.NewTaskRepository(testutils.StormConn(db)).UpdateStatus(ctx, f.taskB, models.TaskCompleted)
				},
				unchanged: func() bool {
					var m models.TaskModel
					if err := db.First(&m, "id = ?", models.FromUUID(f.taskB)).Error; err != nil {
						t.Fatalf("reload task: %v", err)
					}
					return m.Status == models.TaskUnclaimed
				},
			},
			{
				name: "rewrite another tenant's task",
				write: func() error {
					return pg.NewTaskRepository(testutils.StormConn(db)).Update(ctx, models.TaskModel{
						Base:      models.Base{ID: models.FromUUID(f.taskB)},
						ProjectID: models.FromUUID(f.projectA),
						Name:      "stolen",
					})
				},
				unchanged: func() bool {
					var m models.TaskModel
					if err := db.First(&m, "id = ?", models.FromUUID(f.taskB)).Error; err != nil {
						t.Fatalf("reload task: %v", err)
					}
					return m.Name == "task B" && uuid.UUID(m.ProjectID) == f.projectB
				},
			},
			{
				name: "rewrite another tenant's process instance",
				write: func() error {
					return pg.NewProcessRepository(testutils.StormConn(db)).Update(ctx, models.ProcessInstanceModel{
						Base:      models.Base{ID: models.FromUUID(f.instanceB)},
						ProjectID: models.FromUUID(f.projectA),
						Status:    models.ProcessFailed,
					})
				},
				unchanged: func() bool {
					var m models.ProcessInstanceModel
					if err := db.First(&m, "id = ?", models.FromUUID(f.instanceB)).Error; err != nil {
						t.Fatalf("reload instance: %v", err)
					}
					return m.Status == models.ProcessActive && uuid.UUID(m.ProjectID) == f.projectB
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
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		writes := []struct {
			name  string
			write func() error
		}{
			{"mark own notification read", func() error {
				return pg.NewNotificationRepository(testutils.StormConn(db)).MarkAsRead(ctx, f.notificationA)
			}},
			{"mark a system notification read", func() error {
				return pg.NewNotificationRepository(testutils.StormConn(db)).MarkAsRead(ctx, f.systemNotification)
			}},
			{"mark whole inbox read", func() error {
				return pg.NewNotificationRepository(testutils.StormConn(db)).MarkAllAsRead(ctx, sharedUserID)
			}},
			{"own task status", func() error {
				return pg.NewTaskRepository(testutils.StormConn(db)).UpdateStatus(ctx, f.taskA, models.TaskClaimed)
			}},
			{"own correlation key", func() error {
				return pg.NewSubscriptionRepository(testutils.StormConn(db)).UpdateCorrelationKey(ctx, f.subscriptionA, "resolved")
			}},
			{"own connector instance", func() error {
				// Load before saving, the way the service layer does. A model
				// built from scratch has a zero CreatedAt, which MySQL rejects
				// in strict mode — that is a Save footgun, not a scope failure.
				repo := pg.NewConnectorInstanceRepository(testutils.StormConn(db))
				m, err := repo.Get(ctx, f.connectorInstA)
				if err != nil {
					return err
				}
				m.Name = "renamed"
				return repo.Update(ctx, m)
			}},
			{"own form deleted", func() error {
				return pg.NewFormRepository(testutils.StormConn(db)).Delete(ctx, f.formA)
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
		var other models.NotificationModel
		if err := db.First(&other, "id = ?", models.FromUUID(f.notifB)).Error; err != nil {
			t.Fatalf("reload other tenant's notification: %v", err)
		}
		if other.IsRead {
			t.Error("MarkAllAsRead crossed into the other tenant's inbox")
		}
	})
}

// TestTenantIsolation_ProjectsAreScoped covers the table every other scope joins
// *through*, and which nothing scoped as a result: unscoped, List returned every
// organization's projects to anyone authenticated.
func TestTenantIsolation_ProjectsAreScoped(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)
		// Projects have moved to storm; the scoping property is the same and
		// is asserted through the same contract.
		repo := pg.NewProjectRepository(testutils.StormConn(db))

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
			if !rowExists(t, db, &models.ProjectModel{}, "projects", f.projectB) {
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
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		foreign := models.FromUUID(f.projectB)
		newID := func() models.Base { return models.Base{ID: models.FromUUID(uuid.New())} }

		tests := []struct {
			name   string
			create func() error
		}{
			{"definition", func() error {
				return pg.NewDefinitionRepository(testutils.StormConn(db)).Create(ctx,
					models.ProcessDefinitionModel{Base: newID(), ProjectID: foreign, Key: "planted"})
			}},
			{"decision", func() error {
				return pg.NewDecisionRepository(testutils.StormConn(db)).Create(ctx,
					models.DecisionDefinitionModel{Base: newID(), ProjectID: foreign, Key: "planted"})
			}},
			{"form", func() error {
				return pg.NewFormRepository(testutils.StormConn(db)).Create(ctx,
					models.FormModel{Base: newID(), ProjectID: foreign, Key: "planted"})
			}},
			{"deployment", func() error {
				return pg.NewDeploymentRepository(testutils.StormConn(db)).Create(ctx,
					models.DeploymentModel{Base: newID(), ProjectID: foreign, Name: "planted"})
			}},
			{"task", func() error {
				return pg.NewTaskRepository(testutils.StormConn(db)).Create(ctx,
					models.TaskModel{Base: newID(), ProjectID: foreign, Name: "planted"})
			}},
			{"process instance", func() error {
				_, err := pg.NewProcessRepository(testutils.StormConn(db)).Create(ctx,
					models.ProcessInstanceModel{Base: newID(), ProjectID: foreign, Status: models.ProcessActive})
				return err
			}},
			{"connector instance", func() error {
				_, err := pg.NewConnectorInstanceRepository(testutils.StormConn(db)).Create(ctx,
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
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		own := models.FromUUID(f.projectA)
		if err := pg.NewFormRepository(testutils.StormConn(db)).Create(ctx, models.FormModel{
			Base: models.Base{ID: models.FromUUID(uuid.New())}, ProjectID: own, Key: "mine",
		}); err != nil {
			t.Errorf("create into own project: %v", err)
		}
		if err := pg.NewTaskRepository(testutils.StormConn(db)).Create(ctx, models.TaskModel{
			Base: models.Base{ID: models.FromUUID(uuid.New())}, ProjectID: own, Name: "mine",
		}); err != nil {
			t.Errorf("create task into own project: %v", err)
		}
	})
}

// rowExists reports whether a row is still present, ignoring the tenant scope —
// these assertions check the database, not the repository.
func rowExists(t *testing.T, db *gorm.DB, model any, table string, id uuid.UUID) bool {
	t.Helper()
	var count int64
	if err := db.Model(model).Where(table+".id = ?", models.FromUUID(id)).Count(&count).Error; err != nil {
		t.Fatalf("existence check on %s: %v", table, err)
	}
	return count == 1
}

// TestTenantIsolation_OwnRowsStillReadable guards the other direction: the scope
// must not hide the caller's own data. A join that returned nothing for everyone
// would satisfy every assertion above.
func TestTenantIsolation_OwnRowsStillReadable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		reads := []struct {
			name string
			read func() error
		}{
			{"form", func() error { _, err := pg.NewFormRepository(testutils.StormConn(db)).Get(ctx, f.formA); return err }},
			{"form by key", func() error {
				_, err := pg.NewFormRepository(testutils.StormConn(db)).GetByKey(ctx, f.projectA, sharedFormKey)
				return err
			}},
			{"deployment", func() error {
				_, err := pg.NewDeploymentRepository(testutils.StormConn(db)).Get(ctx, f.deploymentA)
				return err
			}},
			{"deployment resource", func() error {
				_, err := pg.NewDeploymentRepository(testutils.StormConn(db)).GetResource(ctx, f.resourceA)
				return err
			}},
			{"external task", func() error {
				_, err := pg.NewExternalTaskRepository(testutils.StormConn(db)).Get(ctx, f.externalTaskA)
				return err
			}},
			{"connector instance", func() error {
				_, err := pg.NewConnectorInstanceRepository(testutils.StormConn(db)).Get(ctx, f.connectorInstA)
				return err
			}},
			{"connector instance by project and connector", func() error {
				_, err := pg.NewConnectorInstanceRepository(testutils.StormConn(db)).GetByProjectAndConnector(ctx, f.projectA, f.connectorID)
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
			rows, err := pg.NewDeploymentRepository(testutils.StormConn(db)).ListResources(ctx, f.deploymentA)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			assertSameIDs(t, idsOf(rows, func(m models.ResourceModel) uuid.UUID { return uuid.UUID(m.ID) }), []uuid.UUID{f.resourceA})
		})

		t.Run("own signals", func(t *testing.T) {
			rows, err := pg.NewSubscriptionRepository(testutils.StormConn(db)).FindSignals(ctx, f.projectA, sharedSignal)
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

	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		ctx := t.Context()

		forms, err := pg.NewFormRepository(testutils.StormConn(db)).ListByProject(ctx, uuid.Nil)
		if err != nil {
			t.Fatalf("list forms: %v", err)
		}
		wantForms := []uuid.UUID{f.formA, f.formB}
		if strict {
			wantForms = nil
		}
		assertSameIDs(t, idsOf(forms, func(m models.FormModel) uuid.UUID { return uuid.UUID(m.ID) }), wantForms)

		notifications, err := pg.NewNotificationRepository(testutils.StormConn(db)).ListByUser(ctx, sharedUserID)
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

// isNotFound accepts either sentinel a repository may answer a denial with.
//
// The GORM repositories return gorm.ErrRecordNotFound; the storm ones return
// apierr.ErrNotFound, which is what every repository will return once the port
// finishes — a persistence contract that leaks its ORM's sentinel is one of the
// things the port removes. Accepting both is temporary and deliberate: the
// property under test is that the row is denied, not which package named it.
func isNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, apierr.ErrNotFound)
}
