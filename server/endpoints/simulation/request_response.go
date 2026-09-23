package simulation

import (
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// The public shape of a simulation request.
//
// Spelled in snake_case and decoded by hand rather than reusing the entity,
// because this is the surface the Go SDK and every other integrator speaks. An
// entity that gains a field gains it here only when somebody decides it should
// be public, which is the difference between an API and an accident.
type SimulateRequest struct {
	ProjectID     string `json:"project_id"`
	DefinitionKey string `json:"definition_key"`
	// Version 0 means whatever is live — what a caller who does not care sends.
	Version int `json:"version"`

	Variables map[string]any  `json:"variables"`
	Answers   []AnswerPayload `json:"answers"`

	// BPMNXML and ReplayInstanceID are declared but not yet run.
	//
	// Declared, because the decoder refuses fields it does not know and a
	// client sending either would otherwise get "this request body could not be
	// read" — which says nothing about which mode is missing. Named here, they
	// get an answer that does: deploy a version, or wait for replay.
	BPMNXML          string `json:"bpmn_xml"`
	ReplayInstanceID string `json:"replay_instance_id"`

	// ClockStart fixes where virtual time begins. A caller writing a CI
	// assertion sets it; a trace that moves with the wall clock cannot be
	// asserted on.
	ClockStart time.Time `json:"clock_start"`

	Seed     int64 `json:"seed"`
	MaxSteps int   `json:"max_steps"`
}

// AnswerPayload is what the outside world says when the process asks.
type AnswerPayload struct {
	Node string `json:"node"`
	Kind string `json:"kind"`

	Actor string `json:"actor"`
	After string `json:"after"`

	Returns map[string]any `json:"returns"`
	Fails   string         `json:"fails"`

	ArrivesAfter string `json:"arrives_after"`
}

// ToEntity converts the wire request, reporting the project id it could not read.
//
// The project id is the one field with no safe default: everything else has a
// documented zero meaning ("version 0 is live", "no answers yet"), and guessing
// a project would run somebody's definition under a scope they did not name.
func (r SimulateRequest) ToEntity() (entities.SimulationRequest, error) {
	projectID, err := uuid.Parse(r.ProjectID)
	if err != nil {
		return entities.SimulationRequest{}, err
	}

	answers := make([]entities.SimulationAnswer, 0, len(r.Answers))
	for _, answer := range r.Answers {
		answers = append(answers, entities.SimulationAnswer{
			Node:         answer.Node,
			Kind:         entities.SimulationAnswerKind(answer.Kind),
			Actor:        answer.Actor,
			After:        answer.After,
			Returns:      answer.Returns,
			Fails:        answer.Fails,
			ArrivesAfter: answer.ArrivesAfter,
		})
	}

	return entities.SimulationRequest{
		ProjectID:     projectID,
		DefinitionKey: r.DefinitionKey,
		Version:       r.Version,
		Variables:     r.Variables,
		Answers:       answers,
		ClockStart:    r.ClockStart,
		Seed:          r.Seed,
		MaxSteps:      r.MaxSteps,
	}, nil
}

// BatchRequest runs a whole suite of cases.
//
// One request rather than one per case because each run holds a database
// transaction open for its length, on the pool real instances are also using.
// N parallel calls from a browser is N concurrent transactions; one call is one
// at a time.
type BatchRequest struct {
	Scenarios []SimulateRequest `json:"scenarios"`
}

// BatchResponse keeps the order it was given, so a caller can line results up
// against the cases it sent without matching on anything.
type BatchResponse struct {
	Runs []entities.SimulationRun `json:"runs"`
}

// MaxBatchScenarios bounds one batch. Twenty cases is a generous suite and a
// bounded amount of work; an unbounded list is a way to hold a connection for
// as long as the caller likes.
const MaxBatchScenarios = 20
