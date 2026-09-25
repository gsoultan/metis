package impl

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/rs/zerolog/log"
)

// SSEObserver holds the browsers connected to this process and delivers events
// to the ones an event is for.
//
// Every client is registered with a scope — its organization and, when it
// connected on an environment's port, that environment. Delivery compares the
// two. Before it did, the registry was a flat set: a signed-in browser received
// every organization's process variables, live, including the ones from a
// runtime it had no access to.
type SSEObserver struct {
	mu      sync.RWMutex
	clients map[chan string]entities.SSEScope

	// publish hands an encoded event to the shared bus so replicas other than
	// this one can deliver it. Nil on a single-replica installation and in
	// tests, where local delivery is the whole story — a nil check is cheaper
	// than a null-object indirection on a path that runs per event.
	publish func(scope entities.SSEScope, payload string)

	// resolveProject answers which organization a project belongs to.
	//
	// Needed because background work — the job worker, the timer sweep — runs
	// under a system context with no tenant, so the event it produces has no
	// organization on it. Without an answer the event is not delivered at all,
	// which is the right failure: a stale list beats another tenant's data.
	resolveProject func(ctx context.Context, projectID uuid.UUID) (uuid.UUID, bool)

	// orgOfProject caches what resolveProject answered. A project's
	// organization does not change, and the alternative is a database read on
	// the path that produced the event.
	orgOfProject map[uuid.UUID]uuid.UUID

	// afterCommit holds an event back until the transaction that produced it
	// has committed, and drops it if that transaction rolls back. Nil in tests
	// that deliver at once. See DeliverAfterCommitWith.
	afterCommit func(ctx context.Context, fn func())
}

func NewSSEObserver() *SSEObserver {
	return &SSEObserver{
		clients:      make(map[chan string]entities.SSEScope),
		orgOfProject: make(map[uuid.UUID]uuid.UUID),
	}
}

// PublishVia gives the observer a shared bus to put events on.
//
// Delivery to this process's own clients does not go through the bus: it
// happens inline, so a browser connected here sees its own replica's events at
// the speed it always did. The bus exists only to reach browsers connected
// somewhere else.
func (o *SSEObserver) PublishVia(publish func(scope entities.SSEScope, payload string)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.publish = publish
}

// ResolveProjectsWith supplies the project-to-organization lookup.
//
// Set at composition, where the repository lives. Without it, events from
// background work cannot be scoped and are not delivered — reported once per
// event rather than silently, because "the list stopped updating" is otherwise
// unattributable.
func (o *SSEObserver) ResolveProjectsWith(resolve func(ctx context.Context, projectID uuid.UUID) (uuid.UUID, bool)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.resolveProject = resolve
}

// DeliverAfterCommitWith has each event wait for the transaction that
// produced it. Set at composition, where the unit of work lives.
//
// An event is a hint to refetch. Sent from inside the transaction, a browser
// that refetched at once read the state from before and got no second hint,
// and a hint about work that then rolled back pointed at something that never
// happened: a completion whose next step failed still told the inbox the task
// was done.
func (o *SSEObserver) DeliverAfterCommitWith(afterCommit func(ctx context.Context, fn func())) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.afterCommit = afterCommit
}

// OnEvent delivers a process event to the browsers it belongs to.
//
// Scoped and encoded now, from the event as it stands and the context that
// produced it; only the delivery waits for the commit.
func (o *SSEObserver) OnEvent(ctx context.Context, event entities.ProcessEvent) {
	scope := o.scopeOf(ctx, event)
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	o.mu.RLock()
	afterCommit := o.afterCommit
	o.mu.RUnlock()
	if afterCommit == nil {
		o.BroadcastTo(scope, json.RawMessage(payload))
		return
	}
	afterCommit(ctx, func() { o.BroadcastTo(scope, json.RawMessage(payload)) })
}

// BroadcastTo encodes one event and delivers it within a scope.
//
// There is no unscoped broadcast. Making the scope an argument rather than an
// option is what stops a future caller from delivering to everybody by
// forgetting something.
func (o *SSEObserver) BroadcastTo(scope entities.SSEScope, data any) {
	if !scope.Resolved() {
		// Not delivered, and said out loud. An event nobody can be shown is a
		// bug in whoever produced it — a dispatch that lost its tenant — and
		// the alternative reading of an empty scope is "everybody".
		log.Debug().Msg("An event was produced with no organization, so it was not delivered to any browser.")
		return
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	payload := string(jsonData)

	o.deliverLocally(scope, payload)

	o.mu.RLock()
	publish := o.publish
	o.mu.RUnlock()
	if publish != nil {
		publish(scope, payload)
	}
}

// DeliverFromPeer delivers an event another replica produced. It does not
// re-publish: the bus is where this came from, and putting it back would make
// every event circulate forever.
//
// The scope travels with the payload on the bus, because the receiving replica
// has no context to recover it from — the request that produced the event
// happened somewhere else.
func (o *SSEObserver) DeliverFromPeer(scope entities.SSEScope, payload string) {
	if !scope.Resolved() {
		return
	}
	o.deliverLocally(scope, payload)
}

// deliverLocally sends one encoded payload to the browsers it is for.
//
// A client whose buffer is full is skipped rather than waited for. That is
// deliberate and predates the scoping: one browser on a slow connection must
// not hold up delivery to every other, and the UI treats an event as a hint to
// refetch rather than as data, so a dropped one costs a stale list until the
// next refetch — not a lost update.
func (o *SSEObserver) deliverLocally(scope entities.SSEScope, payload string) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	msg := fmt.Sprintf("data: %s\n\n", payload)
	for clientChan, clientScope := range o.clients {
		if !scope.Delivers(clientScope) {
			continue
		}
		select {
		case clientChan <- msg:
		default:
			// Client slow or disconnected
		}
	}
}

// AddClient registers a browser and what it may be shown.
func (o *SSEObserver) AddClient(scope entities.SSEScope) chan string {
	o.mu.Lock()
	defer o.mu.Unlock()
	ch := make(chan string, 10)
	o.clients[ch] = scope
	return ch
}

func (o *SSEObserver) RemoveClient(ch chan string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.clients, ch)
	close(ch)
}

// scopeOf works out who a process event is for.
//
// The context answers it for request-driven work, where the tenant came from
// the token and the environment from the listener. Background work has neither,
// so the organization is resolved from the project the event names — which is
// the one thing every process event carries.
func (o *SSEObserver) scopeOf(ctx context.Context, event entities.ProcessEvent) entities.SSEScope {
	scope := entities.SSEScopeFrom(ctx)
	if scope.Resolved() {
		return scope
	}

	projectID := projectOf(event)
	if projectID == uuid.Nil {
		return scope
	}
	if org, ok := o.cachedOrg(projectID); ok {
		scope.Organization = org
		return scope
	}

	o.mu.RLock()
	resolve := o.resolveProject
	o.mu.RUnlock()
	if resolve == nil {
		return scope
	}
	org, ok := resolve(ctx, projectID)
	if !ok {
		return scope
	}

	o.mu.Lock()
	o.orgOfProject[projectID] = org
	o.mu.Unlock()

	scope.Organization = org
	return scope
}

func (o *SSEObserver) cachedOrg(projectID uuid.UUID) (uuid.UUID, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	org, ok := o.orgOfProject[projectID]
	return org, ok
}

// projectOf finds the project an event belongs to, wherever it was carried.
func projectOf(event entities.ProcessEvent) uuid.UUID {
	if event.Project != nil && event.Project.ID != uuid.Nil {
		return event.Project.ID
	}
	if event.Instance != nil && event.Instance.Project != nil {
		return event.Instance.Project.ID
	}
	return uuid.Nil
}
