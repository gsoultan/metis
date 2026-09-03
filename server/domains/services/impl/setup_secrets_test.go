package impl

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// The wizard and the boot path have to agree about what counts as a secret.
//
// They did not. The wizard applied a rule of its own — sixteen characters for
// the encryption key, and nothing at all for the JWT secret beyond being
// present — while startup refused anything under thirty-two. So a wizard run
// could report success, write the config, and leave a server that refused to
// start on the next restart: configured successfully and permanently unable to
// come back, which is a worse outcome than the weak secret it was trying to
// prevent.
//
// Measured on the previous build: setup returned {} for a sixteen-character key
// and a JWT secret of "secret", and the next start died with "Refusing to
// start".
func TestSetupRefusesSecretsTheServerWillNotStartWith(t *testing.T) {
	cases := []struct {
		name          string
		encryptionKey string
		jwtSecret     string
		wants         string
	}{
		{
			name:          "the key length the wizard used to allow",
			encryptionKey: strings.Repeat("a", 16),
			jwtSecret:     strings.Repeat("s", 40),
			wants:         "encryption key",
		},
		{
			name:          "a JWT secret the wizard used not to check at all",
			encryptionKey: strings.Repeat("a1b2c3d4", 5),
			jwtSecret:     "secret",
			wants:         "JWT secret",
		},
		{
			name:          "a placeholder published in this repository",
			encryptionKey: strings.Repeat("0123456789abcdef", 2),
			jwtSecret:     strings.Repeat("s", 40),
			wants:         "encryption key",
		},
		{
			name:          "long but repetitive, which every length rule accepts",
			encryptionKey: strings.Repeat("a", 64),
			jwtSecret:     strings.Repeat("s", 40),
			wants:         "encryption key",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateSetupRequest(validRequestWith(testCase.encryptionKey, testCase.jwtSecret))
			if err == nil {
				t.Fatal("setup accepted a secret the server refuses to start with: the installation would configure and then never restart")
			}
			if !strings.Contains(err.Error(), testCase.wants) {
				t.Errorf("the message does not name %q: %v", testCase.wants, err)
			}
		})
	}
}

// What the documentation tells operators to generate has to be accepted, or the
// rule is one nobody can satisfy.
func TestSetupAcceptsGeneratedSecrets(t *testing.T) {
	// Generated rather than written down. A literal of the right shape is
	// indistinguishable from a real key to a secret scanner, and a test fixture
	// is not worth teaching it to ignore — the scanner being right about this
	// is what makes it useful about everything else.
	req := validRequestWith(randomHex(24), randomHex(32))
	if err := validateSetupRequest(req); err != nil {
		t.Fatalf("setup refused secrets from the documented commands: %v", err)
	}
}

// The escape hatch has to work here too. If it did not, the two paths would
// disagree again — in the other direction.
func TestSetupHonoursTheWeakSecretOverride(t *testing.T) {
	t.Setenv("METIS_ALLOW_WEAK_SECRETS", "true")

	req := validRequestWith(strings.Repeat("a", 16), "secret")
	if err := validateSetupRequest(req); err != nil {
		t.Fatalf("the override did not apply to setup: %v", err)
	}
}

// A missing secret is still refused with the plain message, override or not:
// there is no reading of "allow weak" that means "allow absent".
func TestSetupStillRequiresSecretsUnderTheOverride(t *testing.T) {
	t.Setenv("METIS_ALLOW_WEAK_SECRETS", "true")

	if err := validateSetupRequest(validRequestWith("", "secret")); err == nil {
		t.Fatal("setup accepted an empty encryption key")
	}
	if err := validateSetupRequest(validRequestWith(strings.Repeat("a", 40), "")); err == nil {
		t.Fatal("setup accepted an empty JWT secret")
	}
}

func validRequestWith(encryptionKey, jwtSecret string) contracts.SetupRequest {
	return contracts.SetupRequest{
		AdminUsername:    "admin",
		AdminPassword:    "a-sufficiently-long-password",
		AdminFullName:    "Admin",
		AdminPublicName:  "Admin",
		OrganizationName: "Org",
		DatabaseDriver:   "sqlite",
		EncryptionKey:    encryptionKey,
		JWTSecret:        jwtSecret,
	}
}

// randomHex returns n bytes of randomness as hex, which is what
// `openssl rand -hex n` produces and what the documentation recommends.
func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(buf)
}
