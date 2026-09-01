package config

import (
	"fmt"
	"os"

	"github.com/gsoultan/metis/internal/pkg/crypto"
	"gopkg.in/yaml.v3"

	"github.com/rs/zerolog/log"
)

const (
	// DefaultConfigPath is the default location for the configuration file.
	DefaultConfigPath = "config.yaml"

	// DriverSQLite represents the SQLite database driver.
	DriverSQLite = "sqlite"
	// DriverPostgres represents the PostgreSQL database driver.
	DriverPostgres = "postgres"
	// DriverMySQL represents the MySQL database driver.
	DriverMySQL = "mysql"
	// DriverSQLServer represents the SQL Server database driver.
	DriverSQLServer = "sqlserver"
)

// DatabaseConfig holds the database connection settings.
type DatabaseConfig struct {
	Driver              string `yaml:"driver" json:"driver"`
	EncryptedConnection string `yaml:"encrypted_connection" json:"encrypted_connection"`
}

// Config represents the top-level application configuration stored in config.yaml.
//
// Security note: While it is recommended to supply the encryption key via the
// ENCRYPTION_KEY environment variable, it can also be provided in config.yaml
// for development convenience. Storing the key alongside the encrypted
// connection string in production is NOT recommended.
type Config struct {
	Database      DatabaseConfig `yaml:"database" json:"database"`
	EncryptionKey string         `yaml:"encryption_key" json:"encryption_key,omitzero"`
	JWTSecret     string         `yaml:"jwt_secret" json:"jwt_secret,omitzero"`
}

// Save writes the configuration to the specified file path as YAML.
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// DecryptConnectionString decrypts the stored connection string using the
// provided passphrase (typically the value of the ENCRYPTION_KEY env var)
// or the key stored in the configuration itself.
func (c *Config) DecryptConnectionString(passphrase string) (string, error) {
	if c.Database.EncryptedConnection == "" {
		return "", nil
	}

	keyToUse := passphrase
	if keyToUse == "" {
		keyToUse = c.EncryptionKey
	}

	if keyToUse == "" {
		return "", fmt.Errorf("ENCRYPTION_KEY environment variable or config encryption_key is required to decrypt the database connection string")
	}

	key := crypto.DeriveKey(keyToUse)
	plaintext, err := crypto.DecryptWithKey(c.Database.EncryptedConnection, key)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt connection string: %w", err)
	}

	return plaintext, nil
}

// Load reads and parses a config.yaml file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}

// Exists checks whether a config file exists at the given path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DatabaseFields holds the individual database connection parameters.
type DatabaseFields struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	DBName     string `json:"db_name"`
	SSLEnabled bool   `json:"ssl_enabled"`
}

// DefaultPort returns the default port for the given database driver.
func DefaultPort(driver string) int {
	switch driver {
	case DriverPostgres:
		return 5432
	case DriverMySQL:
		return 3306
	case DriverSQLServer:
		return 1433
	default:
		return 0
	}
}

// BuildConnectionString constructs a driver-specific connection string from individual fields.
func BuildConnectionString(driver string, fields DatabaseFields) string {
	switch driver {
	case DriverPostgres:
		sslMode := "disable"
		if fields.SSLEnabled {
			sslMode = "require"
		}
		return fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			fields.Host, fields.Port, fields.Username, fields.Password, fields.DBName, sslMode,
		)
	case DriverMySQL:
		tls := "false"
		if fields.SSLEnabled {
			tls = "true"
		}
		return fmt.Sprintf(
			"%s:%s@tcp(%s:%d)/%s?parseTime=true&tls=%s",
			fields.Username, fields.Password, fields.Host, fields.Port, fields.DBName, tls,
		)
	case DriverSQLServer:
		encrypt := "disable"
		if fields.SSLEnabled {
			encrypt = "true"
		}
		return fmt.Sprintf(
			"sqlserver://%s:%s@%s:%d?database=%s&encrypt=%s",
			fields.Username, fields.Password, fields.Host, fields.Port, fields.DBName, encrypt,
		)
	default:
		// SQLite: fields.DBName is the file path
		if fields.DBName == "" {
			return DefaultSQLitePath()
		}
		return fields.DBName
	}
}

// NewConfig creates a new Config by encrypting the provided connection string
// with the supplied passphrase.
func NewConfig(driver, connectionString, encryptionKey, jwtSecret string) (*Config, error) {
	if encryptionKey == "" {
		return nil, fmt.Errorf("encryption key must not be empty")
	}
	key := crypto.DeriveKey(encryptionKey)

	encrypted, err := crypto.EncryptWithKey(connectionString, key)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt connection string: %w", err)
	}

	return &Config{
		Database: DatabaseConfig{
			Driver:              driver,
			EncryptedConnection: encrypted,
		},
		EncryptionKey: encryptionKey,
		JWTSecret:     jwtSecret,
	}, nil
}

// DefaultSQLitePath is the SQLite file used when nothing names one.
//
// The product's file is metis.db. An installation that predates the rename has
// a gobpm.db instead, and creating a fresh empty database beside it would look
// exactly like total data loss to whoever restarted the service — every
// process, task and definition simply gone, with no error to explain it. So an
// existing gobpm.db wins, and says so.
//
// The check is for an existing file rather than a configured name because this
// path is only reached when nothing was configured at all.
func DefaultSQLitePath() string {
	// The new name wins when it exists: an operator who has already renamed
	// their file and left a stale copy behind should get the one they moved to.
	if isRegularFile(defaultSQLiteFile) {
		return defaultSQLiteFile
	}
	if isRegularFile(legacySQLiteFile) {
		log.Info().
			Str("file", legacySQLiteFile).
			Str("new_installs_use", defaultSQLiteFile).
			Msg("Opening the pre-rename database file. Rename it to metis.db when convenient, or set DATABASE_URL to name it explicitly.")
		return legacySQLiteFile
	}
	return defaultSQLiteFile
}

// isRegularFile reports whether path is a file this driver could open.
//
// Stat alone is not enough: it succeeds on a directory, so a stray `gobpm.db/`
// would be handed to the driver as though it were a database.
func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

const (
	defaultSQLiteFile = "metis.db"
	legacySQLiteFile  = "gobpm.db"
)
