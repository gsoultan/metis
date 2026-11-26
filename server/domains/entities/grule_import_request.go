package entities

import "github.com/google/uuid"

// GruleImportRequest asks for a set of Grule rules to be turned into metis
// decisions and a process.
type GruleImportRequest struct {
	ProjectID uuid.UUID

	// Key names what is made: the process is Key, each table Key-<fields>.
	// Name is the process's name, Key when blank.
	Key  string
	Name string

	Files []RuleFile

	// Preview translates and reports, and stores nothing.
	Preview bool

	// Deploy deploys the process as well as storing the decisions. Without it
	// the process is only returned, to be opened in the designer.
	Deploy bool
}
