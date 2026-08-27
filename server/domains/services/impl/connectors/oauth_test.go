package connectors

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// tokenServer stands in for an identity provider, counting how often it is
// asked — which is the property most of these tests are about.
func tokenServer(t *testing.T, expiresIn int64, handler func(r *http.Request)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var issued atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := issued.Add(1)
		if handler != nil {
			handler(r)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"token-%d","token_type":"Bearer","expires_in":%d}`, n, expiresIn)
	}))
	t.Cleanup(server.Close)
	return server, &issued
}

func credsFor(url string) clientCredentials {
	return clientCredentials{tokenURL: url, clientID: "id", clientSecret: "secret", scopes: []string{"read", "write"}}
}

// A token is issued for minutes or hours. Fetching one per connector call would
// double every outbound request and put the identity provider on the critical
// path of every process step.
func TestTokenIsCachedBetweenCalls(t *testing.T) {
	server, issued := tokenServer(t, 3600, nil)
	cache := NewTokenCache(server.Client())

	for range 5 {
		token, err := cache.Token(t.Context(), credsFor(server.URL))
		if err != nil {
			t.Fatalf("token: %v", err)
		}
		if token != "token-1" {
			t.Fatalf("got %q, want the first token reused", token)
		}
	}
	if got := issued.Load(); got != 1 {
		t.Fatalf("the provider was asked %d times for 5 calls; the token is not being cached", got)
	}
}

// When a token expires every worker with a call in flight notices at once.
// Without coordination they all fetch — the thundering herd the cache exists to
// prevent.
func TestConcurrentCallersFetchOnce(t *testing.T) {
	server, issued := tokenServer(t, 3600, func(*http.Request) {
		// Slow enough that the other callers genuinely arrive mid-fetch.
		time.Sleep(80 * time.Millisecond)
	})
	cache := NewTokenCache(server.Client())

	const callers = 8
	tokens := make([]string, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			token, err := cache.Token(t.Context(), credsFor(server.URL))
			if err != nil {
				t.Errorf("caller %d: %v", i, err)
				return
			}
			tokens[i] = token
		}()
	}
	close(start)
	wg.Wait()

	if got := issued.Load(); got != 1 {
		t.Fatalf("%d concurrent callers caused %d token fetches, want 1", callers, got)
	}
	for i, token := range tokens {
		if token != tokens[0] {
			t.Fatalf("caller %d got %q while caller 0 got %q", i, token, tokens[0])
		}
	}
}

// An expiring token must be renewed before it is refused, not after: the call
// it authorises may outlive the last seconds of its validity.
func TestTokenIsRenewedBeforeItExpires(t *testing.T) {
	server, issued := tokenServer(t, 3600, nil)
	cache := NewTokenCache(server.Client())

	now := time.Now()
	cache.now = func() time.Time { return now }

	if _, err := cache.Token(t.Context(), credsFor(server.URL)); err != nil {
		t.Fatalf("first token: %v", err)
	}

	// Move to inside the refresh margin of the hour-long token.
	now = now.Add(time.Hour - (tokenRefreshMargin / 2))
	token, err := cache.Token(t.Context(), credsFor(server.URL))
	if err != nil {
		t.Fatalf("second token: %v", err)
	}
	if token != "token-2" {
		t.Fatalf("got %q; a token inside its refresh margin was reused rather than renewed", token)
	}
	if got := issued.Load(); got != 2 {
		t.Fatalf("provider asked %d times, want 2", got)
	}
}

// A provider issuing a very short token must not produce one that is already
// expired the moment it arrives, or every call fetches a new one forever.
func TestAVeryShortTokenIsStillUsable(t *testing.T) {
	server, issued := tokenServer(t, 10, nil) // shorter than the refresh margin
	cache := NewTokenCache(server.Client())

	if _, err := cache.Token(t.Context(), credsFor(server.URL)); err != nil {
		t.Fatalf("first token: %v", err)
	}
	if _, err := cache.Token(t.Context(), credsFor(server.URL)); err != nil {
		t.Fatalf("second token: %v", err)
	}
	if got := issued.Load(); got != 1 {
		t.Fatalf("a 10-second token was refetched immediately (%d fetches); the margin pushed its expiry into the past", got)
	}
}

// Rotating the secret must stop serving tokens minted with the old one.
func TestRotatingTheSecretIssuesANewToken(t *testing.T) {
	server, issued := tokenServer(t, 3600, nil)
	cache := NewTokenCache(server.Client())

	if _, err := cache.Token(t.Context(), credsFor(server.URL)); err != nil {
		t.Fatalf("first: %v", err)
	}
	rotated := credsFor(server.URL)
	rotated.clientSecret = "rotated"
	token, err := cache.Token(t.Context(), rotated)
	if err != nil {
		t.Fatalf("after rotation: %v", err)
	}
	if token == "token-1" {
		t.Fatal("the rotated secret reused the token minted with the old one")
	}
	if got := issued.Load(); got != 2 {
		t.Fatalf("provider asked %d times, want 2", got)
	}
}

// The grant must be the one the RFC defines, with credentials in the header —
// which keeps the secret out of anything that logs request bodies.
func TestTheGrantIsClientCredentials(t *testing.T) {
	var (
		grant, scope string
		hasBasicAuth bool
		body         string
	)
	server, _ := tokenServer(t, 3600, func(r *http.Request) {
		_ = r.ParseForm()
		grant = r.Form.Get("grant_type")
		scope = r.Form.Get("scope")
		_, _, hasBasicAuth = r.BasicAuth()
		body = r.Form.Encode()
	})

	cache := NewTokenCache(server.Client())
	if _, err := cache.Token(t.Context(), credsFor(server.URL)); err != nil {
		t.Fatalf("token: %v", err)
	}

	if grant != "client_credentials" {
		t.Errorf("grant_type = %q, want client_credentials", grant)
	}
	if scope != "read write" {
		t.Errorf("scope = %q, want the manifest's scopes space-separated", scope)
	}
	if !hasBasicAuth {
		t.Error("credentials were not sent in the Authorization header")
	}
	if strings.Contains(body, "secret") {
		t.Errorf("the client secret was sent in the request body: %q", body)
	}
}

// A refusal must say what to fix without echoing back a body that can carry the
// credentials it was sent.
func TestARefusedGrantIsReportedWithoutEchoingTheProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client","client_secret":"super-secret"}`))
	}))
	t.Cleanup(server.Close)

	cache := NewTokenCache(server.Client())
	_, err := cache.Token(t.Context(), credsFor(server.URL))
	if err == nil {
		t.Fatal("a refused grant reported success")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("the error echoed the provider's body, leaking a credential: %v", err)
	}
	if !strings.Contains(err.Error(), "client id") {
		t.Errorf("the error does not say what to check: %v", err)
	}
}

func TestCredentialsFromRequiresWhatTheGrantNeeds(t *testing.T) {
	testCases := []struct {
		name   string
		auth   Auth
		config map[string]any
		want   string
	}{
		{"no token url", Auth{Type: authOAuth2ClientCredentials},
			map[string]any{"client_id": "a", "client_secret": "b"}, "token URL"},
		{"no client id", Auth{Type: authOAuth2ClientCredentials, TokenURL: "https://e.com/t"},
			map[string]any{"client_secret": "b"}, "client_id"},
		{"no client secret", Auth{Type: authOAuth2ClientCredentials, TokenURL: "https://e.com/t"},
			map[string]any{"client_id": "a"}, "client_secret"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := credentialsFrom(tc.auth, tc.config)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one naming %q", err, tc.want)
			}
		})
	}
}

// The token URL comes from the manifest, never from configuration: otherwise
// whoever configures an instance could redirect the credentials to a host of
// their choosing.
func TestTheTokenURLCannotBeOverriddenByConfiguration(t *testing.T) {
	creds, err := credentialsFrom(
		Auth{Type: authOAuth2ClientCredentials, TokenURL: "https://issuer.example/token"},
		map[string]any{"client_id": "a", "client_secret": "b", "token_url": "https://attacker.example/token"},
	)
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	if creds.tokenURL != "https://issuer.example/token" {
		t.Fatalf("token URL is %q; configuration redirected the credentials", creds.tokenURL)
	}
}
