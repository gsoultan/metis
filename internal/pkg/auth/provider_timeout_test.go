package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// An identity provider that accepts the connection and never answers held the
// server's boot for good: the validator is built at boot, and fetching the
// provider's configuration had no deadline. The same client fetches the
// provider's keys when a token names one not seen yet, so every request
// needing a new key would have queued behind it too.
func TestAnIdentityProviderThatNeverAnswersDoesNotHoldBoot(t *testing.T) {
	previous := providerTimeout
	providerTimeout = 200 * time.Millisecond
	t.Cleanup(func() { providerTimeout = previous })

	answered := make(chan struct{})
	silent := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-answered }))
	t.Cleanup(func() {
		close(answered)
		silent.Close()
	})

	built := make(chan error, 1)
	go func() {
		_, err := NewTokenValidator(context.Background(), silent.URL, "metis")
		built <- err
	}()

	select {
	case err := <-built:
		if err == nil {
			t.Fatal("a validator was built from an identity provider that never answered")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("building the validator was still waiting on an identity provider that never answers, after 5s")
	}
}
