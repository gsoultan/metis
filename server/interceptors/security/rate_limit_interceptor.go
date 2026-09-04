package security

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gsoultan/metis/internal/pkg/sharedcount"
	"github.com/gsoultan/metis/server/interceptors/contracts"
)

const (
	defaultMaxRequestsPerWindow = 240
	defaultRateLimitWindow      = time.Minute
	cleanupEveryRequests        = 128
	staleWindowMultiplier       = 2
)

type clientRequestWindow struct {
	windowStart  time.Time
	requestCount int
}

type rateLimitInterceptor struct {
	maxRequests int
	window      time.Duration
	now         func() time.Time

	// shared carries this replica's counts to the others, so the limit is the
	// installation's rather than each process's. Nil on a single-replica
	// deployment and in tests, where the local window is the whole truth.
	//
	// It does not replace the local window: that is still what admits or
	// refuses, on every request, without touching a database. The shared count
	// only *lowers* the local allowance to this replica's share of what the
	// installation has already spent.
	shared *sharedcount.Counter

	// trusted decides whether this request's X-Forwarded-For may be believed.
	// Without it the header — which any client can set — chose the bucket, so
	// varying it per request bought unlimited requests.
	trusted *trustedProxySet

	mu           sync.Mutex
	windows      map[string]*clientRequestWindow
	requestCount int
}

// SharedLimiter is a rate limiter whose count can be pooled across replicas.
type SharedLimiter interface {
	contracts.TransportInterceptor
	ShareVia(counter *sharedcount.Counter)
}

func NewRateLimitInterceptor(maxRequests int, window time.Duration) contracts.TransportInterceptor {
	if maxRequests <= 0 {
		maxRequests = defaultMaxRequestsPerWindow
	}
	if window <= 0 {
		window = defaultRateLimitWindow
	}

	return &rateLimitInterceptor{
		maxRequests: maxRequests,
		window:      window,
		now:         time.Now,
		trusted:     loadTrustedProxies(),
		windows:     make(map[string]*clientRequestWindow, cleanupEveryRequests),
	}
}

func (i *rateLimitInterceptor) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !i.allow(i.clientKey(r)) {
			w.Header().Set("Retry-After", strconv.Itoa(int(i.window.Seconds())))
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ShareVia gives the interceptor a cross-replica counter.
//
// Without one the limit is per-process, which with N replicas admits N times
// what was configured — the whole reason a single replica was the supported
// topology.
// ShareVia satisfies SharedLimiter.
func (i *rateLimitInterceptor) ShareVia(counter *sharedcount.Counter) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.shared = counter
}

func (i *rateLimitInterceptor) allow(clientKey string) bool {
	now := i.now()

	// Asked before the lock, because the counter has its own and taking them in
	// two orders in two places is how a deadlock is written.
	//
	// The total is a lower bound: it holds this replica's own count plus what
	// the others had reported at the last exchange. So a limiter built on it
	// refuses late rather than early, which is the right direction to be wrong
	// — a legitimate client is never refused because of a count that turned out
	// not to exist.
	if shared := i.sharedCounter(); shared != nil {
		if shared.Add(clientKey, now) > int64(i.maxRequests) {
			return false
		}
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	i.requestCount++
	if i.requestCount%cleanupEveryRequests == 0 {
		staleBefore := now.Add(-(staleWindowMultiplier * i.window))
		i.cleanupStaleWindows(staleBefore)
	}

	window, exists := i.windows[clientKey]
	if !exists {
		i.windows[clientKey] = &clientRequestWindow{windowStart: now, requestCount: 1}
		return true
	}

	if now.Sub(window.windowStart) >= i.window {
		window.windowStart = now
		window.requestCount = 1
		return true
	}

	if window.requestCount >= i.maxRequests {
		return false
	}

	window.requestCount++
	return true
}

func (i *rateLimitInterceptor) cleanupStaleWindows(staleBefore time.Time) {
	for clientKey, window := range i.windows {
		if window.windowStart.Before(staleBefore) {
			delete(i.windows, clientKey)
		}
	}
}

// clientKey decides which bucket a request draws from.
//
// It used to read X-Forwarded-For and believe it. That header is set by the
// client, so an attacker sending a different value on each request was handed a
// fresh bucket every time and the limit never applied: measured, one address
// got 30 requests through a limit of 3. The header is now only consulted when
// the request actually arrived from a proxy we trust — see trustedProxySet.
func (i *rateLimitInterceptor) clientKey(r *http.Request) string {
	return i.trusted.clientAddr(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
}

func hostFromRemoteAddr(remoteAddr string) (string, bool) {
	if remoteAddr == "" {
		return "", false
	}

	if strings.HasPrefix(remoteAddr, "[") {
		closingBracket := strings.LastIndex(remoteAddr, "]")
		if closingBracket > 0 && closingBracket+1 < len(remoteAddr) && remoteAddr[closingBracket+1] == ':' {
			host := remoteAddr[1:closingBracket]
			if host != "" {
				return host, true
			}
		}

		return "", false
	}

	lastColon := strings.LastIndexByte(remoteAddr, ':')
	if lastColon <= 0 || lastColon == len(remoteAddr)-1 {
		return "", false
	}

	if strings.Contains(remoteAddr[:lastColon], ":") {
		return "", false
	}

	return remoteAddr[:lastColon], true
}

func (i *rateLimitInterceptor) sharedCounter() *sharedcount.Counter {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.shared
}
