package common

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/gsoultan/metis/server/endpoints"
)

// EncodeResponse writes a reply as JSON.
//
// A reply that failed is answered as its failure, with the status of the
// failure's class, and nothing of the reply. That is asked first: a reply
// that also names a status of its own never gets to give it for a failure.
//
// A reply that names its status is answered with it. It is for a reply that
// is not "done" — a request that was taken and now waits for somebody else is
// a 202 — and every reply that names none is the 200 it always was.
//
// The reply is written out in full before anything is sent. A status, once
// sent, cannot be taken back: were it sent first, a reply that then could not
// be written would be answered "accepted", or "ok", over a body that says the
// server failed. So a reply that cannot be written is returned as the error
// it is, with nothing sent, and whoever serves the route answers it as the
// server's — a 500, as it was before any reply named a status.
//
// What is sent is what the stream encoder wrote, byte for byte: the same
// encoder writes it, into memory first, the line it ends with included.
func EncodeResponse(ctx context.Context, w http.ResponseWriter, response any) error {
	if f, ok := response.(endpoints.Failer); ok && f.Failed() != nil {
		// EncodeError has written the failure to the caller. Returning it again
		// here would have go-kit write a second reply over the first.
		EncodeError(ctx, f.Failed(), w)
		return nil //nolint:nilerr // the error is reported to the caller by EncodeError, not swallowed
	}
	body, pooled := replyBuffers.Get().(*bytes.Buffer)
	if !pooled {
		body = new(bytes.Buffer)
	}
	defer keepReplyBuffer(body)
	body.Reset()
	if err := json.NewEncoder(body).Encode(response); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if coder, ok := response.(interface{ StatusCode() int }); ok {
		w.WriteHeader(coder.StatusCode())
	}
	_, err := w.Write(body.Bytes())
	return err
}

// replyBuffers holds the memory replies are written into before they are
// sent, so that writing a reply out first costs a route no allocation of its
// own once the server is warm.
var replyBuffers = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// maxKeptReplyBuffer is the largest buffer kept for the next reply. One reply
// of many megabytes must not leave that much held for as long as the server
// runs.
const maxKeptReplyBuffer = 256 << 10

// keepReplyBuffer hands a buffer back for the next reply, unless it has grown
// past what is worth keeping.
func keepReplyBuffer(body *bytes.Buffer) {
	if body.Cap() <= maxKeptReplyBuffer {
		replyBuffers.Put(body)
	}
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
	// A caller's own mistake is not a server failure. Reporting it as one
	// spends the 0.1% 5xx error budget the engine is measured against, and
	// pages whoever is on call for a request that was answered correctly.
	case errors.Is(err, apierr.ErrInvalidArgument):
		return http.StatusBadRequest
	case errors.Is(err, apierr.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, apierr.ErrForbidden):
		return http.StatusForbidden
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

// LimitParam reads ?limit= from a request: the most rows the caller wants,
// or zero for no limit.
//
// Read like PageParams: a value that is not a positive whole number is
// treated as absent, because a malformed limit is not worth failing a read
// over, and absent means what it meant before the parameter existed.
func LimitParam(r *http.Request) int {
	return atoiOrZero(r.URL.Query().Get("limit"))
}

// OffsetParam reads ?offset= from a request: how many rows to skip, or zero.
// Read like LimitParam, and for the same reason.
func OffsetParam(r *http.Request) int {
	return atoiOrZero(r.URL.Query().Get("offset"))
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// ReadLimited reads at most limit bytes, refusing anything longer.
//
// A separate bound from whatever the caller already applied: ParseMultipartForm
// spills past its own limit to a temporary file rather than refusing, so that
// limit bounds memory but not what a subsequent read pulls back into it.
func ReadLimited(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("could not read the upload: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("the upload is larger than %d bytes", limit)
	}
	return body, nil
}
