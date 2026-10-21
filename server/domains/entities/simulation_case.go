package entities

import (
	"time"

	"github.com/google/uuid"
)

// MaxSimulationCasesPerProcess bounds the cases one process keeps.
//
// The same number as a batch runs at once, so a process's whole suite is one
// "Run all" — and a list somebody could otherwise grow without end is a list
// every designer load reads in full.
const MaxSimulationCasesPerProcess = 20

// MaxSimulationCaseNameLength is the column's size, in characters.
const MaxSimulationCaseNameLength = 255

// SimulationCase is one saved test case for a process.
//
// Kept by process key rather than definition version, because a case is
// written against the process and its value is in running it against the
// *next* version: the one somebody is about to deploy.
//
// No JSON or ORM tags: an endpoint maps it to a view, and a repository maps it
// to a row.
type SimulationCase struct {
	ID         uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Project    *Project
	ProcessKey string
	Name       string

	Scenario SimulationScenario
	Expect   SimulationExpectation
}

// SimulationScenario is what a case starts with and how it is answered: the
// parts of a SimulationRequest that belong to the case rather than to the
// process it runs against.
type SimulationScenario struct {
	Variables map[string]any     `json:"variables"`
	Answers   []SimulationAnswer `json:"answers"`
	Seed      int64              `json:"seed"`
}

// SimulationExpectation is what a case must end with. Every field is optional,
// and an expectation with none set asks for nothing — the case then only
// records a scenario somebody wants to keep.
type SimulationExpectation struct {
	// Outcome is how the run must end: completed, needs_answer, incident.
	Outcome SimulationOutcome `json:"outcome,omitempty"`
	// EndsAt is the node a completed run must end at, or the node an unanswered
	// one must stop at.
	EndsAt string `json:"ends_at,omitempty"`
	// Variables are values the run must end holding. Compared as JSON values,
	// so 4200 and 4200.0 are the same number.
	Variables map[string]any `json:"variables,omitempty"`
}

// IsExpectable says whether an outcome is one a case may expect.
//
// A step budget or a timeout is the simulation giving up, never a result a
// process is meant to reach, so a case cannot be written to expect one.
func IsExpectable(outcome SimulationOutcome) bool {
	switch outcome {
	case "", SimulationOutcomeCompleted, SimulationOutcomeNeedsAnswer, SimulationOutcomeIncident:
		return true
	default:
		return false
	}
}
