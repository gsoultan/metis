package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// tokenRefreshMargin is how early a token is treated as expired.
//
// A token that is valid for another two seconds is not worth sending: the call
// it authorises may take longer than that, and the partner would reject it
// after the work of building and dispatching the request. Renewing slightly
// early costs one extra fetch per token lifetime and removes a class of
// intermittent 401 that is miserable to diagnose.
const tokenRefreshMargin = 30 * time.Second

// defaultTokenLifetime is used when a provider returns no expires_in.
//
// Short on purpose. Guessing long risks sending a dead token for an hour;
// guessing short costs an occasional extra fetch, which is the cheaper mistake.
const defaultTokenLifetime = 5 * time.Minute

// tokenRequestTimeout bounds the token fetch itself, which is a network call to
// somebody else's identity provider and must not be able to hold a job worker.
const tokenRequestTimeout = 20 * time.Second

// cachedToken is one issued token and the moment it stops being usable.
type cachedToken struct {
	value     string
	expiresAt time.Time
}

// TokenCache issues and caches OAuth client-credentials tokens.
//
// Caching is the whole point. A token is issued for minutes or hours, and
// fetching one per connector call would double every outbound request, add the
// identity provider to the critical path of every process step, and — for
// providers that rate-limit token issuance, which is most of them — eventually
// get the installation throttled on authentication rather than on work.
//
// Concurrency matters for the same reason: when a token expires, every worker
// with a call in flight notices at once. Without coordination they all fetch,
// which is the thundering herd the cache exists to prevent. Each key's fetch is
// therefore performed once, under its own lock, and the others use the result.
type TokenCache struct {
	client *http.Client
	now    func() time.Time

	mu     sync.Mutex
	tokens map[string]*tokenEntry
}

// tokenEntry is a cache slot with its own lock, so a fetch for one credential
// does not block reads of another.
type tokenEntry struct {
	mu    sync.Mutex
	token cachedToken
}

// NewTokenCache returns a cache issuing tokens through client.
func NewTokenCache(client *http.Client) *TokenCache {
	return &TokenCache{client: client, now: time.Now, tokens: make(map[string]*tokenEntry)}
}

// clientCredentials is what a manifest's configuration must supply.
type clientCredentials struct {
	tokenURL     string
	clientID     string
	clientSecret string
	scopes       []string
	audience     string
}

// cacheKey identifies one credential at one provider.
//
// The secret is deliberately part of it: rotating the secret must not keep
// serving tokens minted with the old one. The key never leaves this process and
// is never logged.
func (c clientCredentials) cacheKey() string {
	return strings.Join([]string{c.tokenURL, c.clientID, c.clientSecret, c.audience, strings.Join(c.scopes, " ")}, "\x00")
}

// Token returns a usable access token, fetching one only when what is cached
// has expired or is about to.
func (t *TokenCache) Token(ctx context.Context, creds clientCredentials) (string, error) {
	key := creds.cacheKey()

	t.mu.Lock()
	entry, ok := t.tokens[key]
	if !ok {
		entry = &tokenEntry{}
		t.tokens[key] = entry
	}
	t.mu.Unlock()

	entry.mu.Lock()
	defer entry.mu.Unlock()

	// Re-checked while holding the entry lock: a caller that queued behind a
	// fetch must use its result rather than immediately fetching again.
	if entry.token.value != "" && t.now().Before(entry.token.expiresAt) {
		return entry.token.value, nil
	}

	token, err := t.fetch(ctx, creds)
	if err != nil {
		return "", err
	}
	entry.token = token
	return token.value, nil
}

// fetch performs the client-credentials grant.
func (t *TokenCache) fetch(ctx context.Context, creds clientCredentials) (cachedToken, error) {
	form := url.Values{"grant_type": {"client_credentials"}}
	if len(creds.scopes) > 0 {
		form.Set("scope", strings.Join(creds.scopes, " "))
	}
	if creds.audience != "" {
		// Not part of the RFC, but Auth0 and others require it and simply issue
		// a useless token without it.
		form.Set("audience", creds.audience)
	}

	ctx, cancel := context.WithTimeout(ctx, tokenRequestTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, creds.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return cachedToken{}, fmt.Errorf("build the token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	// Credentials go in the Authorization header rather than the body: RFC 6749
	// §2.3.1 requires servers to support this form, and it keeps the secret out
	// of anything that logs request bodies.
	request.SetBasicAuth(url.QueryEscape(creds.clientID), url.QueryEscape(creds.clientSecret))

	client := t.client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return cachedToken{}, fmt.Errorf("request a token from the identity provider: %w", err)
	}
	defer closeQuietly(response.Body, "oauth token")

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return cachedToken{}, fmt.Errorf("read the token response: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// The provider's body is not echoed: it can carry the client id, and on
		// some providers the secret it was sent. The status is what an operator
		// acts on, and the connector's own configuration is where to look.
		return cachedToken{}, fmt.Errorf(
			"the identity provider refused these credentials (HTTP %d); check the connector's client id, secret and scopes",
			response.StatusCode)
	}

	var payload struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return cachedToken{}, fmt.Errorf("the identity provider's reply was not JSON: %w", err)
	}
	if payload.AccessToken == "" {
		return cachedToken{}, fmt.Errorf("the identity provider returned no access_token")
	}

	lifetime := defaultTokenLifetime
	if payload.ExpiresIn > 0 {
		lifetime = time.Duration(payload.ExpiresIn) * time.Second
	}
	// Never let the margin push expiry into the past: a provider issuing a
	// 10-second token would otherwise produce a token already considered dead,
	// and every call would fetch a new one.
	if lifetime <= tokenRefreshMargin {
		lifetime /= 2
	} else {
		lifetime -= tokenRefreshMargin
	}

	return cachedToken{value: payload.AccessToken, expiresAt: t.now().Add(lifetime)}, nil
}

// credentialsFrom reads what the client-credentials grant needs out of a
// manifest and the tenant's configuration.
func credentialsFrom(auth Auth, config map[string]any) (clientCredentials, error) {
	clientID, _ := textSetting(config, "client_id")
	clientSecret, _ := textSetting(config, "client_secret")
	audience, _ := textSetting(config, "audience")

	// The token URL comes from the manifest, which is the connector's author
	// describing the API. Letting configuration override it would let whoever
	// configures an instance redirect the credentials to a host of their
	// choosing.
	tokenURL := strings.TrimSpace(auth.TokenURL)

	switch {
	case tokenURL == "":
		return clientCredentials{}, fmt.Errorf("this connector asks for OAuth but names no token URL")
	case clientID == "":
		return clientCredentials{}, fmt.Errorf("this connector authenticates with OAuth, and no client_id is configured")
	case clientSecret == "":
		return clientCredentials{}, fmt.Errorf("this connector authenticates with OAuth, and no client_secret is configured")
	}

	scopes := auth.Scopes
	// A scope configured on the instance narrows what the token can do, which is
	// a tenant's decision to make.
	if configured, ok := textSetting(config, "scope"); ok && strings.TrimSpace(configured) != "" {
		scopes = strings.Fields(configured)
	}

	return clientCredentials{
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		scopes:       scopes,
		audience:     audience,
	}, nil
}
