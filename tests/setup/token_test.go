package setup_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// testSetupToken is the token the tests' setup services are built with.
const testSetupToken = "a-setup-token-for-the-tests-0123456789abcdef"

// Setup is anonymous by necessity, so whoever reached a fresh installation
// first became its administrator. The token, printed only in the server's log,
// is what makes the wizard the operator's.
func TestSetupWithoutTheTokenIsRefusedBeforeAnythingIsTouched(t *testing.T) {
	asWizard(t)
	t.Chdir(t.TempDir())
	svc := impl.NewSetupService(heldInstallation(false), testSetupToken)

	for name, token := range map[string]string{"missing": "", "wrong": "not-the-token"} {
		err := svc.Setup(t.Context(), contracts.SetupRequest{
			SetupToken:    token,
			AdminUsername: "intruder", AdminPassword: "intruder-password", AdminFullName: "I",
			AdminPublicName: "I", OrganizationName: "Mine now", DatabaseDriver: "postgres",
			// A database nobody can reach: a refusal that came from trying it
			// would be the wrong refusal.
			DBHost: "127.0.0.1", DBPort: 1, DBName: "probe",
		})
		if !errors.Is(err, impl.ErrSetupTokenRequired) || !errors.Is(err, apierr.ErrForbidden) {
			t.Fatalf("%s token: setup must be refused as forbidden, got %v", name, err)
		}
	}
}

// Before setup the connection test is a network probe that reports what it
// found at any address. Without the token it must not dial anything.
func TestTheConnectionTestWithoutTheTokenDialsNothing(t *testing.T) {
	asWizard(t)
	t.Chdir(t.TempDir())
	svc := impl.NewSetupService(nil, testSetupToken)

	result := svc.TestConnection(t.Context(), contracts.TestConnectionRequest{
		DatabaseDriver: "postgres", DBHost: "127.0.0.1", DBPort: 1, DBName: "probe",
	})
	if result.Success {
		t.Fatal("a connection test without the token succeeded")
	}
	if strings.Contains(result.Message, "refused") || strings.Contains(result.Message, "127.0.0.1") {
		t.Fatalf("the refusal must not say what it found at the address, got %q", result.Message)
	}
	if !strings.Contains(result.Message, "setup token") {
		t.Fatalf("the refusal should say what is missing, got %q", result.Message)
	}
}

// A chosen token may be weaker than a generated one, so guessing is bounded:
// past the limit even the right token waits.
func TestGuessingTheTokenIsThrottled(t *testing.T) {
	asWizard(t)
	t.Chdir(t.TempDir())
	svc := impl.NewSetupService(nil, testSetupToken)
	probe := func(token string) contracts.TestConnectionResult {
		return svc.TestConnection(t.Context(), contracts.TestConnectionRequest{SetupToken: token})
	}

	for range 10 {
		probe("a-guess")
	}
	result := probe(testSetupToken)
	if !strings.Contains(result.Message, "too many wrong setup tokens") {
		t.Fatalf("the right token was compared after ten wrong ones in a minute, got %q", result.Message)
	}
}

// A chosen token too weak to be a secret closes the wizard rather than leaving
// it open with no token at all.
func TestAWeakChosenTokenClosesTheWizard(t *testing.T) {
	asWizard(t)
	t.Chdir(t.TempDir())
	t.Setenv("METIS_ALLOW_WEAK_SECRETS", "")
	svc := impl.NewSetupService(nil, "short")

	result := svc.TestConnection(t.Context(), contracts.TestConnectionRequest{SetupToken: "short"})
	if result.Success || !strings.Contains(result.Message, impl.EnvSetupToken) {
		t.Fatalf("a weak chosen token must be refused by name, got %+v", result)
	}
}

// With the token, the wizard does what it always did.
func TestTheRightTokenOpensTheConnectionTest(t *testing.T) {
	asWizard(t)
	t.Chdir(t.TempDir())
	svc := impl.NewSetupService(nil, testSetupToken)

	result := svc.TestConnection(t.Context(), contracts.TestConnectionRequest{SetupToken: testSetupToken})
	if result.Message != "Database driver is required" {
		t.Fatalf("with the token the request should reach the wizard's own checks, got %q", result.Message)
	}
}
