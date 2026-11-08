package connectors

import (
	"context"
	"errors"
	"fmt"

	"github.com/gsoultan/metis/internal/pkg/fcm"
)

const (
	// FCMConnectorKey is the built-in Firebase Cloud Messaging connector.
	FCMConnectorKey = "fcm-push"
	// FCMServiceAccountKey is the config key holding the service account key
	// file. Its name contains "key", so the connector API masks it like any
	// other credential and never sends it back.
	FCMServiceAccountKey = "service_account_key"
	// FCMUseForPush is the config key saying this connection is the one the
	// project's own pushes (reminders, push templates) go through.
	FCMUseForPush = "use_for_push"
)

// FCMConnector is a built-in ConnectorExecutor that sends one push through
// Firebase Cloud Messaging.
// Config keys: service_account_key (required), use_for_push.
// Payload keys: token (required), title and/or body, link, data (string map).
type FCMConnector struct {
	pool *fcm.Pool
}

// NewFCMConnector returns a connector sharing one signed-in client per
// service account across every call.
func NewFCMConnector(pool *fcm.Pool) *FCMConnector {
	if pool == nil {
		pool = fcm.NewPool()
	}
	return &FCMConnector{pool: pool}
}

func (c *FCMConnector) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	key, _ := TextSetting(config, FCMServiceAccountKey)
	if key == "" {
		return nil, fmt.Errorf("push connector: no service account key; upload the JSON key file from Firebase console, Project settings, Service accounts")
	}
	client, err := c.pool.Client(key)
	if err != nil {
		return nil, fmt.Errorf("push connector: %w", err)
	}
	msg, err := fcmMessage(payload)
	if err != nil {
		return nil, err
	}
	id, err := client.Send(ctx, msg)
	if err != nil {
		return nil, fcmHint(err)
	}
	return map[string]any{"status": "sent", "message_id": id}, nil
}

func fcmMessage(payload map[string]any) (fcm.Message, error) {
	token, _ := TextSetting(payload, "token")
	title, _ := TextSetting(payload, "title")
	body, _ := TextSetting(payload, "body")
	link, _ := TextSetting(payload, "link")
	if token == "" {
		return fcm.Message{}, fmt.Errorf("push connector: missing required payload key 'token' (the device's registration token)")
	}
	if title == "" && body == "" {
		return fcm.Message{}, fmt.Errorf("push connector: a push needs a 'title' or a 'body'")
	}
	data, err := fcmData(payload["data"])
	if err != nil {
		return fcm.Message{}, err
	}
	return fcm.Message{Token: token, Title: title, Body: body, Link: link, Data: data}, nil
}

// fcmData accepts an object of text values. FCM takes only strings, and
// quietly stringifying a number or an object here would hand the app a shape
// it did not ask for.
func fcmData(raw any) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("push connector: 'data' must be an object of text values")
	}
	data := make(map[string]string, len(values))
	for name, value := range values {
		text, isText := value.(string)
		if !isText {
			return nil, fmt.Errorf("push connector: data value %q must be text", name)
		}
		data[name] = text
	}
	return data, nil
}

// fcmHint says what to do about a refusal in words a person can act on.
func fcmHint(err error) error {
	switch {
	case errors.Is(err, fcm.ErrUnregistered):
		return fmt.Errorf("push connector: the device is no longer registered (the app was removed or its token changed): %w", err)
	case errors.Is(err, fcm.ErrCredentials):
		return fmt.Errorf("push connector: Firebase refused the service account; check the key is current and belongs to this Firebase project: %w", err)
	default:
		return fmt.Errorf("push connector: %w", err)
	}
}
