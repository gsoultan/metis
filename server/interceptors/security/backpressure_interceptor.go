package security

import (
	"net/http"
	"strconv"

	"github.com/gsoultan/metis/server/interceptors/contracts"
)

const (
	defaultMaxInFlightRequests = 128
	defaultMaxQueuedRequests   = 256
	retryAfterSeconds          = 1
)

type backpressureInterceptor struct {
	inFlight chan struct{}
	queue    chan struct{}
}

func NewBackpressureInterceptor(maxInFlightRequests, maxQueuedRequests int) contracts.TransportInterceptor {
	if maxInFlightRequests <= 0 {
		maxInFlightRequests = defaultMaxInFlightRequests
	}

	if maxQueuedRequests <= 0 {
		maxQueuedRequests = defaultMaxQueuedRequests
	}

	return &backpressureInterceptor{
		inFlight: make(chan struct{}, maxInFlightRequests),
		queue:    make(chan struct{}, maxQueuedRequests),
	}
}

func (i *backpressureInterceptor) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !i.tryEnqueue() {
			i.writeBusyResponse(w)
			return
		}

		select {
		case i.inFlight <- struct{}{}:
			i.dequeue()
			defer i.releaseInFlight()
			next.ServeHTTP(w, r)
		case <-r.Context().Done():
			i.dequeue()
			http.Error(w, "Request canceled before execution", http.StatusRequestTimeout)
		}
	})
}

func (i *backpressureInterceptor) tryEnqueue() bool {
	select {
	case i.queue <- struct{}{}:
		return true
	default:
		return false
	}
}

func (i *backpressureInterceptor) dequeue() {
	<-i.queue
}

func (i *backpressureInterceptor) releaseInFlight() {
	<-i.inFlight
}

func (i *backpressureInterceptor) writeBusyResponse(w http.ResponseWriter) {
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	http.Error(w, "Server is busy, retry later", http.StatusServiceUnavailable)
}
