package archive

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/services"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// The page sizes of a project's archive: what a page is unless asked for
// otherwise, and the most it can be.
const (
	defaultPage = 50
	maxPage     = 200
)

// cursorSeparator joins the two halves of a cursor: when the last row of a
// page finished, and its id.
const cursorSeparator = "_"

// Endpoints list a project's archive and open one archived instance.
type Endpoints struct {
	ListArchivedInstances endpoint.Endpoint
	GetArchivedInstance   endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListArchivedInstances: makeListArchivedEndpoint(s),
		GetArchivedInstance:   makeGetArchivedEndpoint(s),
	}
}

func makeListArchivedEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListArchivedRequest)
		if !ok {
			return nil, fmt.Errorf("archive: expected a ListArchivedRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ListArchivedResponse{Err: apierr.Invalidf("project id %q is not a valid identifier", req.ProjectID)}, nil
		}
		filter, err := filterOf(req)
		if err != nil {
			return ListArchivedResponse{Err: err}, nil
		}
		page, err := s.ListArchivedInstances(ctx, projectID, filter)
		if err != nil {
			return ListArchivedResponse{Err: err}, nil
		}
		views := make([]ArchivedView, 0, len(page))
		for _, m := range page {
			views = append(views, ViewOf(m))
		}
		return ListArchivedResponse{Instances: views, NextCursor: nextCursor(page, filter.Limit)}, nil
	}
}

func makeGetArchivedEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(GetArchivedRequest)
		if !ok {
			return nil, fmt.Errorf("archive: expected a GetArchivedRequest, got %T", request)
		}
		id, err := uuid.Parse(req.InstanceID)
		if err != nil {
			return GetArchivedResponse{Err: apierr.Invalidf("instance id %q is not a valid identifier", req.InstanceID)}, nil
		}
		summary, record, err := s.GetArchivedInstance(ctx, id)
		if err != nil {
			return GetArchivedResponse{Err: err}, nil
		}
		view := ViewOf(summary)
		return GetArchivedResponse{Instance: &view, Record: &record}, nil
	}
}

// filterOf reads the list's narrowing from the address.
func filterOf(req ListArchivedRequest) (repocontracts.ArchiveFilter, error) {
	filter := repocontracts.ArchiveFilter{
		ProcessKey:  strings.TrimSpace(req.ProcessKey),
		BusinessKey: strings.TrimSpace(req.BusinessKey),
		Limit:       defaultPage,
	}
	if req.Limit != "" {
		limit, err := strconv.ParseInt(req.Limit, 10, 64)
		if err != nil || limit < 1 {
			return repocontracts.ArchiveFilter{}, apierr.Invalidf("limit %q is not a whole number from 1", req.Limit)
		}
		filter.Limit = min(limit, maxPage)
	}
	if req.Cursor == "" {
		return filter, nil
	}
	finished, id, found := strings.Cut(req.Cursor, cursorSeparator)
	at, timeErr := time.Parse(time.RFC3339Nano, finished)
	after, idErr := uuid.Parse(id)
	if !found || timeErr != nil || idErr != nil {
		return repocontracts.ArchiveFilter{}, apierr.Invalidf("cursor %q is not one this list gave out", req.Cursor)
	}
	filter.FinishedBefore, filter.AfterID = at, after
	return filter, nil
}

// nextCursor is the position after the page's last row, when the page was
// full and there may be more; empty when the page was the last.
func nextCursor(page []models.ArchivedInstanceModel, limit int64) string {
	if int64(len(page)) < limit {
		return ""
	}
	last := page[len(page)-1]
	return last.FinishedAt.UTC().Format(time.RFC3339Nano) + cursorSeparator + last.ID.String()
}
