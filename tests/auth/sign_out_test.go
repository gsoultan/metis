package auth_test

import (
	"net/http"
	"testing"
	"time"
)

// Signing out ends the session on the server, not only in the browser.
//
// Until it did, signing out cleared the token from the one browser it was in,
// and the token itself went on working for the rest of its twenty-four hours:
// on another device, in a copied header, or in the hands of somebody who had
// read it off a shared machine. Signing out ends every session the account
// has, and signing in again afterwards works.
func TestSigningOutEndsTheSessionOnTheServer(t *testing.T) {
	h := newLocalHarness(t)
	org := h.organization("Acme")
	h.localAccount("hopper", "hopper@acme.example", org)

	here := h.login("hopper")
	elsewhere := h.login("hopper")
	h.me(here)
	h.me(elsewhere)

	if status, body := h.do(http.MethodPost, "/api/v1/logout", here, "", nil); status != http.StatusOK {
		t.Fatalf("sign out: status %d (%s), want 200", status, body)
	}

	for name, token := range map[string]string{"this device": here, "another device": elsewhere} {
		if status, body := h.get("/api/v1/users/me", token, ""); status != http.StatusUnauthorized {
			t.Errorf("%s's token after signing out: status %d (%s), want 401", name, status, body)
		}
	}

	// A token's issued-at is in whole seconds and the cutoff is the start of
	// the next one, so signing in again is let through from then on.
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)) + 10*time.Millisecond)
	if got := h.me(h.login("hopper")); got.Username != "hopper" {
		t.Fatalf("signed back in as %q, want hopper", got.Username)
	}
}

// Signing out needs a session to end.
func TestSigningOutWithoutASessionIsRefused(t *testing.T) {
	h := newLocalHarness(t)
	if status, body := h.do(http.MethodPost, "/api/v1/logout", "", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("sign out with no token: status %d (%s), want 401", status, body)
	}
}
