package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gsoultan/gobpm/internal/pkg/idempotency"

	"github.com/gsoultan/gobpm/internal/pkg/httpclient"
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
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("http connector: build request: %w", err)
	}
	applyHeaders(req, config)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("http connector: execute request: %w", err)
	}
	defer closeQuietly(resp.Body, "http")

	return parseJSONResponse(resp)
}

func extractURLAndMethod(config map[string]any) (string, string, error) {
	url, _ := config["url"].(string)
	if url == "" {
		return "", "", fmt.Errorf("http connector: missing required config key 'url'")
	}
	method, _ := config["method"].(string)
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

func applyHeaders(req *http.Request, config map[string]any) {
	req.Header.Set("Content-Type", "application/json")
	// The engine may repeat this call — a job whose result failed to commit is
	// retried — so it says which unit of work it belongs to and leaves the
	// downstream free to answer once.
	if key, ok := idempotency.KeyFrom(req.Context()); ok {
		req.Header.Set(idempotency.Header, key)
	}
	headers, _ := config["headers"].(map[string]any)
	for k, v := range headers {
		if s, ok := v.(string); ok {
			req.Header.Set(k, s)
		}
	}
}

func parseJSONResponse(resp *http.Response) (map[string]any, error) {
	raw, err := io.ReadAll(resp.Body)
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
