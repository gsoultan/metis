package security

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rs/zerolog/log"

	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors/contracts"
)

const (
	defaultIdempotencyTTL         = 15 * time.Minute
	idempotencyCleanupEvery       = 128
	idempotencyKeyHeader          = "Idempotency-Key"
	idempotencyReplayHeader       = "Idempotency-Replayed"
	idempotencyConflictStatusCode = http.StatusConflict
)

type idempotencyInterceptor struct {
	store IdempotencyStore
	ttl   time.Duration
}

// NewIdempotencyInterceptor keeps records in this process.
//
// Correct for a single replica, which is the supported topology; a deployment
// running more than one wants NewIdempotencyInterceptorWithStore over the
// database, or a retry reaching another replica executes the write again.
func NewIdempotencyInterceptor(ttl time.Duration) contracts.TransportInterceptor {
	if ttl <= 0 {
		ttl = defaultIdempotencyTTL
	}
	return NewIdempotencyInterceptorWithStore(NewMemoryIdempotencyStore(ttl), ttl)
}

// NewIdempotencyInterceptorWithStore lets the composition root decide where
// records live.
func NewIdempotencyInterceptorWithStore(store IdempotencyStore, ttl time.Duration) contracts.TransportInterceptor {
	if ttl <= 0 {
		ttl = defaultIdempotencyTTL
	}
	return &idempotencyInterceptor{store: store, ttl: ttl}
}

func (i *idempotencyInterceptor) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idempotencyKey := strings.TrimSpace(r.Header.Get(idempotencyKeyHeader))
		if idempotencyKey == "" || isReadOnlyHTTPMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		requestHash, err := hashRequest(r)
		if err != nil {
			i.writeRequestHashError(w, err)
			return
		}

		ctx := r.Context()
		storageKey := idempotencyStorageKey(r, idempotencyKey)

		outcome, err := i.store.Claim(ctx, storageKey, requestHash)
		if err != nil {
			// The store is how this endpoint knows whether the work has already
			// been done. Guessing "probably not" would execute a duplicate,
			// which is the one outcome the header exists to prevent, so the
			// caller is told to retry instead.
			log.Error().Err(err).Msg("Could not claim an idempotency key; refusing rather than risking a duplicate")
			http.Error(w, "Idempotency store unavailable; retry this request", http.StatusServiceUnavailable)
			return
		}

		switch {
		case outcome.Conflict:
			http.Error(w, "Idempotency key already used with a different request", idempotencyConflictStatusCode)
			return
		case outcome.Response != nil:
			writeIdempotencyResult(w, resultFrom(outcome.Response), true)
			return
		case outcome.InFlight():
			i.waitAndReplay(w, r, storageKey)
			return
		}

		i.executeAndRecord(w, r, next, storageKey)
	})
}

// executeAndRecord runs the handler and stores what it produced.
func (i *idempotencyInterceptor) executeAndRecord(w http.ResponseWriter, r *http.Request, next http.Handler, storageKey string) {
	// Derived once, and deliberately detached from the request: the record is
	// what makes a retry safe, so a client that hangs up mid-flight must not
	// prevent it being written or released.
	recordCtx := context.WithoutCancel(r.Context())

	recorded := false
	// A handler that panics still holds the claim. Releasing it on the way out
	// means the client's retry runs the work rather than waiting out the budget
	// for an answer nobody is going to write.
	defer func() {
		if !recorded {
			if err := i.store.Abandon(recordCtx, storageKey); err != nil {
				log.Warn().Err(err).Msg("Could not release an abandoned idempotency claim")
			}
		}
	}()

	result := i.captureResponse(next, r)

	if err := i.store.Complete(recordCtx, storageKey, StoredResponse{
		StatusCode: result.statusCode,
		Header:     result.header,
		Body:       result.body,
	}); err != nil {
		log.Error().Err(err).Msg("Could not record an idempotent response; a retry of this request will execute again")
	}
	recorded = true

	writeIdempotencyResult(w, result, false)
}

func resultFrom(response *StoredResponse) *idempotencyResult {
	return &idempotencyResult{statusCode: response.StatusCode, header: response.Header, body: response.Body}
}

type idempotencyResult struct {
	statusCode int
	header     http.Header
	body       []byte
}

func (i *idempotencyInterceptor) writeRequestHashError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	http.Error(w, "Failed to read request body", http.StatusBadRequest)
}

// waitAndReplay blocks on the caller already running this exact request.
func (i *idempotencyInterceptor) waitAndReplay(w http.ResponseWriter, r *http.Request, storageKey string) {
	response, err := i.store.Await(r.Context(), storageKey)
	switch {
	case err != nil:
		// Either the client gave up or the wait budget expired. Both are "no
		// answer yet", and a retry is safe because the key is what makes it so.
		http.Error(w, "Still processing the original request; retry with the same Idempotency-Key", http.StatusRequestTimeout)
	case response == nil:
		// The holder abandoned the claim without recording anything.
		http.Error(w, "The original request did not complete; retry with the same Idempotency-Key", http.StatusServiceUnavailable)
	default:
		writeIdempotencyResult(w, resultFrom(response), true)
	}
}

func (i *idempotencyInterceptor) captureResponse(next http.Handler, r *http.Request) *idempotencyResult {
	capture := newResponseCaptureWriter()
	next.ServeHTTP(capture, r)
	return capture.result()
}

func idempotencyStorageKey(r *http.Request, idempotencyKey string) string {
	tenant, principal := callerIdentity(r.Context())
	return r.Method + "\n" + r.URL.Path + "\n" + tenant + "\n" + principal + "\n" + idempotencyKey
}

// callerIdentity returns the tenant and principal this request is acting as.
//
// Both are best-effort by design: this interceptor also sits in front of
// requests that carry no principal yet, and an unauthenticated caller is not a
// reason to refuse a write the chain below is happy to serve. What matters is
// that two *different* callers never produce the same string — an empty
// identity is its own bucket, not a shared one.
func callerIdentity(ctx context.Context) (tenant, principal string) {
	if tc, ok := entities.TenantContextFrom(ctx); ok {
		tenant = tc.TenantID
	}

	switch u := ctx.Value(pkgauth.UserContextKey).(type) {
	case entities.User:
		principal = principalOf(u)
	case *entities.User:
		if u != nil {
			principal = principalOf(*u)
		}
	case pkgauth.UserClaims:
		principal = claimsPrincipal(u)
	case *pkgauth.UserClaims:
		if u != nil {
			principal = claimsPrincipal(*u)
		}
	}
	return tenant, principal
}

// principalOf prefers the stable id over the username, which is editable.
func principalOf(u entities.User) string {
	if u.ID != uuid.Nil {
		return u.ID.String()
	}
	return u.Username
}

// claimsPrincipal prefers the token subject, the one field an OIDC provider
// guarantees is stable and unique.
func claimsPrincipal(c pkgauth.UserClaims) string {
	if c.Subject != "" {
		return c.Subject
	}
	return c.Username
}

func hashRequest(r *http.Request) (string, error) {
	hasher := sha256.New()
	hasher.Write([]byte(r.Method))
	hasher.Write([]byte("\n"))
	hasher.Write([]byte(r.URL.RequestURI()))
	hasher.Write([]byte("\n"))
	hasher.Write([]byte(r.Header.Get("Content-Type")))

	body, err := readRequestBody(r)
	if err != nil {
		return "", err
	}

	hasher.Write([]byte("\n"))
	hasher.Write(body)
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func readRequestBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func writeIdempotencyResult(w http.ResponseWriter, result *idempotencyResult, replayed bool) {
	for headerKey, values := range result.header {
		for _, value := range values {
			w.Header().Add(headerKey, value)
		}
	}

	if replayed {
		w.Header().Set(idempotencyReplayHeader, "true")
	}

	w.WriteHeader(result.statusCode)
	if len(result.body) == 0 {
		return
	}

	if _, err := w.Write(result.body); err != nil {
		log.Debug().Err(err).Msg("The caller went away before the replayed reply could be written")
	}
}

type responseCaptureWriter struct {
	header      http.Header
	body        bytes.Buffer
	statusCode  int
	wroteHeader bool
}

func newResponseCaptureWriter() *responseCaptureWriter {
	return &responseCaptureWriter{
		header:     make(http.Header),
		statusCode: http.StatusOK,
	}
}

func (w *responseCaptureWriter) Header() http.Header {
	return w.header
}

func (w *responseCaptureWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(data)
}

func (w *responseCaptureWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}

	w.wroteHeader = true
	w.statusCode = statusCode
}

func (w *responseCaptureWriter) result() *idempotencyResult {
	return &idempotencyResult{
		statusCode: w.statusCode,
		header:     maps.Clone(w.header),
		body:       bytes.Clone(w.body.Bytes()),
	}
}
