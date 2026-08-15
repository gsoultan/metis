// Package tenant holds the cross-tenant isolation tests: proof that a caller
// authenticated into one organization cannot read another organization's rows,
// on every SQL engine the product supports.
//
// These run against SQLite always, and against PostgreSQL and MySQL when
// GOBPM_TEST_POSTGRES_DSN / GOBPM_TEST_MYSQL_DSN are set. The scope is built
// from SQL joins, so the dialect is part of what needs proving — SQLite
// accepting a join says nothing about how MySQL resolves the same column names.
package tenant

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories/gorms"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"github.com/gsoultan/gobpm/tests/testutils"
	"gorm.io/gorm"
)

const (
	sharedUserID  = "user-in-both-inboxes"
	sharedTopic   = "shared-topic"
	sharedSignal  = "order.cancelled"
	sharedFormKey = "expense-form"
	sharedConnKey = "connector-template"

	// testMaxConns keeps the server-backed pools small; these tests are
	// sequential and a wide pool only slows the schema setup down.
	testMaxConns = 2
)

// forEachDialect runs body against every SQL engine the product supports.
// PostgreSQL and MySQL skip themselves unless their DSN is configured.
func forEachDialect(t *testing.T, body func(t *testing.T, db *gorm.DB)) {
	t.Helper()

	engines := []struct {
		name string
		open func(*testing.T) *gorm.DB
	}{
		{"sqlite", func(t *testing.T) *gorm.DB { return testutils.SetupTestDB(t) }},
		{"postgres", func(t *testing.T) *gorm.DB { return testutils.SetupPostgresDB(t, testMaxConns) }},
		{"mysql", func(t *testing.T) *gorm.DB { return testutils.SetupMySQLDB(t, testMaxConns) }},
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

		&models.ExternalTaskModel{Base: id(f.externalTaskA), ProjectID: models.FromUUID(f.projectA), ProcessInstanceID: models.FromUUID(f.instanceA), Topic: sharedTopic},
		&models.ExternalTaskModel{Base: id(f.extB), ProjectID: models.FromUUID(f.projectB), ProcessInstanceID: models.FromUUID(f.instanceB), Topic: sharedTopic},

		&models.ConnectorInstance{Base: id(f.connectorInstA), ProjectID: models.FromUUID(f.projectA), ConnectorID: models.FromUUID(f.connectorID), Name: "conn A"},
		&models.ConnectorInstance{Base: id(f.connInstB), ProjectID: models.FromUUID(f.projectB), ConnectorID: models.FromUUID(f.connectorID), Name: "conn B"},

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
					rows, err := gorms.NewAuditRepository(db).ListByProject(ctx, f.projectB)
					return idsOf(rows, func(m models.AuditModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "audit of another tenant's instance",
				read: func() ([]uuid.UUID, error) {
					rows, err := gorms.NewAuditRepository(db).ListByInstance(ctx, f.instanceB)
					return idsOf(rows, func(m models.AuditModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "audit of own project",
				read: func() ([]uuid.UUID, error) {
					rows, err := gorms.NewAuditRepository(db).ListByProject(ctx, f.projectA)
					return idsOf(rows, func(m models.AuditModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.auditA},
			},
			{
				name: "forms across all projects",
				read: func() ([]uuid.UUID, error) {
					rows, err := gorms.NewFormRepository(db).ListByProject(ctx, uuid.Nil)
					return idsOf(rows, func(m models.FormModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.formA},
			},
			{
				name: "deployments across all projects",
				read: func() ([]uuid.UUID, error) {
					rows, err := gorms.NewDeploymentRepository(db).ListByProject(ctx, uuid.Nil)
					return idsOf(rows, func(m models.DeploymentModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.deploymentA},
			},
			{
				name: "deployment resources of another tenant's deployment",
				read: func() ([]uuid.UUID, error) {
					rows, err := gorms.NewDeploymentRepository(db).ListResources(ctx, f.deploymentB)
					return idsOf(rows, func(m models.ResourceModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "signal correlation cannot cross tenants",
				read: func() ([]uuid.UUID, error) {
					rows, err := gorms.NewSubscriptionRepository(db).FindSignals(ctx, f.projectB, sharedSignal)
					return idsOf(rows, func(m models.Subscription) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "subscriptions of another tenant's instance",
				read: func() ([]uuid.UUID, error) {
					rows, err := gorms.NewSubscriptionRepository(db).ListByInstance(ctx, f.instanceB)
					return idsOf(rows, func(m models.Subscription) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "external tasks of another tenant's instance",
				read: func() ([]uuid.UUID, error) {
					rows, err := gorms.NewExternalTaskRepository(db).ListByProcessInstance(ctx, f.instanceB)
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
					rows, err := gorms.NewExternalTaskRepository(db).FetchAndLock(ctx, sharedTopic, "worker-a", 10, 30_000)
					return idsOf(rows, func(m *models.ExternalTaskModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.externalTaskA},
			},
			{
				name: "connector instances of another tenant's project",
				read: func() ([]uuid.UUID, error) {
					rows, err := gorms.NewConnectorInstanceRepository(db).ListByProject(ctx, f.projectB)
					return idsOf(rows, func(m models.ConnectorInstance) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: nil,
			},
			{
				name: "notifications keep system messages and drop the other tenant's",
				read: func() ([]uuid.UUID, error) {
					rows, err := gorms.NewNotificationRepository(db).ListByUser(ctx, sharedUserID)
					return idsOf(rows, func(m models.NotificationModel) uuid.UUID { return uuid.UUID(m.ID) }), err
				},
				want: []uuid.UUID{f.notificationA, f.systemNotification},
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
			{"form", func() error { _, err := gorms.NewFormRepository(db).Get(ctx, f.formB); return err }},
			{"form by key", func() error {
				_, err := gorms.NewFormRepository(db).GetByKey(ctx, f.projectB, sharedFormKey)
				return err
			}},
			{"deployment", func() error { _, err := gorms.NewDeploymentRepository(db).Get(ctx, f.deploymentB); return err }},
			{"deployment resource", func() error {
				_, err := gorms.NewDeploymentRepository(db).GetResource(ctx, f.resourceB)
				return err
			}},
			{"external task", func() error { _, err := gorms.NewExternalTaskRepository(db).Get(ctx, f.extB); return err }},
			{"connector instance", func() error {
				_, err := gorms.NewConnectorInstanceRepository(db).Get(ctx, f.connInstB)
				return err
			}},
			{"connector instance by project and connector", func() error {
				_, err := gorms.NewConnectorInstanceRepository(db).GetByProjectAndConnector(ctx, f.projectB, f.connectorID)
				return err
			}},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.read()
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatalf("reading another tenant's row: got err %v, want %v", err, gorm.ErrRecordNotFound)
				}
			})
		}
	})
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
			{"form", func() error { _, err := gorms.NewFormRepository(db).Get(ctx, f.formA); return err }},
			{"form by key", func() error {
				_, err := gorms.NewFormRepository(db).GetByKey(ctx, f.projectA, sharedFormKey)
				return err
			}},
			{"deployment", func() error { _, err := gorms.NewDeploymentRepository(db).Get(ctx, f.deploymentA); return err }},
			{"deployment resource", func() error {
				_, err := gorms.NewDeploymentRepository(db).GetResource(ctx, f.resourceA)
				return err
			}},
			{"external task", func() error {
				_, err := gorms.NewExternalTaskRepository(db).Get(ctx, f.externalTaskA)
				return err
			}},
			{"connector instance", func() error {
				_, err := gorms.NewConnectorInstanceRepository(db).Get(ctx, f.connectorInstA)
				return err
			}},
			{"connector instance by project and connector", func() error {
				_, err := gorms.NewConnectorInstanceRepository(db).GetByProjectAndConnector(ctx, f.projectA, f.connectorID)
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
			rows, err := gorms.NewDeploymentRepository(db).ListResources(ctx, f.deploymentA)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			assertSameIDs(t, idsOf(rows, func(m models.ResourceModel) uuid.UUID { return uuid.UUID(m.ID) }), []uuid.UUID{f.resourceA})
		})

		t.Run("own signals", func(t *testing.T) {
			rows, err := gorms.NewSubscriptionRepository(db).FindSignals(ctx, f.projectA, sharedSignal)
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
func TestTenantIsolation_NoTenantContextReadsEverything(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		ctx := t.Context()

		forms, err := gorms.NewFormRepository(db).ListByProject(ctx, uuid.Nil)
		if err != nil {
			t.Fatalf("list forms: %v", err)
		}
		assertSameIDs(t, idsOf(forms, func(m models.FormModel) uuid.UUID { return uuid.UUID(m.ID) }),
			[]uuid.UUID{f.formA, f.formB})

		notifications, err := gorms.NewNotificationRepository(db).ListByUser(ctx, sharedUserID)
		if err != nil {
			t.Fatalf("list notifications: %v", err)
		}
		assertSameIDs(t, idsOf(notifications, func(m models.NotificationModel) uuid.UUID { return uuid.UUID(m.ID) }),
			[]uuid.UUID{f.notificationA, f.notifB, f.systemNotification})
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
