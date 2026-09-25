package https

import (
	"sync"
)

// The event stream is a request that lasts as long as a tab stays open.
//
// It used to go through the API's backpressure limiter like any other request
// and hold one of its 128 in-flight slots for its whole life, so 128 open tabs
// — across everybody, not per person — stalled every other call the API
// answered. Streams are counted separately now, against limits of their own.
const (
	// maxEventStreams bounds every stream this process holds open. Each is a
	// goroutine, a small buffer and a socket.
	maxEventStreams = 2048
	// maxEventStreamsPerAccount stops one account — a script, a runaway tab
	// reloading itself — from taking the rest.
	maxEventStreamsPerAccount = 16
)

// eventStreamLimit counts open streams, in total and per account.
//
// The map is keyed by the authenticated account, never by anything a caller
// sends, and it cannot outgrow maxEventStreams: an entry exists only while its
// account holds at least one stream, and is deleted when the last one closes.
type eventStreamLimit struct {
	mu         sync.Mutex
	open       int
	perAccount map[string]int
	total      int
	eachLimit  int
}

func newEventStreamLimit(total, perAccount int) *eventStreamLimit {
	return &eventStreamLimit{perAccount: make(map[string]int), total: total, eachLimit: perAccount}
}

// streamRefusal says why a stream was not opened.
type streamRefusal int

const (
	streamAllowed streamRefusal = iota
	streamServerFull
	streamAccountFull
)

// acquire opens a stream for account, or says why it cannot.
func (l *eventStreamLimit) acquire(account string) streamRefusal {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.open >= l.total {
		return streamServerFull
	}
	if l.perAccount[account] >= l.eachLimit {
		return streamAccountFull
	}
	l.open++
	l.perAccount[account]++
	return streamAllowed
}

// release closes a stream acquire opened.
func (l *eventStreamLimit) release(account string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.open--
	if l.perAccount[account] <= 1 {
		delete(l.perAccount, account)
		return
	}
	l.perAccount[account]--
}
