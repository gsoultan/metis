package impl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/httpclient"
	"github.com/gsoultan/metis/internal/pkg/tracing"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
	"github.com/gsoultan/metis/server/domains/services/impl/sqlconnector"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"
)

type connectorService struct {
	repo      repositories.Repository
	executors map[string]servicecontracts.ConnectorExecutor
}

// connectorService still satisfies the contracts; asserted here because the
// constructor now returns the concrete type and would no longer catch a drift.
var _ servicecontracts.JobConnectorService = (*connectorService)(nil)

// InstallManifest registers a connector described by a document.
//
// Validated here rather than at call time: a manifest is installed once and
// called thousands of times, and the moment to discover it names no URL is when
// somebody installs it, not when an instance reaches it at 3am.
//
// Installing an existing key replaces it, because installing again is how an
// author fixes a manifest.
func (s *connectorService) InstallManifest(ctx context.Context, document []byte) (entities.ConnectorManifest, error) {
	manifest, err := connectors.ParseManifest(document)
	if err != nil {
		return entities.ConnectorManifest{}, err
	}

	m := models.ConnectorManifestModel{
		Key:      manifest.Key,
		Name:     manifest.Name,
		Version:  manifest.Version,
		Document: string(document),
		Enabled:  true,
	}
	if err := s.repo.ConnectorManifest().Upsert(ctx, m); err != nil {
		return entities.ConnectorManifest{}, err
	}

	// Read back rather than returning what was sent. Installing an existing key
	// keeps that row's id, so the id the caller needs — to switch this
	// connector off, or delete it — is the stored one and not the one this
	// function might have generated.
	stored, err := s.repo.ConnectorManifest().GetByKey(ctx, manifest.Key)
	if err != nil {
		return entities.ConnectorManifest{}, err
	}
	return entities.ConnectorManifest{
		ID:      uuid.UUID(stored.ID),
		Key:     stored.Key,
		Name:    stored.Name,
		Version: stored.Version,
		Enabled: stored.Enabled,
	}, nil
}

// ListManifests returns the installed manifests, without their documents.
func (s *connectorService) ListManifests(ctx context.Context) ([]entities.ConnectorManifest, error) {
	list, err := s.repo.ConnectorManifest().List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]entities.ConnectorManifest, len(list))
	for i, m := range list {
		out[i] = entities.ConnectorManifest{
			ID: uuid.UUID(m.ID), Key: m.Key, Name: m.Name, Version: m.Version, Enabled: m.Enabled,
		}
	}
	return out, nil
}

// GetManifestDocument returns a manifest exactly as its author wrote it.
func (s *connectorService) GetManifestDocument(ctx context.Context, key string) (string, error) {
	m, err := s.repo.ConnectorManifest().GetByKey(ctx, key)
	if err != nil {
		return "", err
	}
	return m.Document, nil
}

func (s *connectorService) SetManifestEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	return s.repo.ConnectorManifest().SetEnabled(ctx, id, enabled)
}

func (s *connectorService) DeleteManifest(ctx context.Context, id uuid.UUID) error {
	return s.repo.ConnectorManifest().Delete(ctx, id)
}

// ImportOpenAPI turns a specification into manifests and installs every one.
//
// All or nothing would be the wrong shape here: a document of forty operations
// with one this importer cannot read should yield thirty-nine connectors, not a
// refusal. The count of what was installed is the answer.
func (s *connectorService) ImportOpenAPI(ctx context.Context, document []byte) ([]entities.ConnectorManifest, error) {
	manifests, err := connectors.ImportOpenAPI(document)
	if err != nil {
		return nil, err
	}

	installed := make([]entities.ConnectorManifest, 0, len(manifests))
	for _, manifest := range manifests {
		encoded, marshalErr := yaml.Marshal(manifest)
		if marshalErr != nil {
			return nil, fmt.Errorf("could not write the generated manifest for %q: %w", manifest.Key, marshalErr)
		}
		one, installErr := s.InstallManifest(ctx, encoded)
		if installErr != nil {
			return nil, installErr
		}
		installed = append(installed, one)
	}
	return installed, nil
}

// NewConnectorService returns the concrete type, not the interface, so the
// composition root can hand the job service the request runner as well as the
// ConnectorService every other consumer gets — without the facade, which embeds
// ConnectorService, gaining it too. The same reason NewDefinitionService returns
// its concrete type.
func NewConnectorService(
	repo repositories.Repository,
) *connectorService {
	s := &connectorService{
		repo:      repo,
		executors: make(map[string]servicecontracts.ConnectorExecutor),
	}

	// Register built-in executors, once each.
	//
	// http-json, slack-message and email-smtp name the connectors-package
	// implementations, which are the ones the application runs. Each used to
	// have a second implementation registered here and then overwritten by
	// NewServiceFacade, so every test that built a connector service exercised
	// code the application never ran — and the copies had drifted apart. The
	// http-json copy parsed the headers the Connectors page saves as text; the
	// one that shipped read only an object, so every header configured on that
	// page was dropped, and the one test of headers passed against the copy.
	//
	// NewServiceFacade is the only production caller of this constructor and it
	// registered these same implementations, so a running process reads back
	// exactly what it did before. What changed is which code the tests run.
	s.executors[connectors.HTTPConnectorKey] = connectors.NewHTTPConnector(nil)
	s.executors[connectors.SlackConnectorKey] = connectors.NewSlackConnector()
	s.executors[connectors.EmailConnectorKey] = connectors.NewEmailConnector()
	s.executors[sqlconnector.Key] = sqlconnector.New()
	s.executors["rabbitmq-publish"] = NewRabbitMQExecutor()

	// Discord Connector
	s.executors["discord-message"] = &DiscordMessageExecutor{}
	// SendGrid Connector
	s.executors["sendgrid-email"] = &SendGridEmailExecutor{}
	// MS Teams Connector
	s.executors["ms-teams-message"] = &MSTeamsMessageExecutor{}

	// Seed the catalogue for an already-configured installation. A first run
	// writes into the bootstrap database instead, so setup calls this again
	// once it has swapped to the real one.
	if err := s.EnsureDefaultConnectors(entities.WithSystemContext(context.Background())); err != nil {
		log.Error().Err(err).Msg("Failed to bootstrap default connectors")
	}

	return s
}

func (s *connectorService) ListConnectors(ctx context.Context) ([]entities.Connector, error) {
	ms, err := s.repo.Connector().List(ctx)
	if err != nil {
		return nil, err
	}
	res := make([]entities.Connector, len(ms))
	for i, m := range ms {
		res[i] = withNodeSchema(adapters.ConnectorEntityAdapter{Model: m}.ToEntity())
	}
	return res, nil
}

func (s *connectorService) GetConnector(ctx context.Context, id uuid.UUID) (entities.Connector, error) {
	m, err := s.repo.Connector().Get(ctx, id)
	if err != nil {
		return entities.Connector{}, err
	}
	return withNodeSchema(adapters.ConnectorEntityAdapter{Model: m}.ToEntity()), nil
}

// nodeSchemas are the fields a step fills in, for the built-ins that take a
// step's own request.
//
// Compiled in and attached on read rather than stored beside the connection
// schema. What a step asks for is code — it has to agree with the executor
// that reads it — so a stored copy would be a second definition, and one that
// an installation seeded under an older version would keep.
var nodeSchemas = map[string]func() []entities.ConnectorProperty{
	sqlconnector.Key: sqlconnector.NodeSchema,
}

func withNodeSchema(c entities.Connector) entities.Connector {
	if schema, ok := nodeSchemas[c.Key]; ok {
		c.NodeSchema = schema()
	}
	return c
}

func (s *connectorService) CreateConnector(ctx context.Context, c entities.Connector) (entities.Connector, error) {
	if c.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return entities.Connector{}, fmt.Errorf("could not generate a connector id: %w", err)
		}
		c.ID = id
	}
	c.CreatedAt = time.Now()
	m, err := s.repo.Connector().Create(ctx, adapters.ConnectorModelAdapter{Connector: c}.ToModel())
	if err != nil {
		return entities.Connector{}, err
	}
	return adapters.ConnectorEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *connectorService) UpdateConnector(ctx context.Context, c entities.Connector) error {
	return s.repo.Connector().Update(ctx, adapters.ConnectorModelAdapter{Connector: c}.ToModel())
}

func (s *connectorService) DeleteConnector(ctx context.Context, id uuid.UUID) error {
	return s.repo.Connector().Delete(ctx, id)
}

func (s *connectorService) ListConnectorInstances(ctx context.Context, projectID uuid.UUID) ([]entities.ConnectorInstance, error) {
	ms, err := s.repo.ConnectorInstance().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	res := make([]entities.ConnectorInstance, len(ms))
	for i, m := range ms {
		res[i] = adapters.ConnectorInstanceEntityAdapter{Model: m}.ToEntity()
	}
	s.nameConnectors(ctx, res)
	return res, nil
}

// nameConnectors fills in the connector each instance configures.
//
// A row stores a connector id, so an instance otherwise arrives naming nothing:
// no key, which is what a service task refers to, and no name to show in a
// list. The catalogue is read once for the whole slice rather than per row —
// it is a handful of built-ins, and a query each would be a lot of round trips
// to answer "which Slack is this".
func (s *connectorService) nameConnectors(ctx context.Context, instances []entities.ConnectorInstance) {
	if len(instances) == 0 {
		return
	}
	catalogue, err := s.repo.Connector().List(ctx)
	if err != nil {
		// The instances are still worth returning; they just stay unnamed.
		log.Warn().Err(err).Msg("could not load the connector catalogue to name instances")
		return
	}
	byID := make(map[uuid.UUID]entities.Connector, len(catalogue))
	for _, m := range catalogue {
		c := adapters.ConnectorEntityAdapter{Model: m}.ToEntity()
		byID[c.ID] = c
	}
	for i := range instances {
		if instances[i].Connector == nil {
			continue
		}
		if c, ok := byID[instances[i].Connector.ID]; ok {
			instances[i].Connector = &c
		}
	}
}

func (s *connectorService) GetConnectorInstance(ctx context.Context, id uuid.UUID) (entities.ConnectorInstance, error) {
	m, err := s.repo.ConnectorInstance().Get(ctx, id)
	if err != nil {
		return entities.ConnectorInstance{}, err
	}
	one := []entities.ConnectorInstance{adapters.ConnectorInstanceEntityAdapter{Model: m}.ToEntity()}
	s.nameConnectors(ctx, one)
	return one[0], nil
}

func (s *connectorService) GetConnectorInstanceByProjectAndConnector(ctx context.Context, projectID, connectorID uuid.UUID) (entities.ConnectorInstance, error) {
	m, err := s.repo.ConnectorInstance().GetByProjectAndConnector(ctx, projectID, connectorID)
	if err != nil {
		return entities.ConnectorInstance{}, err
	}
	one := []entities.ConnectorInstance{adapters.ConnectorInstanceEntityAdapter{Model: m}.ToEntity()}
	s.nameConnectors(ctx, one)
	return one[0], nil
}

func (s *connectorService) CreateConnectorInstance(ctx context.Context, instance entities.ConnectorInstance) (entities.ConnectorInstance, error) {
	// An instance belongs to a project and configures a connector, and an
	// instance missing either is unreachable rather than merely incomplete: the
	// only listing is scoped to a project, and execution needs a connector to
	// run. Storing one anyway returns an id for a row nothing can ever find.
	if instance.Project == nil || instance.Project.ID == uuid.Nil {
		return entities.ConnectorInstance{}, fmt.Errorf("connector instance requires a project")
	}
	if instance.Connector == nil || instance.Connector.ID == uuid.Nil {
		return entities.ConnectorInstance{}, fmt.Errorf("connector instance requires a connector")
	}

	if instance.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return entities.ConnectorInstance{}, fmt.Errorf("could not generate a connector instance id: %w", err)
		}
		instance.ID = id
	}
	instance.CreatedAt = time.Now()
	instance.UpdatedAt = time.Now()
	m, err := s.repo.ConnectorInstance().Create(ctx, adapters.ConnectorInstanceModelAdapter{Instance: instance}.ToModel())
	if err != nil {
		return entities.ConnectorInstance{}, err
	}
	return adapters.ConnectorInstanceEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *connectorService) UpdateConnectorInstance(ctx context.Context, instance entities.ConnectorInstance) error {
	instance.UpdatedAt = time.Now()
	return s.repo.ConnectorInstance().Update(ctx, adapters.ConnectorInstanceModelAdapter{Instance: instance}.ToModel())
}

func (s *connectorService) DeleteConnectorInstance(ctx context.Context, id uuid.UUID) error {
	return s.repo.ConnectorInstance().Delete(ctx, id)
}

// manifestFor loads the manifest a connector key names, if one is installed.
//
// A miss is the ordinary case — most connectors are still built in — so it is
// not an error and is not logged. A manifest that is switched off is treated as
// absent, which lets an operator stop a connector without deleting the document
// that defines it.
func (s *connectorService) manifestFor(ctx context.Context, key string) (connectors.Manifest, bool) {
	// A repository that supplies no manifest store has no manifests, which is
	// the same answer as "none installed". Guarded rather than assumed: this is
	// on the path every service task takes, and a nil dereference here kills the
	// job goroutine rather than failing one call — which is exactly what it did
	// the first time a test passed a repository without one.
	store := s.repo.ConnectorManifest()
	if store == nil {
		return connectors.Manifest{}, false
	}

	m, err := store.GetByKey(ctx, key)
	if err != nil || !m.Enabled {
		return connectors.Manifest{}, false
	}

	manifest, err := connectors.ParseManifest([]byte(m.Document))
	if err != nil {
		// Installed manifests are validated on the way in, so this means the
		// row was edited outside the application or the format has moved on.
		// Falling through to the built-ins would silently call the wrong thing.
		log.Error().Err(err).Str("connector", key).
			Msg("An installed connector manifest can no longer be read; reinstall it")
		return connectors.Manifest{}, false
	}
	return manifest, true
}

// ExecuteConnector runs one outbound integration call with the process
// variables as its payload.
//
// It is ExecuteConnectorRequest with nothing but variables, so there is one
// path through a connector call rather than two that can drift apart.
func (s *connectorService) ExecuteConnector(ctx context.Context, connectorKey string, config map[string]any, payload map[string]any) (map[string]any, error) {
	return s.ExecuteConnectorRequest(ctx, connectorKey, config, servicecontracts.ConnectorRequest{Variables: payload})
}

// ExecuteConnectorRequest runs one outbound integration call.
//
// This is the span execution-plan.md §3.4 asks for. It is the boundary where
// this system stops being in control: everything inside is our code, and
// everything past it is somebody else's availability. When an instance has been
// stuck for hours, this span is usually the answer.
//
// The request reaches an executor only if it implements RequestExecutor. Every
// other executor, and every manifest, is called with the variables alone,
// exactly as ExecuteConnector always called it.
func (s *connectorService) ExecuteConnectorRequest(ctx context.Context, connectorKey string, config map[string]any, req servicecontracts.ConnectorRequest) (map[string]any, error) {
	payload := req.Variables
	ctx, span := tracing.Tracer().Start(ctx, "connector.execute",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(tracing.AttrConnectorKey.String(connectorKey)),
	)
	defer span.End()

	// A manifest first: a connector written as data beats one compiled in, so an
	// installed manifest may replace a built-in without editing Go. When none is
	// installed under this key, the built-in executors answer as they always
	// have.
	//
	// Read from the store on every call rather than cached. A cache would have
	// to be invalidated across replicas — install a connector on one and the
	// others would keep calling the old one until they restarted — and this is
	// one indexed read against a function that is about to make a network call.
	if manifest, found := s.manifestFor(ctx, connectorKey); found {
		result, err := connectors.RunManifest(ctx, manifest, config, payload, nil)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "connector call failed")
			return nil, err
		}
		span.SetStatus(codes.Ok, "")
		return result, nil
	}

	executor, ok := s.executors[connectorKey]
	if !ok {
		err := fmt.Errorf("no executor found for connector key: %s", connectorKey)
		span.RecordError(err)
		span.SetStatus(codes.Error, "no executor for connector")
		return nil, err
	}

	result, err := execute(ctx, executor, config, req)
	if err != nil {
		// Recorded rather than merely returned: a connector failure is the most
		// common cause of a stalled instance, and a trace that shows the call
		// happening but not that it failed sends the reader looking elsewhere.
		span.RecordError(err)
		span.SetStatus(codes.Error, "connector call failed")
		return nil, err
	}
	span.SetStatus(codes.Ok, "")
	return result, nil
}

// execute hands the request to an executor that can take one, and the
// variables to one that cannot.
func execute(ctx context.Context, executor servicecontracts.ConnectorExecutor, config map[string]any, req servicecontracts.ConnectorRequest) (map[string]any, error) {
	if requests, ok := executor.(servicecontracts.RequestExecutor); ok {
		return requests.ExecuteRequest(ctx, config, req)
	}
	return executor.Execute(ctx, config, req.Variables)
}

func (s *connectorService) RegisterExecutor(key string, executor servicecontracts.ConnectorExecutor) {
	s.executors[key] = executor
}

// EnsureDefaultConnectors creates the built-in catalogue in whichever database
// is current, skipping any connector that is already there.
//
// This runs at construction and again after setup swaps to the target
// database. Without the second call the catalogue only ever existed in the
// bootstrap database that setup replaces, so a configured installation offered
// no connectors at all and every service task had nothing to call.
func (s *connectorService) EnsureDefaultConnectors(ctx context.Context) error {
	log.Info().Msg("Bootstrapping default connectors...")

	connectors := []entities.Connector{
		{
			ID:          uuid.MustParse("018e1a1a-1a1a-7a1a-a1a1-1a1a1a1a1a1a"),
			Key:         "http-json",
			Name:        "HTTP JSON Connector",
			Description: "Send a JSON request to an HTTP endpoint",
			Icon:        "Globe",
			Type:        "utility",
			Schema: []entities.ConnectorProperty{
				{Key: "url", Label: "URL", Type: "string", Required: true},
				{Key: "method", Label: "Method", Type: "select", DefaultValue: "POST", Options: []any{"GET", "POST", "PUT", "DELETE", "PATCH"}},
				{Key: "headers", Label: "Headers (JSON)", Type: "string", DefaultValue: "{}"},
			},
		},
		{
			ID:          uuid.MustParse("018e1a1a-1a1a-7a1a-a1a1-1a1a1a1a1a1b"),
			Key:         "slack-message",
			Name:        "Slack Connector",
			Description: "Send a message to a Slack channel via Webhook",
			Icon:        "MessageSquare",
			Type:        "social",
			Schema: []entities.ConnectorProperty{
				{Key: "webhook_url", Label: "Webhook URL", Type: "password", Required: true},
				{Key: "channel", Label: "Default Channel", Type: "string"},
			},
		},
		{
			ID:          uuid.MustParse("018e1a1a-1a1a-7a1a-a1a1-1a1a1a1a1a1d"),
			Key:         "discord-message",
			Name:        "Discord Connector",
			Description: "Send a message to a Discord channel via Webhook",
			Icon:        "MessageSquare",
			Type:        "social",
			Schema: []entities.ConnectorProperty{
				{Key: "webhook_url", Label: "Webhook URL", Type: "password", Required: true},
				{Key: "username", Label: "Bot Username", Type: "string"},
			},
		},
		{
			ID:          uuid.MustParse("018e1a1a-1a1a-7a1a-a1a1-1a1a1a1a1a1e"),
			Key:         "sendgrid-email",
			Name:        "SendGrid Email",
			Description: "Send an email via SendGrid API",
			Icon:        "Mail",
			Type:        "messaging",
			Schema: []entities.ConnectorProperty{
				{Key: "api_key", Label: "SendGrid API Key", Type: "password", Required: true},
				{Key: "from_email", Label: "From Email", Type: "string", Required: true},
				{Key: "from_name", Label: "From Name", Type: "string"},
				{Key: "to_email", Label: "To Email", Type: "string", Required: true},
				{Key: "subject", Label: "Subject", Type: "string", Required: true},
				{Key: "content", Label: "Content", Type: "textarea", Required: true},
			},
		},
		{
			ID:          uuid.MustParse("018e1a1a-1a1a-7a1a-a1a1-1a1a1a1a1a1f"),
			Key:         "ms-teams-message",
			Name:        "MS Teams Connector",
			Description: "Send a message to a Microsoft Teams channel via Webhook",
			Icon:        "Users",
			Type:        "social",
			Schema: []entities.ConnectorProperty{
				{Key: "webhook_url", Label: "Webhook URL", Type: "password", Required: true},
			},
		},
		{
			ID:          uuid.MustParse("018e1a1a-1a1a-7a1a-a1a1-1a1a1a1a1a1c"),
			Key:         "rabbitmq-publish",
			Name:        "RabbitMQ Publisher",
			Description: "Publish a message to a RabbitMQ exchange",
			Icon:        "Send",
			Type:        "messaging",
			Schema: []entities.ConnectorProperty{
				// #nosec G101 -- RabbitMQ's documented default, shown as a form
				// placeholder so somebody knows the shape to type. Not a credential
				// this installation holds.
				{Key: "url", Label: "RabbitMQ URL", Type: "string", Required: true, DefaultValue: "amqp://guest:guest@localhost:5672/"},
				{Key: "exchange", Label: "Exchange", Type: "string", Required: true},
				{Key: "routing_key", Label: "Routing Key", Type: "string"},
				{Key: "queue", Label: "Queue (Direct Publish)", Type: "string"},
			},
		},
		{
			ID:          uuid.MustParse("018e1a1a-1a1a-7a1a-a1a1-1a1a1a1a1a20"),
			Key:         "email-smtp",
			Name:        "SMTP Email",
			Description: "Send an email via SMTP server",
			Icon:        "Mail",
			Type:        "messaging",
			Schema: []entities.ConnectorProperty{
				{Key: "host", Label: "SMTP Host", Type: "string", Required: true},
				{Key: "port", Label: "SMTP Port", Type: "number", Required: true, DefaultValue: "587"},
				{Key: "username", Label: "Username", Type: "string", Required: true},
				{Key: "password", Label: "Password", Type: "password", Required: true},
				{Key: "from", Label: "From Email", Type: "string", Required: true},
			},
		},
		sqlconnector.CatalogueEntry(),
	}

	var failed error
	for _, c := range connectors {
		if _, err := s.repo.Connector().GetByKey(ctx, c.Key); err == nil {
			log.Debug().Str("key", c.Key).Msg("Default connector already exists")
			continue
		}
		log.Info().Str("key", c.Key).Msg("Creating default connector")
		c.CreatedAt = time.Now()
		if _, err := s.repo.Connector().Create(ctx, adapters.ConnectorModelAdapter{Connector: c}.ToModel()); err != nil {
			log.Error().Err(err).Str("key", c.Key).Msg("Failed to create default connector")
			failed = err
		}
	}
	return failed
}

// Built-in Executors

// closeResponse closes a response body and says so when it could not.
//
// A failed close means the connection was already gone, which changes nothing
// about the answer already read — but a stream of them is a sign of something
// worth knowing about, and silently dropping the error is how nobody finds out.
func closeResponse(body io.Closer, connector string) {
	if err := body.Close(); err != nil {
		log.Debug().Err(err).Str("connector", connector).Msg("Could not close the connector response")
	}
}

// readResponse reads a response body, refusing a partial one.
//
// A truncated read used to be discarded, so a connection that dropped halfway
// through a reply produced an empty result and a *successful* service task. The
// process then carried on with variables the partner never sent.
func readResponse(resp *http.Response, connector string) ([]byte, error) {
	raw, err := httpclient.ReadResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: the reply could not be read in full: %w", connector, err)
	}
	return raw, nil
}

type DiscordMessageExecutor struct{}

func (e *DiscordMessageExecutor) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	webhookURL, _ := connectors.TextSetting(config, "webhook_url")
	// "text" is the alternative spelling of "content". Reading it with a
	// single-value assertion panicked whenever it was absent — which is every
	// payload that spells it "content" — and whenever a mapping produced a
	// number rather than a string.
	content, _ := connectors.TextSetting(payload, "content")
	if content == "" {
		content, _ = connectors.TextSetting(payload, "text")
	}
	if content == "" {
		content = "No content provided"
	}

	discordPayload := map[string]any{
		"content": content,
	}
	if username, ok := config["username"].(string); ok && username != "" {
		discordPayload["username"] = username
	}

	body, err := json.Marshal(discordPayload)
	if err != nil {
		return nil, fmt.Errorf("discord-message: could not encode the request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpclient.Shared().Do(req)
	if err != nil {
		return nil, err
	}
	defer closeResponse(resp.Body, "discord-message")

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("discord-message: the Discord API returned %s", resp.Status)
	}

	return map[string]any{"status": "sent"}, nil
}

type SendGridEmailExecutor struct{}

func (e *SendGridEmailExecutor) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	apiKey, _ := connectors.TextSetting(config, "api_key")
	fromEmail, _ := connectors.TextSetting(config, "from_email")
	fromName, _ := connectors.TextSetting(config, "from_name")

	toEmail, _ := connectors.TextSetting(payload, "to_email")
	if toEmail == "" {
		toEmail, _ = connectors.TextSetting(config, "to_email")
	}
	subject, _ := connectors.TextSetting(payload, "subject")
	if subject == "" {
		subject, _ = connectors.TextSetting(config, "subject")
	}
	content, _ := connectors.TextSetting(payload, "content")
	if content == "" {
		content, _ = connectors.TextSetting(config, "content")
	}

	sgPayload := map[string]any{
		"personalizations": []map[string]any{
			{
				"to": []map[string]any{{"email": toEmail}},
			},
		},
		"from":    map[string]any{"email": fromEmail, "name": fromName},
		"subject": subject,
		"content": []map[string]any{
			{"type": "text/plain", "value": content},
		},
	}

	body, err := json.Marshal(sgPayload)
	if err != nil {
		return nil, fmt.Errorf("sendgrid-email: could not encode the request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.sendgrid.com/v3/mail/send", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpclient.Shared().Do(req)
	if err != nil {
		return nil, err
	}
	defer closeResponse(resp.Body, "sendgrid-email")

	if resp.StatusCode >= 400 {
		respBody, readErr := readResponse(resp, "sendgrid-email")
		if readErr != nil {
			return nil, readErr
		}
		return nil, fmt.Errorf("SendGrid API error: %s - %s", resp.Status, string(respBody))
	}

	return map[string]any{"status": "sent"}, nil
}

type MSTeamsMessageExecutor struct{}

func (e *MSTeamsMessageExecutor) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	webhookURL, _ := connectors.TextSetting(config, "webhook_url")
	text, _ := connectors.TextSetting(payload, "text")
	if text == "" {
		text = "No message text provided"
	}

	teamsPayload := map[string]any{
		"text": text,
	}

	body, err := json.Marshal(teamsPayload)
	if err != nil {
		return nil, fmt.Errorf("ms-teams-message: could not encode the request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpclient.Shared().Do(req)
	if err != nil {
		return nil, err
	}
	defer closeResponse(resp.Body, "ms-teams-message")

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("MS Teams API error: %s", resp.Status)
	}

	return map[string]any{"status": "sent"}, nil
}

type RabbitMQExecutor struct {
	conns sync.Map // url -> *amqp.Connection
}

func NewRabbitMQExecutor() *RabbitMQExecutor {
	return &RabbitMQExecutor{}
}

func (e *RabbitMQExecutor) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	url, _ := connectors.TextSetting(config, "url")
	exchange, _ := connectors.TextSetting(config, "exchange")
	routingKey, _ := connectors.TextSetting(config, "routing_key")
	queue, _ := connectors.TextSetting(config, "queue")

	if url == "" {
		return nil, fmt.Errorf("RabbitMQ URL is required")
	}

	var conn *amqp.Connection
	if v, ok := e.conns.Load(url); ok {
		// The pool is keyed by URL and only this executor writes to it, so the
		// stored type is ours — but a sync.Map is untyped, and a bare assertion
		// here would take the worker down rather than reconnecting.
		pooled, isConnection := v.(*amqp.Connection)
		if !isConnection || pooled.IsClosed() {
			e.conns.Delete(url)
		} else {
			conn = pooled
		}
	}

	if conn == nil {
		var err error
		conn, err = amqp.Dial(url)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
		}
		e.conns.Store(url, conn)
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}
	// A channel that will not close is a leaked AMQP channel; the broker holds
	// them per connection and runs out.
	defer func() {
		if err := ch.Close(); err != nil {
			log.Warn().Err(err).Msg("Could not close the AMQP channel")
		}
	}()

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// The connector's schema offers a "Queue (Direct Publish)" field, and
	// filling in only a url and a queue is the obvious way to use it. That
	// configuration used to publish to the default exchange with an *empty*
	// routing key, which routes to nothing: the message was discarded, the
	// service task reported success and the process carried on. The fallback
	// written for this was unreachable, because it ran only when the publish
	// returned an error and a fire-and-forget publish never does.
	//
	// The default exchange routes by queue name, so the queue *is* the routing
	// key.
	if routingKey == "" && exchange == "" {
		routingKey = queue
	}

	// Publisher confirms and mandatory delivery, both of which are the
	// difference between "sent" and "gone" — see amqp_confirm.go. This used to
	// be written out here, and the two publish paths in messaging.go did not
	// have it; sharing it is what stopped those two being the exception.
	publisher, err := newConfirmingPublisher(ch)
	if err != nil {
		return nil, err
	}
	if err := publisher.publish(ctx, exchange, routingKey, amqp.Publishing{
		ContentType: "application/json",
		Body:        body,
	}); err != nil {
		return nil, err
	}

	return map[string]any{"status": "published"}, nil
}

// BuiltInConnectorKeys names every executor compiled into the binary.
//
// It exists so a test can exercise all of them rather than whichever ones
// somebody remembered — a payload of the wrong type used to take the worker
// down through the one executor nobody had written a test for.
func BuiltInConnectorKeys() []string {
	return []string{
		"http-json",
		"slack-message",
		"email-smtp",
		"rabbitmq-publish",
		"discord-message",
		"sendgrid-email",
		"ms-teams-message",
		sqlconnector.Key,
	}
}
