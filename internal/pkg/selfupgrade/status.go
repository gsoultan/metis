package selfupgrade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Outcome is where an upgrade ended, or the step it is on.
type Outcome string

const (
	OutcomeRunning Outcome = "running"
	// OutcomeDone: the new release is serving.
	OutcomeDone Outcome = "done"
	// OutcomeRefused: stopped before anything on the host changed.
	OutcomeRefused Outcome = "refused"
	// OutcomeRolledBack: the new release did not come up, nothing had
	// migrated, and the previous release is serving again.
	OutcomeRolledBack Outcome = "rolled_back"
	// OutcomeNeedsRestore: the new release did not come up after migrating.
	// Metis is stopped; restoring Backup is the way back.
	OutcomeNeedsRestore Outcome = "needs_restore"
)

// Status is written to status.json at every step, for the Upgrades page and
// for whoever is reading the host afterwards.
type Status struct {
	From        string    `json:"from"`
	To          string    `json:"to"`
	RequestedBy string    `json:"requested_by,omitempty"`
	Outcome     Outcome   `json:"outcome"`
	Step        string    `json:"step"`
	Detail      string    `json:"detail,omitempty"`
	Backup      string    `json:"backup,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// Steps lists every step finished, in order, with how it went.
	Steps []StepRecord `json:"steps"`
}

// StepRecord is one finished step.
type StepRecord struct {
	Name   string    `json:"name"`
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at"`
}

// writeStatus replaces path atomically, so a reader never sees half a file.
func writeStatus(path string, s Status) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".status-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ReadStatus reads the last upgrade's status. A missing file is no upgrade.
func ReadStatus(path string) (*Status, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s Status
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
