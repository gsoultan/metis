package deviation

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
	ListInstanceDeviations endpoint.Endpoint
	DeviateInstance        endpoint.Endpoint

	// The requests that wait for a second administrator: read, and decided.
	ListDeviationRequests   endpoint.Endpoint
	GetDeviationRequest     endpoint.Endpoint
	ApproveDeviationRequest endpoint.Endpoint
	RejectDeviationRequest  endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListInstanceDeviations:  MakeListInstanceDeviationsEndpoint(s),
		DeviateInstance:         MakeDeviateInstanceEndpoint(s),
		ListDeviationRequests:   MakeListDeviationRequestsEndpoint(s),
		GetDeviationRequest:     MakeGetDeviationRequestEndpoint(s),
		ApproveDeviationRequest: MakeApproveDeviationRequestEndpoint(s),
		RejectDeviationRequest:  MakeRejectDeviationRequestEndpoint(s),
	}
}

func MakeListInstanceDeviationsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListInstanceDeviationsRequest)
		if !ok {
			return nil, fmt.Errorf("deviation: expected a ListInstanceDeviationsRequest, got %T", request)
		}
		id, err := uuid.Parse(req.InstanceID)
		if err != nil {
			return ListInstanceDeviationsResponse{Err: apierr.Invalidf("instance id %q is not a valid identifier", req.InstanceID)}, nil
		}
		rows, err := s.ListInstanceDeviations(ctx, id)
		if err != nil {
			return ListInstanceDeviationsResponse{Err: err}, nil
		}
		views := make([]DeviationView, 0, len(rows))
		for _, row := range rows {
			views = append(views, ViewOf(row))
		}
		return ListInstanceDeviationsResponse{Deviations: views}, nil
	}
}

// MakeDeviateInstanceEndpoint waives the step one running instance waits at,
// cancels the instance or holds it, where it stands.
//
// It answers a preview unless the request says otherwise: the only request
// that changes anything is one that says "dry_run": false. Who is asking is
// the service's to read from the request's context, for a preview as for an
// apply, so a preview refuses what the apply would.
//
// A refusal travels in the reply's Err, which the transport answers with the
// status of its class: what the caller can fix is a 400, an instance the
// caller cannot see is a 404, and anything else is the server's.
func MakeDeviateInstanceEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeviateInstanceRequest)
		if !ok {
			return nil, fmt.Errorf("deviation: expected a DeviateInstanceRequest, got %T", request)
		}
		id, err := uuid.Parse(req.InstanceID)
		if err != nil {
			return DeviateInstanceResponse{Err: apierr.Invalidf("instance id %q is not a valid identifier", req.InstanceID)}, nil
		}
		outcome, err := s.DeviateInstance(ctx, entities.DeviationCommand{
			InstanceID: id,
			Kind:       entities.DeviationKind(req.Kind),
			NodeID:     req.NodeID,
			Reason:     req.Reason,
			Outputs:    req.Outputs,
			VisitKey:   req.VisitKey,
			DryRun:     req.dryRun(),
		})
		if err != nil {
			return DeviateInstanceResponse{Err: err}, nil
		}
		return responseOf(outcome), nil
	}
}
