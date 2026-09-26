package decisions

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/decision"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps decision.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/decisions", httptransport.NewServer(
		eps.ListDecisions,
		decodeListDecisionsRequest,
		common.EncodeResponse,
		options...,
	))
	// Every key once, as its newest version, without the tables: for the
	// dependency graph and a step's decision picker.
	m.Handle("GET /api/v1/decisions/summaries", httptransport.NewServer(
		eps.ListSummaries,
		decodeListDecisionSummariesRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/decisions", httptransport.NewServer(
		eps.CreateDecision,
		decodeCreateDecisionRequest,
		common.EncodeResponse,
		options...,
	))
	// The table run against its own examples.
	m.Handle("POST /api/v1/decisions/{id}/tests/run", httptransport.NewServer(
		eps.RunTests,
		decodeRunDecisionTestsRequest,
		common.EncodeResponse,
		options...,
	))
	// What depends on this decision, before somebody changes it.
	m.Handle("GET /api/v1/decisions/{id}/impact", httptransport.NewServer(
		eps.DecisionImpact,
		decodeDecisionImpactRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/decisions/{id}", httptransport.NewServer(
		eps.GetDecision,
		decodeGetDecisionRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("DELETE /api/v1/decisions/{id}", httptransport.NewServer(
		eps.DeleteDecision,
		decodeDeleteDecisionRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("PUT /api/v1/decisions/{id}", httptransport.NewServer(
		eps.UpdateDecision,
		decodeUpdateDecisionRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/decisions/evaluate", httptransport.NewServer(
		eps.EvaluateDecision,
		decodeEvaluateDecisionRequest,
		common.EncodeResponse,
		options...,
	))

	// A key's version history, and making one of its versions live. The key
	// travels in the query and the body rather than the path, for the reason
	// the process routes give: a wildcard segment here would be ambiguous
	// against /decisions/{id}/..., which ServeMux refuses at registration.
	m.Handle("GET /api/v1/decisions/versions", httptransport.NewServer(
		eps.ListDecisionVersions,
		decodeListDecisionVersionsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/decisions/versions/promote", httptransport.NewServer(
		eps.PromoteDecision,
		decodePromoteDecisionRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeListDecisionVersionsRequest(_ context.Context, r *http.Request) (any, error) {
	return decision.ListDecisionVersionsRequest{
		ProjectID: r.URL.Query().Get("project_id"),
		Key:       r.URL.Query().Get("key"),
	}, nil
}

func decodePromoteDecisionRequest(_ context.Context, r *http.Request) (any, error) {
	var req decision.PromoteDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeListDecisionsRequest(_ context.Context, r *http.Request) (any, error) {
	page, pageSize := common.PageParams(r)
	return decision.ListDecisionsRequest{
		ProjectID: r.URL.Query().Get("project_id"),
		Search:    r.URL.Query().Get("q"),
		Page:      page,
		PageSize:  pageSize,
	}, nil
}

func decodeListDecisionSummariesRequest(_ context.Context, r *http.Request) (any, error) {
	page, pageSize := common.PageParams(r)
	return decision.ListDecisionSummariesRequest{
		ProjectID: r.URL.Query().Get("project_id"),
		Page:      page,
		PageSize:  pageSize,
	}, nil
}

func decodeCreateDecisionRequest(_ context.Context, r *http.Request) (any, error) {
	var req decision.CreateDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeGetDecisionRequest(_ context.Context, r *http.Request) (any, error) {
	return decision.GetDecisionRequest{ID: r.PathValue("id")}, nil
}

func decodeDeleteDecisionRequest(_ context.Context, r *http.Request) (any, error) {
	return decision.DeleteDecisionRequest{ID: r.PathValue("id")}, nil
}

func decodeUpdateDecisionRequest(_ context.Context, r *http.Request) (any, error) {
	var req decision.UpdateDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	req.ID = r.PathValue("id")
	return req, nil
}

func decodeEvaluateDecisionRequest(_ context.Context, r *http.Request) (any, error) {
	var req decision.EvaluateDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeDecisionImpactRequest(_ context.Context, r *http.Request) (any, error) {
	return decision.DecisionImpactRequest{ID: r.PathValue("id")}, nil
}

func decodeRunDecisionTestsRequest(_ context.Context, r *http.Request) (any, error) {
	return decision.RunDecisionTestsRequest{ID: r.PathValue("id")}, nil
}
