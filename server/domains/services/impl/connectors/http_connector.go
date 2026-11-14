package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/httpclient"
	"github.com/gsoultan/metis/internal/pkg/idempotency"
)

const HTTPConnectorKey = "http-json"

// HTTPConnector is a built-in ConnectorExecutor that sends an HTTP request and
// returns the parsed JSON response body as output variables.
// Config keys: url (required), method (default GET), headers (map[string]string).
// Payload is serialised as the JSON request body for non-GET methods.
type HTTPConnector struct {
	client *http.Client
}

// NewHTTPConnector creates a new HTTPConnector with the given HTTP client.
func NewHTTPConnector(client *http.Client) *HTTPConnector {
	return &HTTPConnector{client: client}
}

func (c *HTTPConnector) httpClient() *http.Client {
	if c.client != nil {
		return c.client
	}
	return httpclient.Shared()
}

func (c *HTTPConnector) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	url, method, err := extractURLAndMethod(config)
	if err != nil {
		return nil, err
	}
	body, err := buildRequestBody(method, payload)
	if err != nil {
		return nil, err
	}
	headers, err := configuredHeaders(config)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("http connector: build request: %w", err)
	}
	applyHeaders(req, headers)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("http connector: execute request: %w", err)
	}
	defer closeQuietly(resp.Body, "http")

	return parseJSONResponse(resp)
}

func extractURLAndMethod(config map[string]any) (string, string, error) {
	url, _ := textSetting(config, "url")
	if url == "" {
		return "", "", fmt.Errorf("http connector: missing required config key 'url'")
	}
	method, _ := textSetting(config, "method")
	if method == "" {
		method = http.MethodGet
	}
	return url, method, nil
}

func buildRequestBody(method string, payload map[string]any) (io.Reader, error) {
	if method == http.MethodGet || len(payload) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("http connector: marshal payload: %w", err)
	}
	return bytes.NewReader(data), nil
}

func applyHeaders(req *http.Request, headers map[string]any) {
	req.Header.Set("Content-Type", "application/json")
	// The engine may repeat this call — a job whose result failed to commit is
	// retried — so it says which unit of work it belongs to and leaves the
	// downstream free to answer once.
	if key, ok := idempotency.KeyFrom(req.Context()); ok {
		req.Header.Set(idempotency.Header, key)
	}
	for k, v := range headers {
		if s, ok := v.(string); ok {
			req.Header.Set(k, s)
		}
	}
}

// configuredHeaders reads the headers setting in either form it arrives in.
//
// The Connectors page stores it as text: the catalogue declares "Headers
// (JSON)" as a string field, so what the administrator types is saved as a
// string. An API caller may pass an object. Reading only the object form
// dropped every header typed into the page — an Authorization header
// configured there never left the server, and the call went out without it.
//
// Text that is not a JSON object is refused, and the step fails. It used to be
// logged and the call sent without any headers, so a mistyped Authorization
// header sent the request unauthenticated, and the partner's answer — or worse,
// its acceptance — was the first sign. The error names the parse failure and
// not the text, which may hold a token.
func configuredHeaders(config map[string]any) (map[string]any, error) {
	switch headers := config["headers"].(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return headers, nil
	case string:
		text := strings.TrimSpace(headers)
		if text == "" {
			return nil, nil
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			return nil, fmt.Errorf("http connector: the connection's headers are not a JSON object (%v); fix them on the Connectors page", err)
		}
		return parsed, nil
	default:
		return nil, fmt.Errorf("http connector: the connection's headers are a %T, not a JSON object", headers)
	}
}

func parseJSONResponse(resp *http.Response) (map[string]any, error) {
	raw, err := httpclient.ReadResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http connector: read response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("http connector: server returned %d: %s", resp.StatusCode, string(raw))
	}
	if len(raw) == 0 {
		return map[string]any{"status_code": resp.StatusCode}, nil
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		// Not every 200 is JSON. A plain-text or XML body is handed on as text
		// rather than treated as an error, so a process can still read it.
		//nolint:nilerr // a non-JSON body is a result, not a failure
		return map[string]any{"body": string(raw), "status_code": resp.StatusCode}, nil
	}
	result["status_code"] = resp.StatusCode
	return result, nil
}
