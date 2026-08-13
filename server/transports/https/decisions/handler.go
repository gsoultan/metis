package decisions

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/gobpm/server/endpoints/decision"
	"github.com/gsoultan/gobpm/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps decision.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/decisions", httptransport.NewServer(
		eps.ListDecisions,
		decodeListDecisionsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/decisions", httptransport.NewServer(
		eps.CreateDecision,
		decodeCreateDecisionRequest,
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
}

func decodeListDecisionsRequest(_ context.Context, r *http.Request) (any, error) {
	page, pageSize := common.PageParams(r)
	return decision.ListDecisionsRequest{
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
