package entities

import "github.com/google/uuid"

// ScriptTaskUsage locates one stored script task — one line of the inventory
// behind the script sandbox's remaining gap.
//
// The sandbox bounds wall-clock time, recursion depth and host capability, and
// it cannot bound memory: goja exposes no heap limit, so an abandoned script
// goes on allocating until it finishes. That is the one veto in AGENTS.md §2
// `sec` the codebase does not meet, and the fix is either a FEEL replacement
// for what script tasks are actually used for, or running them out of process
// under an rlimit.
//
// Which of those is right is a question about the scripts that exist, not a
// question of principle — so this reports them. A risk whose blast radius
// cannot be enumerated cannot be managed.
//
// Unlike JavaScriptConditionUsage this is not a migration worklist: script
// tasks are not gated by a flag and nothing here refuses to run. It is an
// inventory, and the distinction matters — a reader must not come away
// thinking these are broken.
type ScriptTaskUsage struct {
	DefinitionID   uuid.UUID `json:"definition_id"`
	DefinitionKey  string    `json:"definition_key"`
	DefinitionName string    `json:"definition_name"`
	Version        int       `json:"version"`

	// NodeID names the script task; NodeName is its display name, which is
	// what an operator will recognise and is often empty in imported models.
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name,omitzero"`

	// ScriptFormat is whatever the model declared. Empty is the common case
	// and still means JavaScript: the handler sends every script task to goja
	// regardless, so the format is reported rather than filtered on.
	ScriptFormat string `json:"script_format,omitzero"`

	// Script is the body, because deciding between FEEL and process isolation
	// means reading what these scripts do. Length is alongside it so a caller
	// can rank by size without measuring every string itself — the longest
	// scripts are the ones least likely to survive a translation to FEEL.
	Script       string `json:"script"`
	ScriptLength int    `json:"script_length"`
}
