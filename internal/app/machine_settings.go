package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/internal/pkg/envvar"
)

// machineSettings is this installation's config.yaml for a machine added
// from the Servers page that runs role: the same database, the same
// ENCRYPTION_KEY and JWT_SECRET, and what this server is called, is for and
// is reached at. Read when an administrator accepts a machine, and sealed to
// that machine's key before it leaves this process.
//
// What config.yaml does not hold is not handed over: METIS_PEOPLE_TOKEN_KEY,
// a payload directory, an archive store, RabbitMQ. A machine that needs those
// is given them the way this one was.
func (a *App) machineSettings(role string) ([]byte, error) {
	dsn, err := a.resolveDSN()
	if err != nil {
		return nil, err
	}
	key, err := encryptionKeyInUse()
	if err != nil {
		return nil, err
	}
	jwtSecret, err := a.resolveJWTSecret()
	if err != nil {
		return nil, err
	}
	cfg, err := config.NewConfig(config.DriverPostgres, dsn, key, jwtSecret)
	if err != nil {
		return nil, err
	}
	cfg.Server = config.ServerConfig{
		Role:      role,
		PublicURL: strings.TrimSpace(envvar.Get(config.EnvPublicURL)),
		Purpose:   strings.TrimSpace(envvar.Get(config.EnvServerPurpose)),
		Name:      strings.TrimSpace(envvar.Get(config.EnvServerName)),
	}
	return cfg.Marshal()
}

// encryptionKeyInUse is the key this installation's data is sealed with, by
// the precedence setupEncryption reads it: config.yaml's first, because it
// is the one the data was sealed with, then ENCRYPTION_KEY.
func encryptionKeyInUse() (string, error) {
	if config.Exists(config.DefaultConfigPath) {
		cfg, err := config.Load(config.DefaultConfigPath)
		if err != nil {
			return "", fmt.Errorf("could not read %s: %w", config.DefaultConfigPath, err)
		}
		if cfg.EncryptionKey != "" {
			return cfg.EncryptionKey, nil
		}
	}
	if key := envvar.Get("ENCRYPTION_KEY"); key != "" {
		return key, nil
	}
	return "", errors.New("this server has no ENCRYPTION_KEY to hand to a new machine")
}
