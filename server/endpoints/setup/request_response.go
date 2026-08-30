package setup

import (
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

type GetSetupStatusRequest struct{}

type GetSetupStatusResponse struct {
	Status contracts.SetupStatus `json:"status"`
	Err    error                 `json:"err,omitempty"`
}

func (r GetSetupStatusResponse) Failed() error { return r.Err }

type SetupRequest struct {
	AdminUsername    string `json:"admin_username"`
	AdminPassword    string `json:"admin_password"`
	AdminFullName    string `json:"admin_full_name"`
	AdminPublicName  string `json:"admin_public_name"`
	AdminEmail       string `json:"admin_email"`
	OrganizationName string `json:"organization_name"`
	ProjectName      string `json:"project_name"`
	DatabaseDriver   string `json:"database_driver"`
	DBHost           string `json:"db_host"`
	DBPort           int    `json:"db_port"`
	DBUsername       string `json:"db_username"`
	DBPassword       string `json:"db_password"`
	DBName           string `json:"db_name"`
	DBSSLEnabled     bool   `json:"db_ssl_enabled"`
	EncryptionKey    string `json:"encryption_key"`
	JWTSecret        string `json:"jwt_secret"`
}

type SetupResponse struct {
	Err error `json:"err,omitempty"`
}

func (r SetupResponse) Failed() error { return r.Err }

type TestConnectionRequest struct {
	DatabaseDriver string `json:"database_driver"`
	DBHost         string `json:"db_host"`
	DBPort         int    `json:"db_port"`
	DBUsername     string `json:"db_username"`
	DBPassword     string `json:"db_password"`
	DBName         string `json:"db_name"`
	DBSSLEnabled   bool   `json:"db_ssl_enabled"`
}

type TestConnectionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
