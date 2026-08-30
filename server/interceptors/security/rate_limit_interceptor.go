package security

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

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

	mu           sync.Mutex
	windows      map[string]*clientRequestWindow
	requestCount int
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
		windows:     make(map[string]*clientRequestWindow, cleanupEveryRequests),
	}
}

func (i *rateLimitInterceptor) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !i.allow(clientKeyFromRequest(r)) {
			w.Header().Set("Retry-After", strconv.Itoa(int(i.window.Seconds())))
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (i *rateLimitInterceptor) allow(clientKey string) bool {
	now := i.now()

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

func clientKeyFromRequest(r *http.Request) string {
	xForwardedFor := r.Header.Get("X-Forwarded-For")
	if xForwardedFor != "" {
		client, _, _ := strings.Cut(xForwardedFor, ",")
		if trimmedClient := strings.TrimSpace(client); trimmedClient != "" {
			return trimmedClient
		}
	}

	if host, ok := hostFromRemoteAddr(r.RemoteAddr); ok {
		return host
	}

	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}

	return "unknown"
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
