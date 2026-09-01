// Package metis is the Go client for a Metis server.
//
// It covers the four ways an application integrates with the engine:
//
//   - deploy a process definition and start instances of it
//   - correlate messages and broadcast signals into running instances
//   - work human tasks: list, claim, complete
//   - serve external tasks: a Worker long-polls a topic, does the work in
//     your process, and reports back
//
// The package has no dependencies outside the standard library, so importing
// it costs nothing but this code.
//
//	client := metis.NewClient("http://localhost:8080")
//	if err := client.Login(ctx, "admin", "secret"); err != nil { ... }
//
//	id, err := client.StartProcess(ctx, projectID, "invoice-approval",
//		metis.Variables{"amount": 900})
package metis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Variables is the map of process variables passed into and out of the engine.
type Variables = map[string]any

// Client talks to one metis server as one authenticated principal.
//
// It is safe for concurrent use. The zero value is not usable; construct with
// NewClient.
type Client struct {
	baseURL string
	http    *http.Client

	mu    sync.RWMutex
	token string
	orgID string
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient replaces the underlying HTTP client — for custom TLS,
// proxies, or instrumentation.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithToken supplies a token obtained elsewhere, instead of calling Login.
// Useful when the token comes from a secret store rather than a password.
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// defaultTimeout bounds every request. Without it a wedged server holds the
// caller's goroutine forever, and integrations inherit the outage.
const defaultTimeout = 30 * time.Second

// NewClient builds a client for the server at baseURL, e.g.
// "https://bpm.example.com". The /api/v1 prefix is added by the client.
func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// APIError is a non-2xx answer from the server, carrying the HTTP status and
// the server's message.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("metis: server returned %d: %s", e.StatusCode, e.Message)
}

// IsNotFound reports whether err is the server saying a resource does not
// exist — which, under tenant scoping, is also what "not yours" looks like.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// IsUnauthorized reports whether err means the token is missing, expired, or
// not allowed to do this.
func IsUnauthorized(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) &&
		(apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden)
}

// Login authenticates with a username and password and stores the token on
// the client for every later call.
func (c *Client) Login(ctx context.Context, username, password string) error {
	var out struct {
		Token string `json:"token"`
	}
	err := c.do(ctx, http.MethodPost, "/api/v1/login", map[string]string{
		"username": username,
		"password": password,
	}, &out)
	if err != nil {
		return err
	}
	if out.Token == "" {
		return errors.New("metis: login answered without a token")
	}

	c.mu.Lock()
	c.token = out.Token
	c.mu.Unlock()
	return nil
}

// SetOrganization selects which of the caller's organizations later requests
// act in. Only needed for accounts that belong to more than one — the server
// validates the choice against the caller's actual memberships.
func (c *Client) SetOrganization(organizationID string) {
	c.mu.Lock()
	c.orgID = organizationID
	c.mu.Unlock()
}

// do runs one JSON request. in may be nil for no body; out may be nil to
// discard the response body.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("metis: encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("metis: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.mu.RLock()
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.orgID != "" {
		req.Header.Set("X-Organization-ID", c.orgID)
	}
	c.mu.RUnlock()

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("metis: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	// Bounded read: an error page should not become an unbounded allocation.
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("metis: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return &APIError{StatusCode: resp.StatusCode, Message: serverMessage(payload)}
	}
	if out == nil || len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("metis: decode response: %w", err)
	}
	return nil
}

// serverMessage extracts the server's {"error": "..."} body, falling back to
// the raw text so a proxy's HTML error page is still visible, just truncated.
func serverMessage(payload []byte) string {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(payload, &body); err == nil && body.Error != "" {
		return body.Error
	}
	text := strings.TrimSpace(string(payload))
	if len(text) > 300 {
		text = text[:300] + "…"
	}
	if text == "" {
		return "(no body)"
	}
	return text
}
