package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	neturl "net/url"
	"os"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/crypto"
	"gopkg.in/yaml.v3"
)

const (
	// DefaultConfigPath is the default location for the configuration file.
	DefaultConfigPath = "config.yaml"

	// DriverPostgres is the database this runs on. It is the only one.
	//
	// SQLite, MySQL and SQL Server were supported and are not any more. The
	// engine's storage layer is compiled rather than assembled at run time,
	// which is what lets a query's shape be checked before it runs and a
	// soft-delete predicate be a property of the schema rather than a rule every
	// call site remembers — and that compiler emits PostgreSQL. Four dialects
	// also meant four spellings of every constraint, three of which were
	// exercised by a test suite that skipped unless somebody had a server
	// running, so "the tests pass" routinely meant "SQLite passes".
	//
	// The constant remains rather than being inlined because a stored config
	// names its driver, and an installation upgrading into this needs its file
	// to still parse so it can be told what changed.
	DriverPostgres = "postgres"
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
//
// It writes the encryption key and the JWT secret, which is the point: this is
// the file the setup wizard produces and the server reads them back from.
// 0600 is what makes that acceptable, so the mode below is a security control
// rather than a default — the same secrets in a world-readable file would be
// an administrator's token and a decryption key for any stolen backup.
//
// #nosec G117 -- marshalling the secrets is what this function is for; the
// file mode is the control, and docs/recovery.md covers keeping ENCRYPTION_KEY
// backed up separately from the database.
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

	// Or with a previous key: after ENCRYPTION_KEY is rotated, and before
	// --reseal has sealed this again, the connection string is still under the
	// old one, and without it the server cannot reach its database at all.
	key := crypto.DeriveKey(keyToUse)
	plaintext, err := crypto.DecryptWithKeyOrPrevious(c.Database.EncryptedConnection, key)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt connection string: %w", err)
	}

	return plaintext, nil
}

// Load reads and parses a config.yaml file from the given path.
func Load(path string) (*Config, error) {
	// #nosec G304 -- path comes from a flag or the installation default, not
	// from a request. Reading an operator-named file is what this function is.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	// KnownFields: an unrecognised key is a typo, and a typo in a database
	// setting is a server quietly pointed at something nobody intended.
	// yaml.Unmarshal ignores them by default, so `databse:` read as no database
	// at all and the caller fell through to a fresh local one.
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
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

// DefaultPort returns the port PostgreSQL listens on unless told otherwise.
//
// It still takes a driver so that a stored config naming one this no longer
// supports gets zero rather than 5432 — a wrong port is a connection error that
// names the host, which is a better thing to read than a refusal to start.
func DefaultPort(driver string) int {
	if driver == DriverPostgres {
		return 5432
	}
	return 0
}

// BuildConnectionString assembles a PostgreSQL connection string from fields.
//
// A driver this does not recognise returns the empty string rather than a
// best-effort guess. Opening on an empty DSN fails immediately and says so,
// where a guess would connect to something — the local socket, a default
// database — and the first sign of trouble would be data in the wrong place.
//
// Values are quoted when they need it. Interpolating them raw is what this used
// to do, and libpq's keyword/value grammar skips whitespace between `=` and the
// value — so an empty password emitted `password= dbname=metis` and parsed as
// the password *being* `dbname=metis`, leaving no database at all. libpq then
// falls back to a database named after the user. The wizard does not require a
// database password, so that was reachable by leaving a field blank: setup
// migrated and seeded a database nobody named, wrote it into config.yaml, and
// every boot afterwards went there. A space in the password made the whole DSN
// unparseable, and a backslash was silently dropped from it.
func BuildConnectionString(driver string, fields DatabaseFields) string {
	if driver != DriverPostgres {
		return ""
	}
	sslMode := "disable"
	if fields.SSLEnabled {
		sslMode = "require"
	}
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		quoteDSNValue(fields.Host), fields.Port, quoteDSNValue(fields.Username),
		quoteDSNValue(fields.Password), quoteDSNValue(fields.DBName), sslMode,
	)
}

// dsnValueNeedsQuoting reports whether a value would not survive being written
// bare into a keyword/value connection string.
//
// Empty is included: a bare `password=` does not mean "no password", it means
// the parser keeps reading and takes the next keyword as the value.
func dsnValueNeedsQuoting(value string) bool {
	if value == "" {
		return true
	}
	return strings.ContainsAny(value, " \t\r\n'\\")
}

// quoteDSNValue renders a value for a keyword/value connection string.
//
// The common case is returned unchanged, so an ordinary DSN reads exactly as it
// did before and an operator comparing config.yaml against their notes sees no
// difference.
func quoteDSNValue(value string) string {
	if !dsnValueNeedsQuoting(value) {
		return value
	}
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('\'')
	for i := range len(value) {
		if c := value[i]; c == '\'' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(value[i])
	}
	b.WriteByte('\'')
	return b.String()
}

// parseKeywordValueDSN reads libpq's keyword/value grammar.
//
// strings.Fields was here, and it is wrong in both directions: it splits a
// quoted value that contains a space into two fields and drops the second for
// having no `=`, and it cannot see that `password=` swallows the keyword after
// it. Both produce a map missing `dbname`, which is how one layer reached the
// configured database and the other reached whatever libpq defaulted to.
//
// The second return reports whether the string parsed. A malformed DSN is
// handed back to the driver untouched rather than guessed at, so the error
// names the connection string instead of a database nobody asked for.
func parseKeywordValueDSN(dsn string) (map[string]string, bool) {
	fields := map[string]string{}
	i, n := 0, len(dsn)
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

	for {
		for i < n && isSpace(dsn[i]) {
			i++
		}
		if i >= n {
			return fields, true
		}

		start := i
		for i < n && dsn[i] != '=' && !isSpace(dsn[i]) {
			i++
		}
		keyword := dsn[start:i]
		if keyword == "" {
			return nil, false
		}

		for i < n && isSpace(dsn[i]) {
			i++
		}
		if i >= n || dsn[i] != '=' {
			return nil, false
		}
		i++
		for i < n && isSpace(dsn[i]) {
			i++
		}

		var value strings.Builder
		if i < n && dsn[i] == '\'' {
			i++
			for {
				if i >= n {
					return nil, false
				}
				if dsn[i] == '\'' {
					i++
					break
				}
				if dsn[i] == '\\' {
					i++
					if i >= n {
						return nil, false
					}
				}
				value.WriteByte(dsn[i])
				i++
			}
		} else {
			for i < n && !isSpace(dsn[i]) {
				if dsn[i] == '\\' {
					i++
					if i >= n {
						return nil, false
					}
				}
				value.WriteByte(dsn[i])
				i++
			}
		}
		fields[keyword] = value.String()
	}
}

// PostgresURL converts a key/value connection string into the URL form pgx
// takes.
//
// Two formats for one database is not a choice anybody made; it is what the two
// drivers accept. Converting in one place means an environment is configured
// once and both layers reach the same database — resolving it twice is how one
// ends up on the configured database and the other somewhere else.
//
// A DSN that does not parse, or that names no database, is returned unchanged.
// Building a URL out of it would produce `postgres://user@host:5432/` — a
// request to connect to the default database, which is the silent wrong answer
// this function exists to prevent.
func PostgresURL(dsn string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return dsn
	}
	fields, ok := parseKeywordValueDSN(dsn)
	if !ok || fields["dbname"] == "" {
		return dsn
	}
	sslMode := fields["sslmode"]
	if sslMode == "" {
		sslMode = "disable"
	}

	query := neturl.Values{}
	query.Set("sslmode", sslMode)
	if searchPath := fields["search_path"]; searchPath != "" {
		query.Set("search_path", searchPath)
	}

	// net/url rather than Sprintf: a password is arbitrary bytes, and an `@`
	// or a `/` in one silently re-points the host or the database when the URL
	// is assembled by hand.
	u := neturl.URL{
		Scheme:   "postgres",
		User:     neturl.UserPassword(fields["user"], fields["password"]),
		Host:     net.JoinHostPort(fields["host"], fields["port"]),
		Path:     "/" + fields["dbname"],
		RawQuery: query.Encode(),
	}
	return u.String()
}

// SupportedDriver reports whether a stored or submitted driver is one this can
// open, so the refusal happens where somebody can read it rather than at the
// first query.
func SupportedDriver(driver string) bool { return driver == DriverPostgres }

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
