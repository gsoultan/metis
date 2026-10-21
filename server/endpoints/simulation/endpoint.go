package simulation

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
)

type Endpoints struct {
	Simulate      endpoint.Endpoint
	SimulateBatch endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		Simulate:      MakeSimulateEndpoint(s),
		SimulateBatch: MakeSimulateBatchEndpoint(s),
	}
}

// SimulateResponse carries the whole trace.
//
// A run that ended in an incident is a *result*, not an Err: the engine
// refusing a decision point is the simulation working, and it is the finding
// the caller asked for. Err is for a request that could not be run at all — an
// unknown definition, an unreadable project — so a client can tell "your
// process stops here" from "your request was wrong".
type SimulateResponse struct {
	Run entities.SimulationRun `json:"run,omitzero"`
	Err error                  `json:"-"`
}

func (r SimulateResponse) Failed() error { return r.Err }

type SimulateBatchResponse struct {
	Runs []entities.SimulationRun `json:"runs"`
	Err  error                    `json:"-"`
}

func (r SimulateBatchResponse) Failed() error { return r.Err }

func MakeSimulateEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SimulateRequest)
		if !ok {
			return nil, fmt.Errorf("simulation: expected a SimulateRequest, got %T", request)
		}

		entity, err := runnable(req)
		if err != nil {
			return SimulateResponse{Err: err}, nil
		}

		run, err := s.Simulate(ctx, entity)
		return SimulateResponse{Run: run, Err: err}, nil
	}
}

func MakeSimulateBatchEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(BatchRequest)
		if !ok {
			return nil, fmt.Errorf("simulation: expected a BatchRequest, got %T", request)
		}
		if len(req.Scenarios) > MaxBatchScenarios {
			return SimulateBatchResponse{Err: apierr.Invalidf(
				"a batch runs at most %d cases at once; this one asked for %d",
				MaxBatchScenarios, len(req.Scenarios))}, nil
		}

		// Sequential, not concurrent. Each run holds a transaction for its
		// length; fanning twenty of them at the pool that real instances are
		// using is how a simulation becomes an outage.
		runs := make([]entities.SimulationRun, 0, len(req.Scenarios))
		for i, scenario := range req.Scenarios {
			entity, err := runnable(scenario)
			if err != nil {
				return SimulateBatchResponse{Err: fmt.Errorf("case %d: %w", i, err)}, nil
			}
			run, err := s.Simulate(ctx, entity)
			if err != nil {
				return SimulateBatchResponse{Err: err}, nil
			}
			runs = append(runs, run)
		}

		return SimulateBatchResponse{Runs: runs}, nil
	}
}

// runnable converts one case and refuses what cannot be run.
//
// Shared by the single and the batch endpoint. The batch used to convert its
// cases without these checks, so a case carrying a draft or a replay id was run
// against the live definition instead — a different process from the one the
// caller sent, reported as if it were theirs.
func runnable(req SimulateRequest) (entities.SimulationRequest, error) {
	entity, err := req.ToEntity()
	if err != nil {
		return entities.SimulationRequest{}, apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)
	}
	if req.ReplayInstanceID != "" {
		id, err := uuid.Parse(req.ReplayInstanceID)
		if err != nil {
			return entities.SimulationRequest{}, apierr.Invalidf("replay_instance_id %q is not a valid identifier: %v", req.ReplayInstanceID, err)
		}
		entity.ReplayInstanceID = id
	}
	if entity.Draft != nil && entity.DraftBPMN != "" {
		return entities.SimulationRequest{}, apierr.Invalidf("send the draft once — as definition or as bpmn_xml, not both")
	}
	// A replay with no key runs the instance's own process, as it is live.
	if entity.DefinitionKey == "" && !entity.IsDraft() && entity.ReplayInstanceID == uuid.Nil {
		return entities.SimulationRequest{}, apierr.Invalidf(
			"definition_key is required: name a deployed definition, send a draft as definition or bpmn_xml, or replay an instance")
	}
	return entity, nil
}
