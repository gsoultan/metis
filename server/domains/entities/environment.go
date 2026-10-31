package entities

import (
	"time"

	"github.com/google/uuid"
)

// Environment is one runtime a project deploys into, and the database that
// runtime owns.
//
// A project's environments share nothing. Each names its own database, so a
// process instance started in staging exists only in staging's — not scoped
// away by a column, but absent, with no query that could reach it.
type Environment struct {
	ID      uuid.UUID `json:"id"`
	Project *Project  `json:"project,omitzero"`
	// Name is what a person calls it: "staging", "production".
	Name string `json:"name"`
	// Port is where this environment is served.
	Port int `json:"port"`
	// Driver is the database engine. PostgreSQL is the only one; the field
	// remains because a stored environment names its driver and an installation
	// upgrading into this needs its rows to still read.
	Driver string `json:"driver"`
	// Connection holds host, port, username, password, db_name, ssl_enabled.
	//
	// Leaving the service it is masked: every credential-shaped key is replaced
	// by a sentinel the caller sends back to mean "unchanged". The password
	// itself never reaches a browser. See internal/pkg/configsecret.
	Connection map[string]any `json:"connection,omitzero"`
	Enabled    bool           `json:"enabled"`
	// Protected marks a runtime people rely on, such as production: there only
	// a releaser or an administrator may deploy, promote or schedule a model.
	Protected bool      `json:"protected"`
	CreatedAt time.Time `json:"created_at,omitzero"`
}

// EnvironmentHealth is what a reachability probe found.
//
// Separate from Environment because it is a fact about right now, not about the
// configuration: a correctly configured environment whose database is down is
// still correctly configured, and the page has to say which of the two is
// wrong.
type EnvironmentHealth struct {
	EnvironmentID uuid.UUID `json:"environment_id"`
	Reachable     bool      `json:"reachable"`
	// Detail names what failed, for the case a person has to fix it. Empty when
	// reachable.
	Detail string `json:"detail,omitzero"`
}
