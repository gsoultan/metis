package https

import (
	"net/http"

	"github.com/rs/zerolog/log"
)

// securityHeaders wraps the API with the response headers an enterprise buyer
// expects to find.
//
// The engine serves no pages: the web app is metis-server
// (github.com/gsoultan/metis-server), which forwards the browser's API calls
// here. So the policy is the strictest there is. Nothing a response carries
// may load, run or be framed; a JSON body opened in a browser tab stays inert.
func securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		// HSTS is only meaningful over TLS, which this server does not terminate
		// itself; the ingress adds it. Set it anyway so a direct HTTPS terminator
		// in front inherits a sane default. Harmless over plain HTTP: browsers
		// ignore it there.
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// notAnAPIPathBody is notAnAPIPath's answer, in the API's error shape. A
// constant, so nothing on this path can fail before the write.
const notAnAPIPathBody = `{"err":"Not found. This is the Metis engine's API, under /api/v1; the web app is served by metis-server."}` + "\n"

// notAnAPIPath answers every path the engine does not serve.
//
// The engine used to serve the web app at "/", so a browser pointed at it now
// is most likely somebody who has not heard the web app moved. The answer says
// where it went, in the API's own error shape.
func notAnAPIPath(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	if _, err := w.Write([]byte(notAnAPIPathBody)); err != nil {
		log.Debug().Err(err).Msg("Could not write the not-found response")
	}
}
