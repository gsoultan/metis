package fcm

import (
	"crypto/sha256"
	"sync"

	"github.com/gsoultan/metis/internal/pkg/lru"
)

// poolCapacity bounds how many service accounts keep a signed-in client. One
// per project with push is the expected count; the bound is for the
// installation with many projects, not a tuning knob.
const poolCapacity = 128

// Pool keeps one Client per service account key, so a burst of pushes signs in
// to Google once rather than once per message.
type Pool struct {
	mu      sync.Mutex
	clients *lru.Cache[[sha256.Size]byte, *Client]
	opts    []Option
}

// NewPool returns an empty pool whose clients are built with opts.
func NewPool(opts ...Option) *Pool {
	return &Pool{clients: lru.New[[sha256.Size]byte, *Client](poolCapacity), opts: opts}
}

// Client returns the client for a service account key, parsing it the first
// time. Keyed by a hash of the key file, so a replaced key gets a new client
// and the old one ages out.
func (p *Pool) Client(serviceAccountKey string) (*Client, error) {
	id := sha256.Sum256([]byte(serviceAccountKey))
	p.mu.Lock()
	defer p.mu.Unlock()
	if client, ok := p.clients.Get(id); ok {
		return client, nil
	}
	account, err := ParseServiceAccount(serviceAccountKey)
	if err != nil {
		return nil, err
	}
	client := NewClient(account, p.opts...)
	p.clients.Put(id, client)
	return client, nil
}
