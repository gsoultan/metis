package setup_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// A server that started with no database serves only the wizard. Setup writes
// the database and the server's address into config.yaml, and then tells the
// server, which starts on it without anybody restarting it.
func TestAWizardOnlySetupWritesTheServerSectionAndHandsOver(t *testing.T) {
	asWizard(t)
	host, port, user, password := postgresCredentials(t)
	database := "metis_setup_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	admin := createDatabase(t, host, port, user, password, database)
	defer dropDatabase(t, admin, database)
	t.Chdir(t.TempDir())

	handedOver := 0
	svc := impl.NewSetupService(nil, testSetupToken, impl.WizardOnly(func() { handedOver++ }))

	status, err := svc.GetSetupStatus(t.Context())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.WizardOnly || status.IsInitialized {
		t.Fatalf("a server serving only the wizard should say so, got %+v", status)
	}

	err = svc.Setup(t.Context(), contracts.SetupRequest{
		SetupToken:    testSetupToken,
		AdminUsername: "admin", AdminPassword: "correct-horse-battery-staple",
		AdminFullName: "Admin", AdminPublicName: "Admin", AdminEmail: "admin@example.invalid",
		OrganizationName: "Example Co", DatabaseDriver: "postgres",
		DBHost: host, DBPort: port, DBUsername: user, DBPassword: password, DBName: database,
		EncryptionKey: strongSecret(t), JWTSecret: strongSecret(t),
		HTTPAddress: " 0.0.0.0:8080 ", PublicURL: "https://metis.example.com/",
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if handedOver != 1 {
		t.Fatalf("the server was told %d times that setup finished; it waits for exactly one", handedOver)
	}

	cfg, err := config.Load(config.DefaultConfigPath)
	if err != nil {
		t.Fatalf("setup wrote a config.yaml the server cannot read: %v", err)
	}
	// Written as the server will read them: trimmed, and without the slash a
	// link would otherwise double.
	want := config.ServerConfig{HTTPAddress: "0.0.0.0:8080", PublicURL: "https://metis.example.com"}
	if cfg.Server != want {
		t.Fatalf("server section = %+v, want %+v", cfg.Server, want)
	}
}

// An address the server could not use is refused before the database is
// touched: a config.yaml naming one is a server that does not come back.
func TestSetupRefusesAServerAddressItCouldNotUse(t *testing.T) {
	asWizard(t)
	t.Chdir(t.TempDir())

	for _, bad := range []contracts.SetupRequest{
		{HTTPAddress: "8080"},
		{HTTPAddress: ":70000"},
		{PublicURL: "metis.example.com"},
		{PublicURL: "https://metis.example.com/?next=x"},
	} {
		handedOver := false
		svc := impl.NewSetupService(nil, testSetupToken, impl.WizardOnly(func() { handedOver = true }))
		req := contracts.SetupRequest{
			SetupToken:    testSetupToken,
			AdminUsername: "admin", AdminPassword: "correct-horse-battery-staple",
			AdminFullName: "Admin", AdminPublicName: "Admin", AdminEmail: "admin@example.invalid",
			OrganizationName: "Example Co", DatabaseDriver: "postgres",
			// Nothing listens here: reaching the database would be a failure
			// of its own, and the refusal must come first.
			DBHost: "127.0.0.1", DBPort: 1, DBUsername: "metis", DBName: "metis",
			EncryptionKey: strongSecret(t), JWTSecret: strongSecret(t),
			HTTPAddress: bad.HTTPAddress, PublicURL: bad.PublicURL,
		}
		err := svc.Setup(t.Context(), req)
		if !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Errorf("%+v: want an invalid-argument refusal, got %v", bad, err)
		}
		if handedOver || config.Exists(config.DefaultConfigPath) {
			t.Errorf("%+v: a refused setup wrote its configuration or handed over", bad)
		}
	}
}
