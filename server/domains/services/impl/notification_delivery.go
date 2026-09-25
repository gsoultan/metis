package impl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/httpclient"
	"github.com/gsoultan/metis/internal/pkg/mail"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// Getting a notification out of the application.
//
// The notification centre is only read by somebody who is already looking at
// it, and the whole point of telling people a task is theirs is to reach them
// when they are not. A channel is how a notification leaves.

// NotificationChannel delivers one notification somewhere outside the app.
type NotificationChannel interface {
	// Deliver sends it. An error means this channel did not, which is worth
	// logging and is not worth failing the notification over.
	Deliver(ctx context.Context, n entities.Notification) error
	// Name is what the logs call this channel.
	Name() string
}

// deliveringNotificationService stores a notification and then hands it to
// every configured channel.
type deliveringNotificationService struct {
	contracts.NotificationService
	channels []NotificationChannel
	schedule DeliveryScheduler
}

// DeliveryScheduler decides when, and on which goroutine, a stored
// notification is delivered.
type DeliveryScheduler func(ctx context.Context, deliver func(context.Context))

// DeliverNow delivers on the caller's goroutine, straight away. For a caller
// with no transaction around it, and for tests.
func DeliverNow(ctx context.Context, deliver func(context.Context)) { deliver(ctx) }

// NewDeliveringNotificationService wraps a notification service so that what it
// stores is also sent, when schedule says.
//
// With no channels it is the service it wraps, which is what an installation
// that has configured nothing should get.
func NewDeliveringNotificationService(inner contracts.NotificationService, schedule DeliveryScheduler, channels ...NotificationChannel) contracts.NotificationService {
	live := make([]NotificationChannel, 0, len(channels))
	for _, channel := range channels {
		if channel != nil {
			live = append(live, channel)
		}
	}
	if len(live) == 0 {
		return inner
	}
	if schedule == nil {
		schedule = DeliverNow
	}
	return &deliveringNotificationService{NotificationService: inner, channels: live, schedule: schedule}
}

// Send stores the notification first, then delivers it.
//
// Storing first, and failing only on that, is deliberate. The in-app centre is
// the record: if it holds the notification then the person will see it the next
// time they look, and an SMTP server that is down should not also lose them the
// one place it was guaranteed to appear. A channel that fails is logged loudly
// with which channel and which recipient, because "the email never arrived" is
// otherwise unanswerable.
//
// Delivery is scheduled rather than done here. Send is called by the engine
// while it creates, claims or assigns a task — inside that transaction — and
// delivering there made a webhook or an SMTP conversation hold the
// transaction's connection and the task's row locks for as long as somebody
// else's server took to answer. It also delivered notifications for work a
// rollback then undid.
func (s *deliveringNotificationService) Send(ctx context.Context, n entities.Notification) error {
	if err := s.NotificationService.Send(ctx, n); err != nil {
		return err
	}
	s.schedule(ctx, func(deliveryCtx context.Context) { s.deliver(deliveryCtx, n) })
	return nil
}

func (s *deliveringNotificationService) deliver(ctx context.Context, n entities.Notification) {
	for _, channel := range s.channels {
		if err := channel.Deliver(ctx, n); err != nil {
			recipient := ""
			if n.User != nil {
				recipient = n.User.Username
			}
			log.Warn().Err(err).
				Str("channel", channel.Name()).
				Str("recipient", recipient).
				Msg("A notification was stored but not delivered; it is in the notification centre and nowhere else")
		}
	}
}

// ─── webhook ────────────────────────────────────────────────────────────────

// WebhookNotificationChannel posts each notification to one URL.
type WebhookNotificationChannel struct {
	url    string
	client *http.Client
}

// NewWebhookNotificationChannel returns a channel posting to url, or nil when
// there is no url — an installation that configured nothing gets nothing.
//
// It returns the interface rather than the concrete type on purpose. A
// constructor that returns a typed nil pointer hands back something that is not
// equal to nil once it is in an interface, so a caller checking for nil keeps
// it and the first delivery panics on a nil receiver.
func NewWebhookNotificationChannel(url string) NotificationChannel {
	if strings.TrimSpace(url) == "" {
		return nil
	}
	// The shared client, so a notification webhook is subject to the same
	// egress policy as every other outbound call. A URL that reaches somewhere
	// the policy forbids is refused here rather than being a way around it.
	return &WebhookNotificationChannel{url: strings.TrimSpace(url), client: httpclient.Shared()}
}

func (c *WebhookNotificationChannel) Name() string { return "webhook" }

// webhookPayload is the shape posted. Flat and named, rather than the entity:
// a receiver should not have to know the engine's internal structs to read who
// a notification is for.
type webhookPayload struct {
	Recipient string `json:"recipient"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Link      string `json:"link,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Instance  string `json:"instance_id,omitempty"`
}

func (c *WebhookNotificationChannel) Deliver(ctx context.Context, n entities.Notification) error {
	payload := webhookPayload{
		Type:    string(n.Type),
		Title:   n.Title,
		Message: n.Message,
		Link:    n.Link,
	}
	if n.User != nil {
		payload.Recipient = n.User.Username
	}
	if n.Project != nil {
		payload.ProjectID = n.Project.ID.String()
	}
	if n.Instance != nil {
		payload.Instance = n.Instance.ID.String()
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("notification webhook: encode: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notification webhook: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("notification webhook: post: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Str("url", c.url).Msg("Could not close the notification webhook response")
		}
	}()

	// A 2xx or it did not arrive. A receiver answering 500 has not taken it,
	// and treating that as delivered is the same mistake as a publish without
	// confirms.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("notification webhook: %s answered %s", c.url, resp.Status)
	}
	return nil
}

// ─── email ──────────────────────────────────────────────────────────────────

// EmailSettings is what an installation needs to send mail.
type EmailSettings struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
}

// Configured reports whether there is enough here to send anything.
func (s EmailSettings) Configured() bool {
	return s.Host != "" && s.From != ""
}

// AddressLookup answers where to send a person's mail.
type AddressLookup func(ctx context.Context, username string) (string, error)

// EmailNotificationChannel sends each notification to its recipient's address.
type EmailNotificationChannel struct {
	settings EmailSettings
	address  AddressLookup
	send     MailSender
}

// MailSender hands a message to an SMTP server, giving up at ctx's deadline.
type MailSender func(ctx context.Context, addr string, a smtp.Auth, from string, to []string, msg []byte) error

// EmailOption adjusts an email channel.
type EmailOption func(*EmailNotificationChannel)

// WithMailSender replaces the function that hands a message to the SMTP server.
// It exists so a test can assert what would be sent without a mail server.
func WithMailSender(send MailSender) EmailOption {
	return func(c *EmailNotificationChannel) { c.send = send }
}

// NewEmailNotificationChannel returns a channel that emails notifications, or
// nil when the installation has not configured mail.
//
// Returns the interface for the same reason the webhook constructor does: a
// typed nil pointer is not nil once it is in one.
func NewEmailNotificationChannel(settings EmailSettings, address AddressLookup, opts ...EmailOption) NotificationChannel {
	if !settings.Configured() || address == nil {
		return nil
	}
	if settings.Port == "" {
		settings.Port = "587"
	}
	channel := &EmailNotificationChannel{settings: settings, address: address, send: mail.Send}
	for _, opt := range opts {
		opt(channel)
	}
	return channel
}

func (c *EmailNotificationChannel) Name() string { return "email" }

func (c *EmailNotificationChannel) Deliver(ctx context.Context, n entities.Notification) error {
	if n.User == nil || n.User.Username == "" {
		return fmt.Errorf("notification email: the notification names no recipient")
	}
	to, err := c.address(ctx, n.User.Username)
	if err != nil {
		return fmt.Errorf("notification email: looking up an address for %s: %w", n.User.Username, err)
	}
	if strings.TrimSpace(to) == "" {
		// Not an error worth shouting about: plenty of accounts have no
		// address, and a notification for one of them is simply an in-app one.
		return nil
	}

	var body strings.Builder
	body.WriteString("From: " + c.settings.From + "\r\n")
	body.WriteString("To: " + to + "\r\n")
	body.WriteString("Subject: " + n.Title + "\r\n")
	body.WriteString("MIME-Version: 1.0\r\n")
	body.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	body.WriteString(n.Message)
	if n.Link != "" {
		body.WriteString("\r\n\r\n" + n.Link)
	}

	var auth smtp.Auth
	if c.settings.Username != "" {
		auth = smtp.PlainAuth("", c.settings.Username, c.settings.Password, c.settings.Host)
	}
	// The envelope names the person, not the sender. The connector next door
	// addressed it to itself for a while and every message went to the noreply
	// mailbox, which is a mistake worth only making once.
	addr := c.settings.Host + ":" + c.settings.Port
	if err := c.send(ctx, addr, auth, c.settings.From, []string{to}, []byte(body.String())); err != nil {
		return fmt.Errorf("notification email: send to %s: %w", to, err)
	}
	return nil
}
