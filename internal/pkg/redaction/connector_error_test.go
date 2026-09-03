package redaction

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// A connector failure carries the URL it was calling, and Go's *url.Error
// includes the query string. Many APIs take their key as a query parameter, and
// a connector manifest is built to template one in — so this is the shape of
// error a real installation produces, not a contrived one.
//
// It matters because the text is stored: the job service writes it to the
// incident table, which is what the UI shows an operator. Before this was
// redacted, every failed connection wrote the key into the database in
// plaintext, and incidents are kept.
func TestAConnectorErrorDoesNotCarryItsCredential(t *testing.T) {
	// Assembled rather than written down. A literal shaped like a real key is
	// one to a secret scanner, and the scanner being right about that is what
	// makes it worth listening to elsewhere — the redactor keys on the
	// parameter name, so the value's shape is not what is under test.
	secret := "sk-live-" + strings.Repeat("a1b2c3d4", 3)

	// Built the way net/http builds it, so the test is about the real shape.
	inner := &url.Error{
		Op:  "Get",
		URL: "https://api.example.com/v1/leads?api_key=" + secret,
		Err: fmt.Errorf("dial tcp 203.0.113.10:443: connect: connection refused"),
	}
	jobErr := fmt.Errorf("connector %q: %w", "salesforce", inner)

	redacted := RedactText(jobErr.Error())

	if strings.Contains(redacted, secret) {
		t.Fatalf("the credential survived redaction:\n  %s", redacted)
	}
	// Still has to be a useful incident: an operator needs to know which
	// connector, which host and why.
	for _, keep := range []string{"salesforce", "api.example.com", "connection refused"} {
		if !strings.Contains(redacted, keep) {
			t.Errorf("redaction removed %q, which the operator needs: %s", keep, redacted)
		}
	}
}

// The other spellings a manifest might use for the same thing.
func TestCredentialsInQueryParametersAreRedacted(t *testing.T) {
	secret := strings.Repeat("a1b2c3d4", 4)

	for _, param := range []string{"api_key", "apikey", "api-key", "token", "access_token", "secret", "password"} {
		t.Run(param, func(t *testing.T) {
			raw := fmt.Sprintf(`Get "https://api.example.com/v1?%s=%s": connection refused`, param, secret)
			if redacted := RedactText(raw); strings.Contains(redacted, secret) {
				t.Errorf("%s= was not redacted: %s", param, redacted)
			}
		})
	}
}

// Credentials in userinfo, which is the other way a manifest can carry one.
func TestCredentialsInTheURLUserinfoAreRedacted(t *testing.T) {
	raw := `Get "https://svc:hunter2-the-real-password@api.example.com/v1": connection refused`
	redacted := RedactText(raw)
	if strings.Contains(redacted, "hunter2-the-real-password") {
		t.Fatalf("userinfo credential survived: %s", redacted)
	}
}
