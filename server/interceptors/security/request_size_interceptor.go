package security

import (
	"net/http"

	"github.com/gsoultan/metis/server/interceptors/contracts"
)

const defaultMaxBodyBytes int64 = 2 << 20

type requestSizeInterceptor struct {
	maxBodyBytes int64
}

func NewRequestSizeInterceptor(maxBodyBytes int64) contracts.TransportInterceptor {
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultMaxBodyBytes
	}

	return &requestSizeInterceptor{maxBodyBytes: maxBodyBytes}
}

func (i *requestSizeInterceptor) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || isReadOnlyHTTPMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		if r.ContentLength > i.maxBodyBytes {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, i.maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func isReadOnlyHTTPMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}
