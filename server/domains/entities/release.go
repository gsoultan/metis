package entities

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// ReleaseFormat names the shape of a release bundle, so an installation that
// reads a later one can tell rather than guess.
const ReleaseFormat = "metis.release/v1"

// MaxReleaseNameLength is the column's size, in characters.
const MaxReleaseNameLength = 255

// ReleaseState is where a release is in one runtime.
type ReleaseState string

const (
	// ReleaseCut is a release recorded where it was cut. Its versions are the
	// ones already there; cutting changes nothing that runs.
	ReleaseCut ReleaseState = "cut"
	// ReleaseStaged is a release imported and checked, not yet live.
	ReleaseStaged ReleaseState = "staged"
	// ReleaseLive is the release new instances start on.
	ReleaseLive ReleaseState = "live"
	// ReleaseSuperseded was live and another release has since gone live.
	ReleaseSuperseded ReleaseState = "superseded"
)

// ReleaseOrigin is how a runtime came to hold a release.
type ReleaseOrigin string

const (
	ReleaseOriginCut      ReleaseOrigin = "cut"
	ReleaseOriginImported ReleaseOrigin = "imported"
)

// Kinds of release item.
const (
	ReleaseItemProcess  = "process"
	ReleaseItemDecision = "decision"
)

// ReleaseBundle is a release as it travels: the models, whole, without
// anything that only means something in the runtime they came from — ids,
// version numbers, the project — and with every step naming its connection
// by name.
type ReleaseBundle struct {
	Format    string               `json:"format"`
	Name      string               `json:"name"`
	CutAt     time.Time            `json:"cut_at"`
	CutBy     string               `json:"cut_by,omitzero"`
	Processes []*ProcessDefinition `json:"processes"`
	Decisions []DecisionDefinition `json:"decisions"`
	// Connections are the connection names the processes call through. Each
	// must exist in a runtime before the release can be imported there.
	Connections []string `json:"connections"`
	// Checked is what the release passed where it was cut, before it was
	// cut: its decisions' own tests and its processes' saved simulation
	// cases. It is inside the hash, so an installation that trusts the
	// signature also knows the release was checked at the source. A release
	// cut before releases were checked has none.
	Checked *ReleaseChecks `json:"checked,omitempty"`
}

// ReleaseChecks is what a release passed where it was cut.
type ReleaseChecks struct {
	At              time.Time `json:"at"`
	DecisionTests   int       `json:"decision_tests"`
	SimulationCases int       `json:"simulation_cases"`
}

// ReleaseItem is one model of a release, as it is in one runtime.
type ReleaseItem struct {
	Kind string `json:"kind"`
	Key  string `json:"key"`
	// Version is the version this runtime holds it as. Zero means the key is
	// new here and is stored when the release goes live: the first version of
	// a key is live the moment it exists, so storing it at import would put
	// it into force before anyone said so.
	Version int `json:"version"`
	// SourceVersion is the version it was where the release was cut.
	SourceVersion int `json:"source_version"`
}

// Release is a set of process and decision versions that move from one
// environment to the next together, unchanged.
type Release struct {
	ID        uuid.UUID
	CreatedAt time.Time
	Project   *Project
	Name      string
	Hash      string
	Origin    ReleaseOrigin
	State     ReleaseState
	Bundle    ReleaseBundle
	Items     []ReleaseItem
	CutBy     string
	ActedBy   string
	// ImportedBy is who imported it here; ApprovedBy who else agreed it may
	// go live. A protected runtime requires them to be two people, neither of
	// them the one who cut it.
	ImportedBy string
	ApprovedBy string
	LiveAt     *time.Time
	// Previous is the release that was live before this one went live here.
	Previous string
	// LiveNote is another environment's signed note that this release went
	// live there, attached here before it goes live. Nil when there is none.
	LiveNote *ReleaseLiveNote
}

// ReleaseLiveNoteFormat names the shape of a release's live note.
const ReleaseLiveNoteFormat = "metis.release-live/v1"

// ReleaseLiveNote is one environment's signed word that a release went live
// there: staging's, carried to production, which can require it before the
// same release goes live there too.
//
// The signature is over ReleaseLiveNoteStatement, by the key named. The
// release is named by its hash, so the note is for exactly that content.
type ReleaseLiveNote struct {
	Format    string    `json:"format"`
	Release   string    `json:"release"`
	Hash      string    `json:"hash"`
	LiveAt    time.Time `json:"live_at"`
	Key       string    `json:"key"`
	Signature string    `json:"signature"`
}

// ReleaseLiveNoteStatement is the bytes a live note's signature covers:
// everything in it but the key and the signature, as JSON in a fixed order.
func ReleaseLiveNoteStatement(n ReleaseLiveNote) ([]byte, error) {
	statement, err := json.Marshal(struct {
		Format  string    `json:"format"`
		Release string    `json:"release"`
		Hash    string    `json:"hash"`
		LiveAt  time.Time `json:"live_at"`
	}{n.Format, n.Release, n.Hash, n.LiveAt.UTC()})
	if err != nil {
		return nil, fmt.Errorf("could not write the release's live note: %w", err)
	}
	return statement, nil
}

// HashReleaseBundle is the SHA-256 of a bundle's JSON, which two runtimes
// compare to know they hold the same release.
//
// Computed over the decoded bundle re-encoded, not the bytes as received:
// struct fields encode in a fixed order and map keys sorted, so a bundle
// that went through a jsonb column and back hashes the same.
func HashReleaseBundle(b ReleaseBundle) (string, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return "", fmt.Errorf("could not encode the release: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// WalkNodes visits every node of the definition, those inside sub-processes
// included.
func (pd *ProcessDefinition) WalkNodes(fn func(*Node)) {
	pd.traverseNodes(fn)
}

// Properties a step names another model by, and pins a version of it with.
const (
	DecisionKeyProperty           = "decision_key"
	AssignmentDecisionKeyProperty = "assignment_decision_key"
	decisionVersionProperty       = "decision_version"
	calledElementProperty         = "called_element"
	calledProcessKeyProperty      = "called_process_key"
	calledElementVersionProperty  = "called_element_version"
	calledProcessVersionProperty  = "called_process_version"
)

// DecisionKeys are the decisions the definition's steps consult, each once,
// sorted.
func (pd *ProcessDefinition) DecisionKeys() []string {
	var keys []string
	pd.WalkNodes(func(n *Node) {
		for _, p := range []string{DecisionKeyProperty, AssignmentDecisionKeyProperty} {
			if k := n.GetStringProperty(p); k != "" {
				keys = append(keys, k)
			}
		}
	})
	slices.Sort(keys)
	return slices.Compact(keys)
}

// CalledProcessKeys are the processes the definition's call activities
// start, each once, sorted.
func (pd *ProcessDefinition) CalledProcessKeys() []string {
	var keys []string
	pd.WalkNodes(func(n *Node) {
		k := n.GetStringProperty(calledElementProperty)
		if k == "" {
			k = n.GetStringProperty(calledProcessKeyProperty)
		}
		if k != "" {
			keys = append(keys, k)
		}
	})
	slices.Sort(keys)
	return slices.Compact(keys)
}

// PinnedVersionStep names a step that consults a decision or calls a process
// at a fixed version number, or "" when none does.
//
// A release moves models between runtimes whose version numbers differ, so a
// pinned number would name a different model, or none, where it arrives.
func (pd *ProcessDefinition) PinnedVersionStep() string {
	found := ""
	pd.WalkNodes(func(n *Node) {
		if found != "" {
			return
		}
		for _, p := range []string{decisionVersionProperty, calledElementVersionProperty, calledProcessVersionProperty} {
			if pinsVersion(n.Properties[p]) {
				found = n.ID
				return
			}
		}
	})
	return found
}

func pinsVersion(v any) bool {
	switch x := v.(type) {
	case float64:
		return x != 0
	case int:
		return x != 0
	case string:
		return x != "" && x != "0"
	}
	return false
}
