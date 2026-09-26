package tenantscope

import (
	"context"
	"slices"
	"sync"

	"github.com/google/uuid"
)

// Request is the scope one request has resolved, kept for as long as the
// request lasts.
//
// A scoped repository call works out what the caller may see by reading the
// ids of every project in the caller's organization, and a request makes
// several such calls — the dashboard's statistics make six, completing a task
// nine. Each read the list again, so an organization paid for the size of its
// project list once per call: at ten thousand projects, four milliseconds and
// ten megabytes a call (docs/performance.md). A request now reads it once, and
// every scoped call in it reuses that.
//
// Kept for one request rather than cached across requests. A cache across
// requests has to be invalidated by whoever changes the projects, and the window
// where it is not is a project somebody created and cannot use. The only
// projects a request changes are changed through it, and creating or deleting
// one forgets the list, so the next scoped call in the same request reads it
// again.
//
// Staleness cannot widen a tenant. A project never changes organization — the
// project repository refuses to move one — so every id kept here belongs to the
// organization it is kept for. What a request can miss is a project another
// request created while it ran, and what it can still see is one another
// request deleted while it ran: both its own organization's, and neither for
// longer than the request.
//
// Bound to the one organization the tenant resolver chose. A context that names
// another — the account service asks each of an account's organizations in turn
// — is answered from the database every time, as before. So is one with no
// request at all: background work, the event stream and the message consumers
// hold their context for as long as they run, and what they read is read fresh.
type Request struct {
	organization string

	mu       sync.Mutex
	projects []uuid.UUID
	resolved bool
	// generation counts Forget calls, so a read that was under way when the
	// projects changed is not kept.
	generation uint64
	ended      bool
}

type requestKey struct{}

// WithRequest gives ctx somewhere to keep organization's scope for the length
// of one request. The caller ends it with End when the request is over; from
// then on nothing is answered from it, whoever still holds the context.
func WithRequest(ctx context.Context, organization string) (context.Context, *Request) {
	request := &Request{organization: organization}
	return context.WithValue(ctx, requestKey{}, request), request
}

// RequestFrom returns the request ctx belongs to, or nil outside one. Every
// method of a nil Request keeps nothing and reads every time.
func RequestFrom(ctx context.Context) *Request {
	request, ok := ctx.Value(requestKey{}).(*Request)
	if !ok {
		return nil
	}
	return request
}

// Projects returns organization's project ids, calling read for them the first
// time the request asks and reusing the answer after that.
//
// The ids come back shared by every scoped call in the request: read them,
// never write them. A failed read is not kept, so the next call tries again.
func (r *Request) Projects(organization string, read func() ([]uuid.UUID, error)) ([]uuid.UUID, error) {
	if r == nil {
		return read()
	}
	r.mu.Lock()
	if r.kept(organization) {
		projects := r.projects
		r.mu.Unlock()
		return projects, nil
	}
	generation := r.generation
	r.mu.Unlock()

	// Read without the lock: it is a database round trip, and a lock held
	// across one is a lock whose holder can be waiting on a connection that a
	// goroutine queued behind the lock is holding.
	projects, err := read()
	if err != nil {
		return nil, err
	}
	// Clipped, so an append by any caller copies rather than writing past the
	// end into what the next caller is handed.
	projects = slices.Clip(projects)

	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.ended && organization == r.organization && generation == r.generation {
		r.projects, r.resolved = projects, true
	}
	return projects, nil
}

// kept reports whether organization's projects are held. The caller holds mu.
func (r *Request) kept(organization string) bool {
	return r.resolved && !r.ended && organization == r.organization
}

// Forget drops the kept projects, because the organization's projects changed
// during the request. The next scoped call reads them again.
func (r *Request) Forget() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projects, r.resolved = nil, false
	r.generation++
}

// End closes the request. Anything still holding its context — a goroutine the
// request started — reads the projects itself from here on.
func (r *Request) End() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projects, r.resolved, r.ended = nil, false, true
}
