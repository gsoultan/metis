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

// The four endpoints below read and decide the requests that wait for a
// second administrator. Each is shaped as MakeDeviateInstanceEndpoint is: a
// refusal travels in the reply's Err, which the transport answers with the
// status of its class — what the caller can fix is a 400, who may not ask a
// 403, a request the caller cannot see a 404, and anything else the server's.
// None of them answers a refusal as a reply that went well.
//
// Who is asking is the service's to read from the request's context. The
// routes are gated on the administrator role as well; neither check stands in
// for the other.

// requestIDOf reads the id of a request from the address.
func requestIDOf(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apierr.Invalidf("request id %q is not a valid identifier", raw)
	}
	return id, nil
}

// MakeListDeviationRequestsEndpoint lists a page of the queue.
//
// A status the service does not know is the service's to refuse. A project
// that is no id is refused here; one the caller's organization does not have
// lists nothing, as the repository scopes every read.
func MakeListDeviationRequestsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListDeviationRequestsRequest)
		if !ok {
			return nil, fmt.Errorf("deviation: expected a ListDeviationRequestsRequest, got %T", request)
		}
		query := entities.DeviationRequestQuery{Status: entities.DeviationRequestStatus(req.Status), Page: req.Page, PageSize: req.PageSize}
		if req.ProjectID != "" {
			projectID, err := uuid.Parse(req.ProjectID)
			if err != nil {
				return ListDeviationRequestsResponse{Err: apierr.Invalidf("project id %q is not a valid identifier", req.ProjectID)}, nil
			}
			query.Project = &entities.Project{ID: projectID}
		}
		page, total, err := s.ListDeviationRequests(ctx, query)
		if err != nil {
			return ListDeviationRequestsResponse{Err: err}, nil
		}
		return ListDeviationRequestsResponse{Requests: listedViews(page), Total: total}, nil
	}
}

// MakeGetDeviationRequestEndpoint reads one request, whole.
func MakeGetDeviationRequestEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(GetDeviationRequestRequest)
		if !ok {
			return nil, fmt.Errorf("deviation: expected a GetDeviationRequestRequest, got %T", request)
		}
		id, err := requestIDOf(req.ID)
		if err != nil {
			return GetDeviationRequestResponse{Err: err}, nil
		}
		found, err := s.GetDeviationRequest(ctx, id)
		if err != nil {
			return GetDeviationRequestResponse{Err: err}, nil
		}
		return GetDeviationRequestResponse{Request: RequestViewOf(found)}, nil
	}
}

// MakeApproveDeviationRequestEndpoint approves a request and carries out what
// it asked for.
//
// It hands the service the request's id and the approver's note, and nothing
// else: what is carried out is what the request stored.
func MakeApproveDeviationRequestEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DecideDeviationRequestRequest)
		if !ok {
			return nil, fmt.Errorf("deviation: expected a DecideDeviationRequestRequest, got %T", request)
		}
		id, err := requestIDOf(req.ID)
		if err != nil {
			return ApproveDeviationRequestResponse{Err: err}, nil
		}
		outcome, err := s.ApproveDeviationRequest(ctx, id, req.Reason)
		if err != nil {
			return ApproveDeviationRequestResponse{Err: err}, nil
		}
		return approvalOf(outcome), nil
	}
}

// approvalOf maps what an approval did to what the route returns.
func approvalOf(outcome entities.DeviationRequestOutcome) ApproveDeviationRequestResponse {
	response := ApproveDeviationRequestResponse{Request: RequestViewOf(outcome.Request), Applied: outcome.Applied}
	if outcome.Deviation != nil {
		view := ViewOf(*outcome.Deviation)
		response.Deviation = &view
	}
	switch {
	case outcome.WaivePlan != nil:
		response.Plan = PlanViewOf(*outcome.WaivePlan)
	case outcome.MigrationPlan != nil:
		response.Plan = *outcome.MigrationPlan
	}
	return response
}

// MakeRejectDeviationRequestEndpoint ends a request without carrying it out.
// Sent by whoever asked, it is a withdrawal.
func MakeRejectDeviationRequestEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DecideDeviationRequestRequest)
		if !ok {
			return nil, fmt.Errorf("deviation: expected a DecideDeviationRequestRequest, got %T", request)
		}
		id, err := requestIDOf(req.ID)
		if err != nil {
			return RejectDeviationRequestResponse{Err: err}, nil
		}
		rejected, err := s.RejectDeviationRequest(ctx, id, req.Reason)
		if err != nil {
			return RejectDeviationRequestResponse{Err: err}, nil
		}
		return RejectDeviationRequestResponse{Request: RequestViewOf(rejected)}, nil
	}
}
