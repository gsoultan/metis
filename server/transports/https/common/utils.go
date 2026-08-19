package common

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/gobpm/internal/pkg/auth"
	"github.com/gsoultan/gobpm/internal/pkg/redaction"
	"github.com/gsoultan/gobpm/server/endpoints"
)

func EncodeResponse(ctx context.Context, w http.ResponseWriter, response any) error {
	if f, ok := response.(endpoints.Failer); ok && f.Failed() != nil {
		// EncodeError has written the failure to the caller. Returning it again
		// here would have go-kit write a second reply over the first.
		EncodeError(ctx, f.Failed(), w)
		return nil //nolint:nilerr // the error is reported to the caller by EncodeError, not swallowed
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(w).Encode(response)
}

func EncodeError(_ context.Context, err error, w http.ResponseWriter) {
	if err == nil {
		panic("EncodeError with nil error")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(CodeFrom(err))
	// The reply itself failing leaves nothing to fall back on — the status is
	// already sent — so this only records that the caller never heard why.
	if writeErr := json.NewEncoder(w).Encode(map[string]any{
		"error": redaction.RedactError(err),
	}); writeErr != nil {
		log.Debug().Err(writeErr).Msg("Could not write the error reply to the caller")
	}
}

func CodeFrom(err error) int {
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, auth.ErrAuthenticationFailed):
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

// PageParams reads ?page= and ?page_size= from a request.
//
// Zero means "not supplied", which the pagination contract reads as the first
// page at the server default — so a caller that knows nothing about paging
// still gets a bounded response rather than everything.
//
// A value that is not a number is treated as absent rather than as an error: a
// malformed page number is not worth failing a read over, and the contract
// clamps whatever it is given anyway.
func PageParams(r *http.Request) (page int, pageSize int) {
	return atoiOrZero(r.URL.Query().Get("page")), atoiOrZero(r.URL.Query().Get("page_size"))
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}
