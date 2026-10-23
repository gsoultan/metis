package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// client speaks to one server as one person, from one address.
//
// forwardedFor is what makes a thousand virtual users a thousand users rather
// than one very busy one. The server's rate limit is per client address, and a
// load generator on one host is one address: without it every virtual user
// shares a single 240-a-minute allowance and the run measures the limiter.
// Loopback is a trusted proxy by default, so the server believes the header
// exactly as it would believe a load balancer's.
type client struct {
	http         *http.Client
	base         string
	token        string
	forwardedFor string
}

const errorBodyLimit = 300

// call sends body as JSON and decodes the answer into out, which may be nil.
func (c *client) call(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding %s: %w", path, err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", c.forwardedFor)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return &apiError{status: resp.StatusCode, body: truncate(raw), retryAfter: retryAfter(resp.Header)}
	}
	return decodeAnswer(raw, out)
}

// decodeAnswer reads a 200 that may still carry a refusal: the Connect and Go
// Kit handlers both answer some refusals with an "error" field and a 200.
func decodeAnswer(raw []byte, out any) error {
	var refused struct {
		Error string `json:"error"`
		Err   string `json:"err"`
	}
	if json.Unmarshal(raw, &refused) == nil && (refused.Error != "" || refused.Err != "") {
		return &apiError{status: http.StatusOK, body: refused.Error + refused.Err}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// retryAfter reads the seconds form of Retry-After, the only one the server sends.
func retryAfter(header http.Header) time.Duration {
	seconds, err := strconv.Atoi(header.Get("Retry-After"))
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func truncate(raw []byte) string {
	if len(raw) > errorBodyLimit {
		return string(raw[:errorBodyLimit])
	}
	return string(raw)
}

// newTransport keeps a connection per virtual user open between its calls, as
// a browser or SDK client would. The default of two idle connections per host
// would have the generator open and close sockets and measure that instead.
func newTransport(concurrency int) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = concurrency
	transport.MaxIdleConnsPerHost = concurrency
	transport.MaxConnsPerHost = 0
	transport.IdleConnTimeout = time.Minute
	return transport
}
