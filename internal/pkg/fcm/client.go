package fcm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gsoultan/metis/internal/pkg/httpclient"
)

// GoogleSendURL is FCM's HTTP v1 send endpoint; %s is the Firebase project ID.
const GoogleSendURL = "https://fcm.googleapis.com/v1/projects/%s/messages:send"

// Client sends pushes for one Firebase service account.
type Client struct {
	account ServiceAccount
	sendURL string
	http    *http.Client
	tokens  *tokenSource
}

// Option adjusts a Client.
type Option func(*Client)

// WithEndpoints points the client somewhere other than Google. It exists so a
// test can stand a fake Firebase up on a local server; nothing a user writes
// reaches it.
func WithEndpoints(tokenURL, sendURL string, client *http.Client) Option {
	return func(c *Client) {
		c.tokens.tokenURL, c.sendURL, c.http, c.tokens.client = tokenURL, sendURL, client, client
	}
}

// NewClient returns a client for account. It signs in lazily, on the first send.
//
// The shared outbound client is used so push is held to the same egress policy
// and timeouts as every other call the server makes.
func NewClient(account ServiceAccount, opts ...Option) *Client {
	shared := httpclient.Shared()
	c := &Client{
		account: account,
		sendURL: fmt.Sprintf(GoogleSendURL, url.PathEscape(account.ProjectID)),
		http:    shared,
		tokens:  &tokenSource{account: account, tokenURL: GoogleTokenURL, client: shared, now: time.Now},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ProjectID is the Firebase project the client sends for.
func (c *Client) ProjectID() string { return c.account.ProjectID }

type sendResponse struct {
	Name string `json:"name"`
}

// Send delivers one message and returns the ID Firebase gave it.
//
// The error wraps ErrUnregistered when the device is gone, ErrCredentials when
// the service account was refused, and ErrRejected when Firebase refused the
// message itself; anything else is worth retrying later.
func (c *Client) Send(ctx context.Context, msg Message) (string, error) {
	if msg.Token == "" {
		return "", fmt.Errorf("%w: it names no device token", ErrRejected)
	}
	body, err := json.Marshal(envelopeFor(msg))
	if err != nil {
		return "", fmt.Errorf("fcm: encode the message: %w", err)
	}
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return "", err
	}
	status, answer, err := c.post(ctx, token, body)
	if err != nil {
		return "", err
	}
	if status == http.StatusUnauthorized {
		c.tokens.forget()
	}
	if status != http.StatusOK {
		return "", sendError(status, answer)
	}
	var sent sendResponse
	if err := json.Unmarshal(answer, &sent); err != nil {
		return "", fmt.Errorf("fcm: read Firebase's answer: %w", err)
	}
	return sent.Name, nil
}

func (c *Client) post(ctx context.Context, token string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sendURL, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("fcm: build the send request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("fcm: send to Firebase: %w", err)
	}
	defer closeBody(resp)
	answer, err := httpclient.ReadResponseBody(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("fcm: read Firebase's answer: %w", err)
	}
	return resp.StatusCode, answer, nil
}
