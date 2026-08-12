package impl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gsoultan/gobpm/internal/pkg/httpclient"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/repositories"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
	"net"
	"net/smtp"
	"strconv"
)

type connectorService struct {
	repo      repositories.Repository
	executors map[string]servicecontracts.ConnectorExecutor
}

func NewConnectorService(
	repo repositories.Repository,
) servicecontracts.ConnectorService {
	s := &connectorService{
		repo:      repo,
		executors: make(map[string]servicecontracts.ConnectorExecutor),
	}

	// Register built-in executors
	s.executors["http-json"] = &HttpJsonExecutor{}
	s.executors["slack-message"] = &SlackMessageExecutor{}
	s.executors["email-smtp"] = &EmailSmtpExecutor{}
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
	if err := s.EnsureDefaultConnectors(context.Background()); err != nil {
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
		res[i] = adapters.ConnectorEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *connectorService) GetConnector(ctx context.Context, id uuid.UUID) (entities.Connector, error) {
	m, err := s.repo.Connector().Get(ctx, id)
	if err != nil {
		return entities.Connector{}, err
	}
	return adapters.ConnectorEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *connectorService) CreateConnector(ctx context.Context, c entities.Connector) (entities.Connector, error) {
	if c.ID == uuid.Nil {
		c.ID, _ = uuid.NewV7()
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
	return res, nil
}

func (s *connectorService) GetConnectorInstance(ctx context.Context, id uuid.UUID) (entities.ConnectorInstance, error) {
	m, err := s.repo.ConnectorInstance().Get(ctx, id)
	if err != nil {
		return entities.ConnectorInstance{}, err
	}
	return adapters.ConnectorInstanceEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *connectorService) GetConnectorInstanceByProjectAndConnector(ctx context.Context, projectID, connectorID uuid.UUID) (entities.ConnectorInstance, error) {
	m, err := s.repo.ConnectorInstance().GetByProjectAndConnector(ctx, projectID, connectorID)
	if err != nil {
		return entities.ConnectorInstance{}, err
	}
	return adapters.ConnectorInstanceEntityAdapter{Model: m}.ToEntity(), nil
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
		instance.ID, _ = uuid.NewV7()
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

func (s *connectorService) ExecuteConnector(ctx context.Context, connectorKey string, config map[string]any, payload map[string]any) (map[string]any, error) {
	executor, ok := s.executors[connectorKey]
	if !ok {
		return nil, fmt.Errorf("no executor found for connector key: %s", connectorKey)
	}
	return executor.Execute(ctx, config, payload)
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

type HttpJsonExecutor struct{}

func (e *HttpJsonExecutor) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	url, _ := config["url"].(string)
	method, _ := config["method"].(string)
	if method == "" {
		method = "POST"
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Apply configured headers
	if hStr, ok := config["headers"].(string); ok && hStr != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(hStr), &headers); err == nil {
			for k, v := range headers {
				req.Header.Set(k, v)
			}
		}
	}

	resp, err := httpclient.Shared().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP error: %s", resp.Status)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	_ = json.Unmarshal(respBody, &result)
	return result, nil
}

type DiscordMessageExecutor struct{}

func (e *DiscordMessageExecutor) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	webhookURL, _ := config["webhook_url"].(string)
	content, _ := payload["content"].(string)
	if content == "" {
		content = payload["text"].(string)
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

	body, _ := json.Marshal(discordPayload)
	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpclient.Shared().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Discord API error: %s", resp.Status)
	}

	return map[string]any{"status": "sent"}, nil
}

type SendGridEmailExecutor struct{}

func (e *SendGridEmailExecutor) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	apiKey, _ := config["api_key"].(string)
	fromEmail, _ := config["from_email"].(string)
	fromName, _ := config["from_name"].(string)

	toEmail, _ := payload["to_email"].(string)
	if toEmail == "" {
		toEmail, _ = config["to_email"].(string)
	}
	subject, _ := payload["subject"].(string)
	if subject == "" {
		subject, _ = config["subject"].(string)
	}
	content, _ := payload["content"].(string)
	if content == "" {
		content, _ = config["content"].(string)
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

	body, _ := json.Marshal(sgPayload)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.sendgrid.com/v3/mail/send", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpclient.Shared().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("SendGrid API error: %s - %s", resp.Status, string(respBody))
	}

	return map[string]any{"status": "sent"}, nil
}

type MSTeamsMessageExecutor struct{}

func (e *MSTeamsMessageExecutor) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	webhookURL, _ := config["webhook_url"].(string)
	text, _ := payload["text"].(string)
	if text == "" {
		text = "No message text provided"
	}

	teamsPayload := map[string]any{
		"text": text,
	}

	body, _ := json.Marshal(teamsPayload)
	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpclient.Shared().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("MS Teams API error: %s", resp.Status)
	}

	return map[string]any{"status": "sent"}, nil
}

type SlackMessageExecutor struct{}

func (e *SlackMessageExecutor) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	webhookURL, _ := config["webhook_url"].(string)
	text, _ := payload["text"].(string)
	if text == "" {
		text = "No message text provided"
	}

	slackPayload := map[string]any{
		"text": text,
	}
	if channel, ok := config["channel"].(string); ok && channel != "" {
		slackPayload["channel"] = channel
	}

	body, _ := json.Marshal(slackPayload)
	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpclient.Shared().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Slack error: %s", resp.Status)
	}

	return map[string]any{"status": "ok"}, nil
}

type EmailSmtpExecutor struct{}

func (e *EmailSmtpExecutor) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	host, _ := config["host"].(string)
	portStr, _ := config["port"].(string)
	username, _ := config["username"].(string)
	password, _ := config["password"].(string)
	from, _ := config["from"].(string)

	to, _ := payload["to"].(string)
	subject, _ := payload["subject"].(string)
	body, _ := payload["body"].(string)

	if host == "" || portStr == "" || username == "" || password == "" {
		return nil, fmt.Errorf("SMTP configuration is incomplete")
	}

	if to == "" {
		return nil, fmt.Errorf("recipient email 'to' is required in payload")
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid SMTP port: %w", err)
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	auth := smtp.PlainAuth("", username, password, host)

	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s\r\n", to, subject, body))

	err = smtp.SendMail(addr, auth, from, []string{to}, msg)
	if err != nil {
		return nil, fmt.Errorf("failed to send email via SMTP: %w", err)
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
	url, _ := config["url"].(string)
	exchange, _ := config["exchange"].(string)
	routingKey, _ := config["routing_key"].(string)
	queue, _ := config["queue"].(string)

	if url == "" {
		return nil, fmt.Errorf("RabbitMQ URL is required")
	}

	var conn *amqp.Connection
	if v, ok := e.conns.Load(url); ok {
		conn = v.(*amqp.Connection)
		if conn.IsClosed() {
			e.conns.Delete(url)
			conn = nil
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
	defer ch.Close()

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	err = ch.PublishWithContext(ctx,
		exchange,   // exchange
		routingKey, // routing key
		false,      // mandatory
		false,      // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		})

	if err != nil && queue != "" && exchange == "" {
		// Try direct queue publish if exchange is empty
		err = ch.PublishWithContext(ctx,
			"",    // exchange
			queue, // routing key (queue name)
			false, // mandatory
			false, // immediate
			amqp.Publishing{
				ContentType: "application/json",
				Body:        body,
			})
	}

	if err != nil {
		return nil, fmt.Errorf("failed to publish message: %w", err)
	}

	return map[string]any{"status": "published"}, nil
}
