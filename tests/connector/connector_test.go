package connector_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// allowLoopbackEgress opts this test into contacting private addresses.
//
// Outbound HTTP is guarded by an SSRF egress policy that blocks loopback,
// link-local and RFC1918 destinations by default, because connector URLs come
// from user-authored process definitions. httptest servers bind to 127.0.0.1,
// so tests must opt in explicitly — exactly as an operator running the engine
// alongside internal services would.
func allowLoopbackEgress(t *testing.T) {
	t.Helper()
	t.Setenv("GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
}

// MockConnectorRepository is a mock for contracts.ConnectorRepository
type MockConnectorRepository struct {
	mock.Mock
}

func (m *MockConnectorRepository) List(ctx context.Context) ([]models.Connector, error) {
	args := m.Called(ctx)
	return args.Get(0).([]models.Connector), args.Error(1)
}

func (m *MockConnectorRepository) Get(ctx context.Context, id uuid.UUID) (models.Connector, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(models.Connector), args.Error(1)
}

func (m *MockConnectorRepository) GetByKey(ctx context.Context, key string) (models.Connector, error) {
	args := m.Called(ctx, key)
	return args.Get(0).(models.Connector), args.Error(1)
}

func (m *MockConnectorRepository) Create(ctx context.Context, connector models.Connector) (models.Connector, error) {
	args := m.Called(ctx, connector)
	return args.Get(0).(models.Connector), args.Error(1)
}

func (m *MockConnectorRepository) Update(ctx context.Context, connector models.Connector) error {
	args := m.Called(ctx, connector)
	return args.Error(0)
}

func (m *MockConnectorRepository) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// MockConnectorInstanceRepository is a mock for contracts.ConnectorInstanceRepository
type MockConnectorInstanceRepository struct {
	mock.Mock
}

func (m *MockConnectorInstanceRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.ConnectorInstance, error) {
	args := m.Called(ctx, projectID)
	return args.Get(0).([]models.ConnectorInstance), args.Error(1)
}

func (m *MockConnectorInstanceRepository) Get(ctx context.Context, id uuid.UUID) (models.ConnectorInstance, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(models.ConnectorInstance), args.Error(1)
}

func (m *MockConnectorInstanceRepository) GetByProjectAndConnector(ctx context.Context, projectID, connectorID uuid.UUID) (models.ConnectorInstance, error) {
	args := m.Called(ctx, projectID, connectorID)
	return args.Get(0).(models.ConnectorInstance), args.Error(1)
}

func (m *MockConnectorInstanceRepository) Create(ctx context.Context, instance models.ConnectorInstance) (models.ConnectorInstance, error) {
	args := m.Called(ctx, instance)
	return args.Get(0).(models.ConnectorInstance), args.Error(1)
}

func (m *MockConnectorInstanceRepository) Update(ctx context.Context, instance models.ConnectorInstance) error {
	args := m.Called(ctx, instance)
	return args.Error(0)
}

func (m *MockConnectorInstanceRepository) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

type MockRepository struct {
	mock.Mock
	connector         *MockConnectorRepository
	connectorInstance *MockConnectorInstanceRepository
}

var _ repositories.Repository = (*MockRepository)(nil)

func (m *MockRepository) Audit() contracts.AuditRepository         { return nil }
func (m *MockRepository) Connector() contracts.ConnectorRepository { return m.connector }
func (m *MockRepository) ConnectorInstance() contracts.ConnectorInstanceRepository {
	return m.connectorInstance
}
func (m *MockRepository) Decision() contracts.DecisionRepository         { return nil }
func (m *MockRepository) Definition() contracts.DefinitionRepository     { return nil }
func (m *MockRepository) Deployment() contracts.DeploymentRepository     { return nil }
func (m *MockRepository) ExternalTask() contracts.ExternalTaskRepository { return nil }
func (m *MockRepository) Form() contracts.FormRepository                 { return nil }
func (m *MockRepository) Incident() contracts.IncidentRepository         { return nil }
func (m *MockRepository) Job() contracts.JobRepository                   { return nil }
func (m *MockRepository) Organization() contracts.OrganizationRepository { return nil }
func (m *MockRepository) Process() contracts.ProcessRepository           { return nil }
func (m *MockRepository) ServiceCall() contracts.ServiceCallRepository   { return nil }
func (m *MockRepository) Webhook() contracts.WebhookRepository           { return nil }
func (m *MockRepository) ConnectorManifest() contracts.ConnectorManifestRepository {
	return nil
}
func (m *MockRepository) Project() contracts.ProjectRepository           { return nil }
func (m *MockRepository) Subscription() contracts.SubscriptionRepository { return nil }
func (m *MockRepository) Task() contracts.TaskRepository                 { return nil }
func (m *MockRepository) User() contracts.UserRepository                 { return nil }
func (m *MockRepository) Group() contracts.GroupRepository               { return nil }
func (m *MockRepository) Notification() contracts.NotificationRepository { return nil }
func (m *MockRepository) CompensatableActivity() contracts.CompensatableActivityRepository {
	return nil
}
func (m *MockRepository) VariableSnapshot() contracts.VariableSnapshotRepository { return nil }
func (m *MockRepository) UnitOfWork() contracts.UnitOfWork                       { return nil }

func TestHttpJsonExecutor(t *testing.T) {
	allowLoopbackEgress(t)
	// CreateAuditEntry a test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "secret-token", r.Header.Get("X-Auth-Token"))
		fmt.Fprint(w, `{"status": "success", "data": "test-data"}`)
	}))
	defer ts.Close()

	executor := &impl.HttpJsonExecutor{}
	ctx := context.Background()
	config := map[string]any{
		"url":     ts.URL,
		"method":  "POST",
		"headers": `{"X-Auth-Token": "secret-token"}`,
	}
	payload := map[string]any{
		"message": "hello world",
	}

	result, err := executor.Execute(ctx, config, payload)
	assert.NoError(t, err)
	assert.Equal(t, "success", result["status"])
	assert.Equal(t, "test-data", result["data"])
}

func TestSlackMessageExecutor(t *testing.T) {
	allowLoopbackEgress(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer ts.Close()

	executor := &impl.SlackMessageExecutor{}
	ctx := context.Background()
	config := map[string]any{
		"webhook_url": ts.URL,
	}
	payload := map[string]any{
		"text": "test slack message",
	}

	result, err := executor.Execute(ctx, config, payload)
	assert.NoError(t, err)
	assert.Equal(t, "ok", result["status"])
}

func TestConnectorService(t *testing.T) {
	allowLoopbackEgress(t)
	mockConnectorRepo := new(MockConnectorRepository)
	mockInstanceRepo := new(MockConnectorInstanceRepository)
	mockRepo := &MockRepository{
		connector:         mockConnectorRepo,
		connectorInstance: mockInstanceRepo,
	}

	// Skip bootstrapping for simplicity in this test
	mockConnectorRepo.On("GetByKey", mock.Anything, mock.Anything).Return(models.Connector{}, fmt.Errorf("not found"))
	mockConnectorRepo.On("Create", mock.Anything, mock.Anything).Return(models.Connector{}, nil)

	service := impl.NewConnectorService(mockRepo)
	assert.NotNil(t, service)

	t.Run("ExecuteConnector", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ok")
		}))
		defer ts.Close()

		config := map[string]any{"webhook_url": ts.URL}
		payload := map[string]any{"text": "test"}

		result, err := service.ExecuteConnector(context.Background(), "slack-message", config, payload)
		assert.NoError(t, err)
		assert.Equal(t, "ok", result["status"])
	})

	t.Run("ExecuteConnector_NotFound", func(t *testing.T) {
		_, err := service.ExecuteConnector(context.Background(), "non-existent", nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no executor found")
	})
}
