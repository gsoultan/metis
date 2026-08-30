package impl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gsoultan/metis/internal/pkg/idempotency"

	"github.com/gsoultan/metis/internal/pkg/httpclient"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/rs/zerolog/log"
)

// HTTPServiceTaskRunner executes an outbound HTTP call for a BPMN service task.
// Responsibilities are split into small, focused private methods to keep each
// step testable in isolation.
type HTTPServiceTaskRunner struct {
	client *http.Client
}

// NewHTTPServiceTaskRunner returns a runner that uses the supplied HTTP client.
// Pass nil to use the shared guarded client (explicit timeout, bounded
// redirects, SSRF egress policy).
func NewHTTPServiceTaskRunner(client *http.Client) *HTTPServiceTaskRunner {
	if client == nil {
		client = httpclient.Shared()
	}
	return &HTTPServiceTaskRunner{client: client}
}

// Run executes the HTTP call described by node properties, merging the process
// variables in payload into the request, and returning the output variables.
// Returns nil, nil when the node has no http_url (simulated task).
func (r *HTTPServiceTaskRunner) Run(ctx context.Context, node entities.Node, payload map[string]any) (map[string]any, error) {
	url := node.GetStringProperty("http_url")
	if url == "" {
		return nil, nil // simulated / no-op task
	}

	method := node.GetStringProperty("http_method")
	if method == "" {
		method = "GET"
	}
	log.Info().Str("method", method).Str("url", url).Msg("Executing HTTP service task")

	reqBody := r.buildRequestBody(method, node, payload)
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	r.addHeaders(req, node)
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	r.applyAuth(req, node)
	// See the note in the HTTP connector: a retried job repeats this call, and
	// the key is what lets the other end tell that from a new one.
	if key, ok := idempotency.KeyFrom(ctx); ok {
		req.Header.Set(idempotency.Header, key)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Debug().Err(err).Msg("Could not close the service task response")
		}
	}()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP error: %s (status %d)", resp.Status, resp.StatusCode)
	}

	return r.parseResponse(resp.Body, node)
}

// buildRequestBody serialises the input mapping (or full payload) for write methods.
func (r *HTTPServiceTaskRunner) buildRequestBody(method string, node entities.Node, payload map[string]any) io.Reader {
	if method != "POST" && method != "PUT" && method != "PATCH" {
		return nil
	}
	mapped := make(map[string]any)
	for k, v := range node.Properties {
		if len(k) > 6 && k[:6] == "input_" {
			if targetKey, ok := v.(string); ok {
				varKey := k[6:]
				if val, ok := payload[varKey]; ok {
					mapped[targetKey] = val
				}
			}
		}
	}
	if len(mapped) == 0 {
		mapped = payload
	}
	data, err := json.Marshal(mapped)
	if err != nil {
		// A body that cannot be encoded is a broken request, not an empty one.
		// Discarding the error sent "null" and left the endpoint to decide.
		log.Error().Err(err).Msg("A service task's request body could not be encoded")
		return nil
	}
	return bytes.NewReader(data)
}

// addHeaders injects header_ prefixed properties from the node into the request.
func (r *HTTPServiceTaskRunner) addHeaders(req *http.Request, node entities.Node) {
	for k, v := range node.Properties {
		if len(k) > 7 && k[:7] == "header_" {
			if val, ok := v.(string); ok {
				req.Header.Set(k[7:], val)
			}
		}
	}
}

// applyAuth adds authentication credentials based on the auth_type property.
func (r *HTTPServiceTaskRunner) applyAuth(req *http.Request, node entities.Node) {
	switch node.GetStringProperty("auth_type") {
	case "basic":
		req.SetBasicAuth(node.GetStringProperty("auth_username"), node.GetStringProperty("auth_password"))
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+node.GetStringProperty("auth_token"))
	case "api_key":
		headerName := node.GetStringProperty("auth_header_name")
		if headerName == "" {
			headerName = "X-API-Key"
		}
		req.Header.Set(headerName, node.GetStringProperty("auth_api_key"))
	}
}

// parseResponse reads the response body and applies output mapping.
func (r *HTTPServiceTaskRunner) parseResponse(body io.Reader, node entities.Node) (map[string]any, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("the service task's reply was cut short after %d bytes: %w", len(raw), err)
	}
	if len(raw) == 0 {
		// An empty body is a legitimate reply — there is simply nothing to map.
		return nil, nil
	}

	var jsonMap map[string]any
	if err := json.Unmarshal(raw, &jsonMap); err != nil {
		// Same as the HTTP connector: a non-JSON body is handed on as text.
		//nolint:nilerr // a non-JSON body is a result, not a failure
		return map[string]any{"response": string(raw)}, nil
	}

	// Apply output_ mapping if configured.
	mapped := make(map[string]any)
	for k, v := range node.Properties {
		if len(k) > 7 && k[:7] == "output_" {
			if targetVar, ok := v.(string); ok {
				jsonPath := k[7:]
				if val, ok := jsonMap[jsonPath]; ok {
					mapped[targetVar] = val
				}
			}
		}
	}
	if len(mapped) == 0 {
		return jsonMap, nil
	}
	return mapped, nil
}
