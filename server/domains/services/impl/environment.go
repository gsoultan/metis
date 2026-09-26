package impl

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/internal/pkg/configsecret"
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/rs/zerolog/log"
)

type environmentService struct {
	repo repositories.Repository
}

// NewEnvironmentService returns the service that manages a project's runtimes.
func NewEnvironmentService(repo repositories.Repository) servicecontracts.EnvironmentService {
	return &environmentService{repo: repo}
}

// Ports below this are reserved by convention and usually need privileges to
// bind. Refusing them here turns a permission error in the server's log, once
// every replica tries to serve the environment, into a message on the form
// somebody is filling in.
const minEnvironmentPort = 1024

// maxEnvironmentNameLength matches the column, so a name that would be silently
// truncated by the database is refused with an explanation instead.
const maxEnvironmentNameLength = 63

// ListEnvironments returns a project's runtimes with their credentials masked.
func (s *environmentService) ListEnvironments(ctx context.Context, projectID uuid.UUID) ([]entities.Environment, error) {
	ms, err := s.repo.Environment().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]entities.Environment, 0, len(ms))
	for _, m := range ms {
		out = append(out, maskedEnvironment(m))
	}
	return out, nil
}

// GetEnvironment returns one runtime with its credentials masked.
func (s *environmentService) GetEnvironment(ctx context.Context, id uuid.UUID) (entities.Environment, error) {
	m, err := s.repo.Environment().Get(ctx, id)
	if err != nil {
		return entities.Environment{}, err
	}
	return maskedEnvironment(m), nil
}

// CreateEnvironment records a new runtime for a project. Every replica starts
// serving an enabled one within seconds (internal/app/environment_watch.go).
func (s *environmentService) CreateEnvironment(ctx context.Context, env entities.Environment) (uuid.UUID, error) {
	if env.Project == nil || env.Project.ID == uuid.Nil {
		return uuid.Nil, apierr.Invalidf("an environment belongs to a project")
	}
	if err := s.validate(ctx, env, uuid.Nil); err != nil {
		return uuid.Nil, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("could not generate an environment id: %w", err)
	}
	m := models.EnvironmentModel{
		Base:       models.Base{ID: models.FromUUID(id)},
		ProjectID:  models.FromUUID(env.Project.ID),
		Name:       strings.TrimSpace(env.Name),
		Port:       env.Port,
		Driver:     env.Driver,
		Connection: models.EncryptedMap(env.Connection),
		Enabled:    env.Enabled,
	}
	if err := s.repo.Environment().Create(ctx, m); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// UpdateEnvironment saves changes to a runtime. Every replica catches up within
// seconds: one disabled stops being served, one enabled again is served, and
// one given another port or database is stopped and served again with it.
//
// The stored connection is merged with what arrived, so a caller who edited the
// host without re-typing the password keeps the password. That is the whole
// reason the sentinel exists — see internal/pkg/configsecret.
func (s *environmentService) UpdateEnvironment(ctx context.Context, env entities.Environment) error {
	existing, err := s.repo.Environment().Get(ctx, env.ID)
	if err != nil {
		return err
	}
	if err := s.validate(ctx, env, env.ID); err != nil {
		return err
	}

	existing.Name = strings.TrimSpace(env.Name)
	existing.Port = env.Port
	existing.Driver = env.Driver
	existing.Enabled = env.Enabled
	existing.Connection = models.EncryptedMap(
		configsecret.Merge(env.Connection, map[string]any(existing.Connection)))

	return s.repo.Environment().Update(ctx, existing)
}

// DeleteEnvironment removes a runtime from the registry.
//
// The database it named is untouched. Removing the row stops the runtime being
// served: every replica notices within a few seconds and stops its port, its
// workers and its connections (internal/app/environment_runtime.go). Deciding to
// destroy what it ran is a separate act, and not one a delete button on a
// settings page should perform.
func (s *environmentService) DeleteEnvironment(ctx context.Context, id uuid.UUID) error {
	return s.repo.Environment().Delete(ctx, id)
}

// validate refuses an environment that could not be served, before it is saved.
//
// Every check here is one whose failure would otherwise surface only when every
// replica tries to serve the environment, seconds after it is saved: as a line
// in each server's log rather than a message on the form.
func (s *environmentService) validate(ctx context.Context, env entities.Environment, excluding uuid.UUID) error {
	name := strings.TrimSpace(env.Name)
	switch {
	case name == "":
		return apierr.Invalidf("an environment needs a name")
	case len(name) > maxEnvironmentNameLength:
		return apierr.Invalidf("the environment name is %d characters; the limit is %d",
			len(name), maxEnvironmentNameLength)
	}

	switch {
	case env.Driver == "":
		return apierr.Invalidf("an environment needs a database driver")
	case !config.SupportedDriver(env.Driver):
		return apierr.Invalidf("driver %q is not a database engine this supports; environments run on PostgreSQL", env.Driver)
	}

	if env.Port < minEnvironmentPort || env.Port > 65535 {
		return apierr.Invalidf("port %d is outside the range an environment can be served on (%d-65535)",
			env.Port, minEnvironmentPort)
	}
	taken, err := s.repo.Environment().PortTaken(ctx, env.Port, excluding)
	if err != nil {
		return err
	}
	if taken {
		// Named without saying whose: a port collision with another
		// organization's environment is still a collision, and telling this
		// caller which organization holds it would answer a question they did
		// not get to ask.
		return apierr.Invalidf("port %d is already served by another environment", env.Port)
	}
	return nil
}

// maskedEnvironment converts a stored row for return over the API, with every
// credential-shaped key replaced by a sentinel.
//
// The masking is here rather than at the endpoint so that no future caller of
// this service can forget it. A database password is worth more than any one
// connector's token: it is every credential in that runtime at once.
func maskedEnvironment(m models.EnvironmentModel) entities.Environment {
	return entities.Environment{
		ID:         uuid.UUID(m.ID),
		Project:    &entities.Project{ID: uuid.UUID(m.ProjectID)},
		Name:       m.Name,
		Port:       m.Port,
		Driver:     m.Driver,
		Connection: configsecret.Mask(map[string]any(m.Connection)),
		Enabled:    m.Enabled,
		CreatedAt:  m.CreatedAt,
	}
}

// TestEnvironmentConnection opens the described database and reports whether it
// answered.
//
// A password sent as the masking sentinel is resolved against what is stored, so
// an administrator can test an existing environment without re-typing a
// credential the browser was never given.
//
// The reply says whether it worked and, when it did not, why — which is a
// network probe, and why this is administrative. The unauthenticated equivalent
// on the setup wizard closes as soon as the installation is configured.
func (s *environmentService) TestEnvironmentConnection(ctx context.Context, env entities.Environment) entities.EnvironmentHealth {
	connection := env.Connection
	if env.ID != uuid.Nil {
		if stored, err := s.repo.Environment().Get(ctx, env.ID); err == nil {
			connection = configsecret.Merge(connection, map[string]any(stored.Connection))
		}
	}

	dsn := config.BuildConnectionString(env.Driver, config.DatabaseFields{
		Host:       stringOf(connection, "host"),
		Port:       intOf(connection, "port"),
		Username:   stringOf(connection, "username"),
		Password:   stringOf(connection, "password"),
		DBName:     stringOf(connection, "db_name"),
		SSLEnabled: boolOf(connection, "ssl_enabled"),
	})

	db, err := gorms.Open(env.Driver, dsn)
	if err != nil {
		return entities.EnvironmentHealth{EnvironmentID: env.ID, Detail: redaction.RedactError(err)}
	}
	defer func() {
		// A connection test that leaks its own pool is a connection test that
		// exhausts the server it was checking.
		if sqlDB, handleErr := db.DB(); handleErr == nil {
			if closeErr := sqlDB.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Msg("Could not close the connection opened to test an environment")
			}
		}
	}()

	if err := gorms.Ping(db); err != nil {
		return entities.EnvironmentHealth{EnvironmentID: env.ID, Detail: redaction.RedactError(err)}
	}
	return entities.EnvironmentHealth{EnvironmentID: env.ID, Reachable: true}
}

// The three readers below take the comma-ok form because the connection map is
// stored as JSON: a port written as an int comes back as a float64, and a bare
// assertion would be a panic on a value that made a legitimate round trip.
func stringOf(m map[string]any, key string) string {
	if value, ok := m[key].(string); ok {
		return value
	}
	return ""
}

func intOf(m map[string]any, key string) int {
	switch value := m[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	}
	return 0
}

func boolOf(m map[string]any, key string) bool {
	value, ok := m[key].(bool)
	return ok && value
}
