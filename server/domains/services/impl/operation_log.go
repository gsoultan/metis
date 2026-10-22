package impl

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// operationLogService reads the audit log from the trail.
type operationLogService struct {
	repo repositories.Repository
}

// NewOperationLogService returns the audit log, read from repo's trail.
func NewOperationLogService(repo repositories.Repository) contracts.OperationLogService {
	return &operationLogService{repo: repo}
}

// ListOperations returns a page of what people did in the caller's
// organization, newest first.
//
// The organization is the one the request acts in, and the trail's own scope
// keeps every row to its projects; an administrator of another organization
// is refused here rather than shown an empty log, so a wrong organization is
// said and not mistaken for a quiet one.
func (s *operationLogService) ListOperations(ctx context.Context, query entities.OperationQuery) (entities.OperationPage, error) {
	caller := signedIn(ctx)
	organization := entities.ActingOrganization(ctx)
	if caller == nil || caller.Username == "" || organization == uuid.Nil ||
		!caller.HoldsRoleIn(organization, entities.RoleAdmin) {
		return entities.OperationPage{}, apierr.Forbiddenf("only an administrator of the organization can read its audit log")
	}
	filter, err := operationFilter(query)
	if err != nil {
		return entities.OperationPage{}, err
	}
	// One more than the page, to know whether there is a next one without a
	// second query.
	limit := filter.Limit
	filter.Limit++
	rows, err := s.repo.Audit().ListOperations(ctx, filter)
	if err != nil {
		return entities.OperationPage{}, err
	}
	page := entities.OperationPage{}
	if int64(len(rows)) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		if last.Seq != nil {
			page.Next = encodeOperationCursor(repocontracts.OperationCursor{At: last.CreatedAt, Seq: *last.Seq})
		}
	}
	page.Operations = make([]entities.AuditEntry, 0, len(rows))
	for _, row := range rows {
		entry := adapters.AuditEntityAdapter{Model: row}.ToEntity()
		// The log says what was done in words, and who; the data behind an
		// entry stays on the instance's own history, where it is read with
		// the instance.
		entry.Data = nil
		page.Operations = append(page.Operations, entry)
	}
	return page, nil
}

// operationFilter checks a query and turns it into the trail's filter.
func operationFilter(query entities.OperationQuery) (repocontracts.OperationFilter, error) {
	filter := repocontracts.OperationFilter{
		Actor:     strings.TrimSpace(query.Actor),
		Type:      strings.TrimSpace(query.Type),
		ProjectID: query.ProjectID,
		From:      query.From,
		To:        query.To,
		Limit:     int64(query.Limit),
	}
	if len(filter.Actor) > 255 {
		return filter, apierr.Invalidf("actor is longer than any username")
	}
	if len(filter.Type) > 64 {
		return filter, apierr.Invalidf("type is longer than any kind of entry")
	}
	if !filter.From.IsZero() && !filter.To.IsZero() && !filter.From.Before(filter.To) {
		return filter, apierr.Invalidf("from must be before to")
	}
	if filter.Limit <= 0 {
		filter.Limit = entities.OperationPageDefault
	}
	filter.Limit = min(filter.Limit, entities.OperationPageMax)
	if query.Before != "" {
		cursor, err := decodeOperationCursor(query.Before)
		if err != nil {
			return filter, apierr.Invalidf("before is not a place in the audit log; send the next value of the page before")
		}
		filter.Before = &cursor
	}
	return filter, nil
}

// encodeOperationCursor is a place in the log as the caller carries it:
// opaque, so nobody builds one by hand and depends on its shape.
func encodeOperationCursor(cursor repocontracts.OperationCursor) string {
	return base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, "%d.%d", cursor.At.UnixNano(), cursor.Seq))
}

func decodeOperationCursor(raw string) (repocontracts.OperationCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return repocontracts.OperationCursor{}, err
	}
	at, seq, ok := strings.Cut(string(decoded), ".")
	if !ok {
		return repocontracts.OperationCursor{}, errors.New("no separator")
	}
	nanos, err := strconv.ParseInt(at, 10, 64)
	if err != nil {
		return repocontracts.OperationCursor{}, err
	}
	number, err := strconv.ParseInt(seq, 10, 64)
	if err != nil {
		return repocontracts.OperationCursor{}, err
	}
	return repocontracts.OperationCursor{At: time.Unix(0, nanos), Seq: number}, nil
}

// unavailableOperationLog is the Null Object for a facade built without the
// audit log.
type unavailableOperationLog struct{}

// NewUnavailableOperationLogService returns an audit log that refuses every
// read, for a facade built without the real one.
func NewUnavailableOperationLogService() contracts.OperationLogService {
	return unavailableOperationLog{}
}

func (unavailableOperationLog) ListOperations(context.Context, entities.OperationQuery) (entities.OperationPage, error) {
	return entities.OperationPage{}, errors.New("the audit log is unavailable: this server was wired without it")
}
