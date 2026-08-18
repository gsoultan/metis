package connectors

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gsoultan/gobpm/internal/pkg/httpclient"
	"github.com/gsoultan/gobpm/internal/pkg/idempotency"
	"github.com/gsoultan/gobpm/server/domains/logic/feel"
	"github.com/rs/zerolog/log"
)

// Running a manifest.
//
// Everything a connector does is here rather than in a Go function per vendor:
// fill in the templates, sign the request, send it, decide whether it worked,
// and turn what came back into process variables — or into a BPMN error a
// boundary event can catch.

// maxResponseBytes bounds what is read back.
//
// The body is parsed and mapped into process variables, so it is held in memory
// in full. A partner that answers a request with a gigabyte should cost one
// failed job, not the engine.
const maxResponseBytes = 4 << 20 // 4 MiB

// ManifestError is a failure a manifest recognised and named.
//
// It carries a BPMN error code so a boundary event can catch it: "the card was
// declined" becomes a path in the diagram rather than an incident somebody has
// to read a stack trace to understand.
type ManifestError struct {
	Code      string
	Message   string
	Status    int
	Retryable bool

	// RetryAfter is what the partner asked for, when they asked. Honouring it
	// is the difference between backing off and being blocked.
	RetryAfter time.Duration
}

func (e *ManifestError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s (status %d)", e.Code, e.Status)
}

// BPMNErrorCode is what an error boundary event matches on.
func (e *ManifestError) BPMNErrorCode() string { return e.Code }

// RunManifest executes one connector call.
//
// config is what an administrator set for the tenant; input is what the node
// supplies. They are separate in the template scope because they come from
// different people with different authority, and a manifest that could read one
// as the other would let a modeller reach a credential.
func RunManifest(
	ctx context.Context,
	manifest Manifest,
	config map[string]any,
	input map[string]any,
	client *http.Client,
) (map[string]any, error) {
	if client == nil {
		client = httpclient.Shared()
	}

	scope := map[string]any{"config": config, "input": input}

	request, err := buildRequest(ctx, manifest, scope)
	if err != nil {
		return nil, err
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("connector %q: %w", manifest.Key, err)
	}
	defer closeQuietly(response.Body, manifest.Key)

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("connector %q: could not read the response: %w", manifest.Key, err)
	}

	return interpret(manifest, response, body)
}

// buildRequest fills in the templates and signs the call.
func buildRequest(ctx context.Context, manifest Manifest, scope map[string]any) (*http.Request, error) {
	rendered, err := renderString(manifest.Request.URL, scope)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(asText(rendered))
	if err != nil {
		return nil, fmt.Errorf("connector %q: the request URL is not a URL: %w", manifest.Key, err)
	}

	query, err := renderStrings(manifest.Request.Query, scope)
	if err != nil {
		return nil, err
	}
	if len(query) > 0 {
		values := parsed.Query()
		for key, value := range query {
			values.Set(key, value)
		}
		parsed.RawQuery = values.Encode()
	}

	// The URL comes from a manifest an administrator installed rather than from
	// a process definition — but it is still configuration, and the egress
	// policy exists because configuration reaches places nobody intended.
	if err := httpclient.CheckURL(parsed); err != nil {
		return nil, fmt.Errorf("connector %q: %w", manifest.Key, err)
	}
	target := parsed.String()

	var payload io.Reader
	if len(manifest.Request.Body) > 0 {
		rendered, renderErr := renderValue(manifest.Request.Body, scope)
		if renderErr != nil {
			return nil, renderErr
		}
		encoded, marshalErr := json.Marshal(rendered)
		if marshalErr != nil {
			return nil, fmt.Errorf("connector %q: could not encode the request body: %w", manifest.Key, marshalErr)
		}
		payload = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, manifest.Method(), target, payload)
	if err != nil {
		return nil, fmt.Errorf("connector %q: %w", manifest.Key, err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	headers, err := renderStrings(manifest.Request.Headers, scope)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	if err := applyAuth(request, manifest.Auth, configOf(scope)); err != nil {
		return nil, fmt.Errorf("connector %q: %w", manifest.Key, err)
	}

	// The engine may repeat this call — a job whose result failed to commit is
	// retried — so it says which unit of work it belongs to.
	if key, ok := idempotency.KeyFrom(ctx); ok {
		request.Header.Set(idempotency.Header, key)
	}
	return request, nil
}

// applyAuth signs the request from the tenant's configuration.
//
// Credentials come from config and never from input: config is set by an
// administrator, input by whoever models a process, and the two must not be
// interchangeable or a modeller could read a credential by mapping it into a
// variable.
func applyAuth(request *http.Request, auth Auth, config map[string]any) error {
	switch auth.Type {
	case "", authNone:
		return nil

	case authBasic:
		username, _ := textSetting(config, "username")
		password, _ := textSetting(config, "password")
		if username == "" {
			return fmt.Errorf("this connector authenticates with a username and password, and no username is configured")
		}
		encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		request.Header.Set("Authorization", "Basic "+encoded)
		return nil

	case authBearer:
		token, _ := textSetting(config, "token")
		if token == "" {
			return fmt.Errorf("this connector authenticates with a token, and none is configured")
		}
		request.Header.Set("Authorization", "Bearer "+token)
		return nil

	case authAPIKey:
		key, _ := textSetting(config, "api_key")
		if key == "" {
			return fmt.Errorf("this connector authenticates with an API key, and none is configured")
		}
		header := auth.Header
		if header == "" {
			header = "Authorization"
		}
		request.Header.Set(header, key)
		return nil

	case authOAuth2ClientCredentials:
		// Deliberately not implemented here. A token has to be fetched, cached
		// until it expires, and refreshed under concurrency — that is a piece of
		// state, and putting it in the middle of a stateless request builder is
		// how it ends up fetched once per call. It belongs beside the connector
		// instance, which is where the credentials already live.
		return fmt.Errorf("OAuth client credentials are not supported yet; use a bearer token for now")

	default:
		return fmt.Errorf("%q is not a kind of authentication this understands", auth.Type)
	}
}

// interpret decides whether the call worked and what it produced.
func interpret(manifest Manifest, response *http.Response, body []byte) (map[string]any, error) {
	scope := responseScope(response, body)

	// Errors are checked before success, so a manifest can name a failure that
	// arrives with a 200 — which plenty of APIs do.
	for _, rule := range manifest.Errors {
		matched, err := condition(rule.When, scope)
		if err != nil {
			return nil, fmt.Errorf("connector %q: %w", manifest.Key, err)
		}
		if !matched {
			continue
		}
		return nil, manifestError(rule, scope, response.StatusCode)
	}

	ok, err := succeeded(manifest.Response.Success, scope, response.StatusCode)
	if err != nil {
		return nil, fmt.Errorf("connector %q: %w", manifest.Key, err)
	}
	if !ok {
		return nil, fmt.Errorf("connector %q: the call failed with status %d", manifest.Key, response.StatusCode)
	}

	outputs := make(map[string]any, len(manifest.Response.Outputs))
	for name, expression := range manifest.Response.Outputs {
		value, evalErr := feel.Evaluate(expression, scope)
		if evalErr != nil {
			return nil, fmt.Errorf("connector %q: could not read %q from the response: %w", manifest.Key, name, evalErr)
		}
		// A field the response did not carry is left out rather than written as
		// null, so a downstream step can tell "absent" from "explicitly empty".
		if value.Kind == feel.KindNull {
			continue
		}
		outputs[name] = value.ToAny()
	}
	return outputs, nil
}

// responseScope is what the success condition, the error rules and the output
// expressions all see.
func responseScope(response *http.Response, body []byte) map[string]any {
	headers := make(map[string]any, len(response.Header))
	for key, values := range response.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}

	scope := map[string]any{
		"status":  float64(response.StatusCode),
		"headers": headers,
		"body":    map[string]any{},
	}

	// A body that is not JSON is not an error: plenty of endpoints answer with
	// nothing, or with text. It just means `body.something` finds nothing.
	var parsed any
	if err := json.Unmarshal(body, &parsed); err == nil {
		scope["body"] = parsed
	}
	return scope
}

// succeeded applies the manifest's idea of success, or the usual one.
func succeeded(expression string, scope map[string]any, status int) (bool, error) {
	if strings.TrimSpace(expression) == "" {
		return status >= 200 && status < 300, nil
	}
	return condition(expression, scope)
}

func condition(expression string, scope map[string]any) (bool, error) {
	return feel.EvaluateCondition(expression, scope)
}

// manifestError builds the failure a matched rule describes.
func manifestError(rule ErrorRule, scope map[string]any, status int) error {
	failure := &ManifestError{
		Code:      rule.BPMNError,
		Message:   rule.Message,
		Status:    status,
		Retryable: rule.Retryable,
	}
	if rule.RetryAfter != "" {
		if value, err := feel.Evaluate(rule.RetryAfter, scope); err == nil {
			failure.RetryAfter = asDuration(value.ToAny())
		}
	}
	return failure
}

// asDuration reads a Retry-After, which arrives as seconds — as a number if the
// partner sent JSON, as text if it came from a header.
func asDuration(value any) time.Duration {
	switch v := value.(type) {
	case float64:
		return time.Duration(v) * time.Second
	case string:
		if seconds, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	return 0
}

func configOf(scope map[string]any) map[string]any {
	config, isMap := scope["config"].(map[string]any)
	if !isMap || config == nil {
		return map[string]any{}
	}
	return config
}

// closeQuietly closes a response body.
//
// A failure closing one means the connection was already gone, which changes
// nothing about the answer already read — but it is logged rather than dropped,
// because a stream of them is a sign of something worth knowing about.
func closeQuietly(body io.Closer, key string) {
	if err := body.Close(); err != nil {
		log.Debug().Err(err).Str("connector", key).Msg("Could not close the connector response")
	}
}

// textSetting reads a configured string.
//
// A setting that is present but not a string is the same as absent as far as
// signing a request goes — and reporting "no token configured" is more useful
// than reporting that a token was a number.
func textSetting(config map[string]any, key string) (string, bool) {
	text, isText := config[key].(string)
	return text, isText
}
