package contracts

import (
	"context"
)

// SetupStatus represents the current initialization state of the system.
type SetupStatus struct {
	IsInitialized bool `json:"is_initialized"`

	// ConfiguredByEnvironment says the server's environment already names its
	// database and both secrets, so the wizard asks only for the organization
	// and the first administrator. It writes no file in that case — the
	// container deployments run on a read-only root, where it could not.
	ConfiguredByEnvironment bool `json:"configured_by_environment"`

	// WizardOnly says the server started with no database at all and is
	// serving nothing but this wizard. Once setup has saved the
	// configuration it moves onto it by itself, with no restart.
	WizardOnly bool `json:"wizard_only"`
}

// InstallationProbe answers whether the database a server runs on already
// holds an installation.
type InstallationProbe interface {
	HasAccounts(ctx context.Context) (bool, error)
}

// SetupRequest contains the initial configuration data for the system.
type SetupRequest struct {
	AdminUsername    string `json:"admin_username"`
	AdminPassword    string `json:"admin_password"`
	AdminFullName    string `json:"admin_full_name"`
	AdminPublicName  string `json:"admin_public_name"`
	AdminEmail       string `json:"admin_email"`
	OrganizationName string `json:"organization_name"`
	ProjectName      string `json:"project_name"`

	// SetupToken proves the caller is the installation's operator: it is
	// printed in the server's log, or chosen with METIS_SETUP_TOKEN.
	SetupToken string `json:"setup_token"`

	// Database configuration
	DatabaseDriver string `json:"database_driver"`
	DBHost         string `json:"db_host"`
	DBPort         int    `json:"db_port"`
	DBUsername     string `json:"db_username"`
	DBPassword     string `json:"db_password"`
	DBName         string `json:"db_name"`
	DBSSLEnabled   bool   `json:"db_ssl_enabled"`

	// Encryption key for securing sensitive data (e.g., connection strings)
	EncryptionKey string `json:"encryption_key"`

	// JWT secret for signing authentication tokens
	JWTSecret string `json:"jwt_secret"`

	// Where the server listens, and the address people reach it at. Both are
	// optional and are written to config.yaml with the database; empty keeps
	// the default (every interface, port 8080) and sends no links.
	HTTPAddress string `json:"http_address"`
	PublicURL   string `json:"public_url"`
}

// TestConnectionRequest contains the database connection parameters for testing connectivity.
type TestConnectionRequest struct {
	SetupToken     string `json:"setup_token"`
	DatabaseDriver string `json:"database_driver"`
	DBHost         string `json:"db_host"`
	DBPort         int    `json:"db_port"`
	DBUsername     string `json:"db_username"`
	DBPassword     string `json:"db_password"`
	DBName         string `json:"db_name"`
	DBSSLEnabled   bool   `json:"db_ssl_enabled"`
}

// TestConnectionResult contains the result of a database connection test.
type TestConnectionResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// SetupService handles the initial configuration and status of the Metis system.
type SetupService interface {
	GetSetupStatus(ctx context.Context) (SetupStatus, error)
	Setup(ctx context.Context, req SetupRequest) error
	TestConnection(ctx context.Context, req TestConnectionRequest) TestConnectionResult

	// AnnounceSetupToken logs how to open the wizard, while it is open.
	AnnounceSetupToken(ctx context.Context)
}
