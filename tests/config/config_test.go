package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/config"
)

func TestNewConfig_EncryptsConnectionString(t *testing.T) {
	cfg, err := config.NewConfig(config.DriverPostgres, "host=localhost dbname=test", "my-secret-key-1234", "test-jwt-secret")
	if err != nil {
		t.Fatalf("NewConfig failed: %v", err)
	}

	if cfg.Database.Driver != config.DriverPostgres {
		t.Fatalf("expected driver %q, got %q", config.DriverPostgres, cfg.Database.Driver)
	}

	if cfg.Database.EncryptedConnection == "" {
		t.Fatal("encrypted connection should not be empty")
	}

	if cfg.Database.EncryptedConnection == "host=localhost dbname=test" {
		t.Fatal("connection string should be encrypted, not plaintext")
	}
}

func TestNewConfig_DecryptConnectionString(t *testing.T) {
	original := "host=localhost port=5432 user=gobpm password=secret dbname=gobpm"
	encKey := "my-encryption-key-16ch"

	cfg, err := config.NewConfig(config.DriverPostgres, original, encKey, "test-jwt-secret")
	if err != nil {
		t.Fatalf("NewConfig failed: %v", err)
	}

	decrypted, err := cfg.DecryptConnectionString(encKey)
	if err != nil {
		t.Fatalf("DecryptConnectionString failed: %v", err)
	}

	if decrypted != original {
		t.Fatalf("expected %q, got %q", original, decrypted)
	}
}

func TestConfig_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	original := "host=db.example.com dbname=production"
	encKey := "save-load-test-key-16"

	cfg, err := config.NewConfig(config.DriverPostgres, original, encKey, "test-jwt-secret")
	if err != nil {
		t.Fatalf("NewConfig failed: %v", err)
	}

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	if !config.Exists(configPath) {
		t.Fatal("config file should exist after save")
	}

	// Verify file permissions (owner read/write only)
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("config file should not be empty")
	}

	// Load and verify
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Database.Driver != config.DriverPostgres {
		t.Fatalf("expected driver %q, got %q", config.DriverPostgres, loaded.Database.Driver)
	}

	decrypted, err := loaded.DecryptConnectionString(encKey)
	if err != nil {
		t.Fatalf("DecryptConnectionString failed: %v", err)
	}

	if decrypted != original {
		t.Fatalf("expected %q, got %q", original, decrypted)
	}
}

func TestConfig_DecryptConnectionString_Empty(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Driver:              config.DriverSQLite,
			EncryptedConnection: "",
		},
		EncryptionKey: "some-key-1234567",
	}

	decrypted, err := cfg.DecryptConnectionString(cfg.EncryptionKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decrypted != "" {
		t.Fatalf("expected empty string, got %q", decrypted)
	}
}

func TestExists_NonExistentFile(t *testing.T) {
	if config.Exists(filepath.Join(t.TempDir(), "nonexistent.yaml")) {
		t.Fatal("Exists should return false for non-existent file")
	}
}

func TestLoad_NonExistentFile(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Fatal("Load should fail for non-existent file")
	}
}

func TestNewConfig_AllDrivers(t *testing.T) {
	drivers := []string{config.DriverSQLite, config.DriverPostgres, config.DriverMySQL, config.DriverSQLServer}

	for _, driver := range drivers {
		t.Run(driver, func(t *testing.T) {
			cfg, err := config.NewConfig(driver, "test-connection", "encryption-key-16ch", "test-jwt-secret")
			if err != nil {
				t.Fatalf("NewConfig failed for driver %s: %v", driver, err)
			}
			if cfg.Database.Driver != driver {
				t.Fatalf("expected driver %q, got %q", driver, cfg.Database.Driver)
			}
		})
	}
}

func TestDefaultPort(t *testing.T) {
	tests := []struct {
		driver string
		want   int
	}{
		{config.DriverPostgres, 5432},
		{config.DriverMySQL, 3306},
		{config.DriverSQLServer, 1433},
		{config.DriverSQLite, 0},
		{"unknown", 0},
	}
	for _, tt := range tests {
		t.Run(tt.driver, func(t *testing.T) {
			got := config.DefaultPort(tt.driver)
			if got != tt.want {
				t.Fatalf("DefaultPort(%q) = %d, want %d", tt.driver, got, tt.want)
			}
		})
	}
}

func TestBuildConnectionString(t *testing.T) {
	tests := []struct {
		name   string
		driver string
		fields config.DatabaseFields
		want   string
	}{
		{
			name:   "postgres without ssl",
			driver: config.DriverPostgres,
			fields: config.DatabaseFields{Host: "localhost", Port: 5432, Username: "user", Password: "pass", DBName: "mydb"},
			want:   "host=localhost port=5432 user=user password=pass dbname=mydb sslmode=disable",
		},
		{
			name:   "postgres with ssl",
			driver: config.DriverPostgres,
			fields: config.DatabaseFields{Host: "db.example.com", Port: 5432, Username: "user", Password: "pass", DBName: "mydb", SSLEnabled: true},
			want:   "host=db.example.com port=5432 user=user password=pass dbname=mydb sslmode=require",
		},
		{
			name:   "mysql without ssl",
			driver: config.DriverMySQL,
			fields: config.DatabaseFields{Host: "localhost", Port: 3306, Username: "root", Password: "secret", DBName: "gobpm"},
			want:   "root:secret@tcp(localhost:3306)/gobpm?parseTime=true&tls=false",
		},
		{
			name:   "mysql with ssl",
			driver: config.DriverMySQL,
			fields: config.DatabaseFields{Host: "db.example.com", Port: 3306, Username: "root", Password: "secret", DBName: "gobpm", SSLEnabled: true},
			want:   "root:secret@tcp(db.example.com:3306)/gobpm?parseTime=true&tls=true",
		},
		{
			name:   "sqlserver without ssl",
			driver: config.DriverSQLServer,
			fields: config.DatabaseFields{Host: "localhost", Port: 1433, Username: "sa", Password: "pass", DBName: "gobpm"},
			want:   "sqlserver://sa:pass@localhost:1433?database=gobpm&encrypt=disable",
		},
		{
			name:   "sqlserver with ssl",
			driver: config.DriverSQLServer,
			fields: config.DatabaseFields{Host: "localhost", Port: 1433, Username: "sa", Password: "pass", DBName: "gobpm", SSLEnabled: true},
			want:   "sqlserver://sa:pass@localhost:1433?database=gobpm&encrypt=true",
		},
		{
			name:   "sqlite with db name",
			driver: config.DriverSQLite,
			fields: config.DatabaseFields{DBName: "custom.db"},
			want:   "custom.db",
		},
		{
			// A fresh install. An existing gobpm.db from before the rename is
			// still opened instead — see DefaultSQLitePath and its own tests,
			// which run in a temp directory so this table stays about the
			// connection string rather than about what is on disk.
			name:   "sqlite default",
			driver: config.DriverSQLite,
			fields: config.DatabaseFields{},
			want:   "metis.db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.BuildConnectionString(tt.driver, tt.fields)
			if got != tt.want {
				t.Fatalf("BuildConnectionString(%q) = %q, want %q", tt.driver, got, tt.want)
			}
		})
	}
}
