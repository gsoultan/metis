// Package operationlog is the audit log's endpoint: who did what across the
// caller's organization, newest first.
package operationlog

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
)

// Endpoints reads the audit log.
type Endpoints struct {
	ListOperations endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{ListOperations: makeListEndpoint(s)}
}

func makeListEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListRequest)
		if !ok {
			return nil, fmt.Errorf("audit log: expected a ListRequest, got %T", request)
		}
		query, err := queryOf(req)
		if err != nil {
			return ListResponse{Err: err}, nil
		}
		page, err := s.ListOperations(ctx, query)
		if err != nil {
			return ListResponse{Err: err}, nil
		}
		views := make([]OperationView, 0, len(page.Operations))
		for _, entry := range page.Operations {
			views = append(views, ViewOf(entry))
		}
		return ListResponse{Operations: views, Next: page.Next}, nil
	}
}

// queryOf reads a request's filter, refusing a value that is not what its
// parameter takes rather than ignoring it: a log narrowed by a filter the
// server dropped reads as a log with nothing in it.
func queryOf(req ListRequest) (entities.OperationQuery, error) {
	query := entities.OperationQuery{Actor: req.Actor, Type: req.Type, Before: req.Before}
	var err error
	if req.ProjectID != "" {
		if query.ProjectID, err = uuid.Parse(req.ProjectID); err != nil {
			return query, apierr.Invalidf("project %q is not a valid identifier", req.ProjectID)
		}
	}
	if query.From, err = momentOf("from", req.From); err != nil {
		return query, err
	}
	if query.To, err = momentOf("to", req.To); err != nil {
		return query, err
	}
	if req.Limit != "" {
		if _, err := fmt.Sscanf(req.Limit, "%d", &query.Limit); err != nil || query.Limit < 1 {
			return query, apierr.Invalidf("limit must be a whole number from 1 to %d", entities.OperationPageMax)
		}
	}
	return query, nil
}

func momentOf(name, raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, apierr.Invalidf("%s must be a moment such as 2026-10-22T09:00:00Z", name)
	}
	return at, nil
}
