package models

// EnvironmentModel is one runtime a project can deploy into — dev, staging,
// production — and the database that runtime owns.
//
// The separation is the point. A project's environments hold no data in common:
// each names its own database, and a process instance started in staging exists
// only in staging's. That is stronger than scoping rows by a column, because
// there is no query that could reach across even if one were written wrong.
//
// This row lives in the main database, alongside the organizations, projects
// and accounts it belongs to. What the environment's own database holds is the
// runtime — deployed definitions, instances, tasks, jobs — and nothing about
// who may sign in.
type EnvironmentModel struct {
	Base
	ProjectID UUID `gorm:"index:idx_environment_project_name,unique,priority:1" json:"project_id,omitzero"`
	// Name is what a person calls it: "staging", "production". Unique within a
	// project, because "deploy to staging" has to name one thing.
	//
	// 63 characters, matching the process key limit, so the composite unique
	// index fits MySQL's key length under utf8mb4.
	Name string `gorm:"size:63;index:idx_environment_project_name,unique,priority:2" json:"name"`

	// Port is where this environment is served. Unique across the whole
	// installation rather than per project: two listeners cannot share a port,
	// and finding that out at bind time — after the row is saved and the
	// server is restarting — is worse than being refused on save.
	Port int `gorm:"uniqueIndex;not null;default:0" json:"port"`

	// Driver is the database engine backing this environment. PostgreSQL is
	// the only one this supports; the column remains because a stored row names
	// its driver, and an installation upgrading into this needs its environments
	// to still read so it can be told which of them will no longer open.
	Driver string `gorm:"size:32" json:"driver"`

	// Connection holds host, port, username, password, db_name and ssl_enabled.
	//
	// A map rather than columns because it is exactly the shape the setup wizard
	// already collects and tests, and because EncryptedMap gives it the same
	// at-rest protection connector credentials get. A database password in
	// cleartext is worth more to an attacker than any single connector's token:
	// it is every credential in that environment at once.
	Connection EncryptedMap `gorm:"type:text" json:"connection,omitzero"`

	// Enabled is whether this environment is served at all. Disabling one keeps
	// the row and its database while taking the listener down — the way to
	// retire an environment without deciding, in the same moment, to destroy
	// everything it ran.
	Enabled bool `json:"enabled"`
}

// TableName overrides the table name for EnvironmentModel.
func (EnvironmentModel) TableName() string {
	return "environments"
}
