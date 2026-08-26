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
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rs/zerolog/log"

	pkgauth "github.com/gsoultan/gobpm/internal/pkg/auth"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/interceptors/contracts"
)

const (
	defaultIdempotencyTTL         = 15 * time.Minute
	idempotencyCleanupEvery       = 128
	idempotencyKeyHeader          = "Idempotency-Key"
	idempotencyReplayHeader       = "Idempotency-Replayed"
	idempotencyConflictStatusCode = http.StatusConflict
)

type idempotencyResult struct {
	statusCode int
	header     http.Header
	body       []byte
}

type idempotencyEntry struct {
	requestHash string
	createdAt   time.Time
	done        chan struct{}
	result      *idempotencyResult
}

type idempotencyInterceptor struct {
	ttl time.Duration
	now func() time.Time

	mu           sync.Mutex
	entries      map[string]*idempotencyEntry
	requestCount int
}

func NewIdempotencyInterceptor(ttl time.Duration) contracts.TransportInterceptor {
	if ttl <= 0 {
		ttl = defaultIdempotencyTTL
	}

	return &idempotencyInterceptor{
		ttl:     ttl,
		now:     time.Now,
		entries: make(map[string]*idempotencyEntry, idempotencyCleanupEvery),
	}
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

		storageKey := idempotencyStorageKey(r, idempotencyKey)
		entry, created, conflict := i.getOrCreateEntry(storageKey, requestHash)
		if conflict {
			http.Error(w, "Idempotency key already used with a different request", idempotencyConflictStatusCode)
			return
		}

		if !created {
			i.waitAndReplay(w, r, entry)
			return
		}

		result := i.captureResponse(next, r)
		i.completeEntry(storageKey, result)
		writeIdempotencyResult(w, result, false)
	})
}

func (i *idempotencyInterceptor) writeRequestHashError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	http.Error(w, "Failed to read request body", http.StatusBadRequest)
}

func (i *idempotencyInterceptor) waitAndReplay(w http.ResponseWriter, r *http.Request, entry *idempotencyEntry) {
	select {
	case <-entry.done:
		if entry.result == nil {
			http.Error(w, "Idempotent response unavailable", http.StatusInternalServerError)
			return
		}
		writeIdempotencyResult(w, entry.result, true)
	case <-r.Context().Done():
		http.Error(w, "Request canceled while waiting for idempotent response", http.StatusRequestTimeout)
	}
}

func (i *idempotencyInterceptor) captureResponse(next http.Handler, r *http.Request) *idempotencyResult {
	capture := newResponseCaptureWriter()
	next.ServeHTTP(capture, r)
	return capture.result()
}

func (i *idempotencyInterceptor) getOrCreateEntry(storageKey, requestHash string) (*idempotencyEntry, bool, bool) {
	now := i.now()

	i.mu.Lock()
	defer i.mu.Unlock()

	i.requestCount++
	if i.requestCount%idempotencyCleanupEvery == 0 {
		staleBefore := now.Add(-i.ttl)
		i.cleanupStaleEntries(staleBefore)
	}

	if existing, ok := i.entries[storageKey]; ok {
		if entryExpired(existing, now, i.ttl) {
			delete(i.entries, storageKey)
		} else {
			if existing.requestHash != requestHash {
				return nil, false, true
			}
			return existing, false, false
		}
	}

	entry := &idempotencyEntry{
		requestHash: requestHash,
		createdAt:   now,
		done:        make(chan struct{}),
	}
	i.entries[storageKey] = entry
	return entry, true, false
}

func (i *idempotencyInterceptor) completeEntry(storageKey string, result *idempotencyResult) {
	now := i.now()

	i.mu.Lock()
	defer i.mu.Unlock()

	entry, ok := i.entries[storageKey]
	if !ok {
		return
	}

	entry.result = result
	entry.createdAt = now
	close(entry.done)
}

func (i *idempotencyInterceptor) cleanupStaleEntries(staleBefore time.Time) {
	for key, entry := range i.entries {
		if entry.createdAt.Before(staleBefore) && entryCompleted(entry) {
			delete(i.entries, key)
		}
	}
}

func entryCompleted(entry *idempotencyEntry) bool {
	select {
	case <-entry.done:
		return true
	default:
		return false
	}
}

func entryExpired(entry *idempotencyEntry, now time.Time, ttl time.Duration) bool {
	if !entryCompleted(entry) {
		return false
	}

	return now.Sub(entry.createdAt) > ttl
}

// idempotencyStorageKey identifies one caller's one request.
//
// **The principal is part of the key, and has to be.** The value of the header
// is chosen by the client, and clients choose obvious things — an order number,
// a business reference, "1". Keyed on method, path and header alone, two
// tenants picking the same value meet in the same cache entry, and the second
// one is handed the first one's response body while their own write never runs
// at all. That is a cross-tenant disclosure and a silently dropped command in
// the same exchange, which is why AGENTS §2.3 states that a cache ignoring who
// asked is a data leak.
//
// Scoping is by tenant *and* user: two people in one organization retrying with
// the same key are still two different commands.
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
