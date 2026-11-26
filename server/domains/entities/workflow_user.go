package entities

import (
	"time"

	"github.com/google/uuid"
)

// WorkflowUser is somebody a process can assign work to.
//
// Scoped to a project and unique within it: two projects naming an approver
// "ada" mean two different people. It carries no roles — what a participant may
// do is decided by the process that names them, and access control lives on the
// platform account, which is a different population.
type WorkflowUser struct {
	ID      uuid.UUID `json:"id"`
	Project *Project  `json:"project,omitzero"`

	Username string `json:"username"`
	// ExternalID is this person's id in the customer's own application. A
	// directory import recognises them by it, so a rename there is a rename
	// here rather than a second person. Unique within the project when set.
	ExternalID  string `json:"external_id,omitzero"`
	DisplayName string `json:"display_name,omitzero"`
	Email       string `json:"email,omitzero"`

	// Active is how somebody stops receiving work without deleting the record
	// of what they did.
	Active bool `json:"active"`

	// HasCredentials reports whether this participant can sign in yet. A
	// directory imported from a CSV is five hundred names, not five hundred
	// passwords, so a participant routinely exists before they can log in.
	//
	// The hash itself never leaves the repository.
	HasCredentials bool `json:"has_credentials"`

	CreatedAt time.Time `json:"created_at,omitzero"`
}

// WorkflowGroup is a named set of participants a task can be offered to.
//
// Not a role: belonging to one grants nothing except the chance to claim work
// addressed to it.
type WorkflowGroup struct {
	ID          uuid.UUID `json:"id"`
	Project     *Project  `json:"project,omitzero"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitzero"`
}

// ImportSummary is what an import of a participant directory did.
//
// Created and Updated are counted separately because they answer different
// questions: one says how many people are new, the other says how many rows
// overwrote somebody who was already there — which is the number worth checking
// when a file was meant to be additive.
type ImportSummary struct {
	Created int `json:"created"`
	// Updated counts the people whose name, address or active flag changed,
	// and the people given an external ID for the first time.
	Updated int `json:"updated"`
	// Unchanged counts the people the source named exactly as they were.
	Unchanged int `json:"unchanged,omitzero"`
	// Renamed counts the people found by external ID under another username.
	Renamed int `json:"renamed,omitzero"`
	// TasksMoved counts the open tasks readdressed to a renamed person's new
	// username.
	TasksMoved int `json:"tasks_moved,omitzero"`
	// Groups is how many groups had to be created to satisfy the file.
	Groups int `json:"groups"`
	// Deactivated is how many people the source stopped naming and was told to
	// stand down. Zero unless the source asks for it — an import leaves people
	// alone, because a partial file is not a statement about everybody it
	// omits.
	Deactivated int `json:"deactivated,omitzero"`
	// Problems are the rows that were not imported, each with its line.
	Problems []ImportProblem `json:"problems,omitzero"`
}

// ImportProblem is one row that could not be imported, and why.
type ImportProblem struct {
	Line     int    `json:"line"`
	Username string `json:"username,omitzero"`
	Reason   string `json:"reason"`
}

// DirectoryImport says where a directory is read from and how.
//
// One shape for the three sources, because the caller is answering one
// question — where does this list come from — and an import and its check
// take the same answer.
type DirectoryImport struct {
	// Kind is csv, http or postgres.
	Kind string

	// CSV is an uploaded file's contents.
	CSV []byte

	// An endpoint, and the headers that authenticate the call. Headers are
	// never returned.
	URL     string
	Method  string
	Headers map[string]string

	// A query against another database. The DSN carries a password and is
	// never returned.
	DSN   string
	Query string

	// Mapping names the source's fields the way Metis reads them, keyed by
	// Metis's name.
	Mapping map[string]string

	// MoveOpenTasks lets a rename carry the open tasks naming the old username
	// over to the new one. Without it, a rename that would leave open tasks
	// behind is not applied, and is reported.
	MoveOpenTasks bool
}

// ImportCheck is what importing a directory would do, worked out without
// writing anything.
type ImportCheck struct {
	// Read is how many rows the source returned, usable or not.
	Read int `json:"read"`
	// Counts are the rows by outcome: new, unchanged, update, linked,
	// renamed, conflict and invalid.
	Counts map[string]int `json:"counts"`
	// Fields are the names the source used, for matching columns.
	Fields []string `json:"fields"`
	// Sample is the first record as the source sent it.
	Sample map[string]string `json:"sample,omitzero"`
	// Rows are every row that is not unchanged, in source order, up to a
	// limit.
	Rows []CheckedRow `json:"rows"`
	// Truncated says Rows stopped at the limit.
	Truncated bool `json:"truncated,omitzero"`
}

// CheckedRow is one row of a check, and what importing it would do.
type CheckedRow struct {
	Line        int    `json:"line"`
	Outcome     string `json:"outcome"`
	Username    string `json:"username,omitzero"`
	ExternalID  string `json:"external_id,omitzero"`
	DisplayName string `json:"display_name,omitzero"`
	Email       string `json:"email,omitzero"`
	// Detail says what would change, or why the row cannot be imported.
	Detail string `json:"detail,omitzero"`
	// OpenTasks is, for a rename, how many open tasks name the old username.
	OpenTasks int `json:"open_tasks,omitzero"`
}
