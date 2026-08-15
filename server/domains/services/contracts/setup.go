package contracts

import (
	"context"
)

// SetupStatus represents the current initialization state of the system.
type SetupStatus struct {
	IsInitialized bool `json:"is_initialized"`
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
}

// TestConnectionRequest contains the database connection parameters for testing connectivity.
type TestConnectionRequest struct {
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
}
