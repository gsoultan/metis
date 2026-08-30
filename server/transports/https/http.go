package https

import (
	"fmt"
	"io/fs"
	"net/http"

	"github.com/gsoultan/metis/internal/pkg/envvar"

	"github.com/rs/zerolog/log"

	"strings"

	"github.com/gsoultan/metis/server/interceptors/tenant"

	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/interceptors"
	"github.com/gsoultan/metis/server/transports/connects"
	"github.com/gsoultan/metis/server/transports/https/collaboration"
	"github.com/gsoultan/metis/server/transports/https/common"
	"github.com/gsoultan/metis/server/transports/https/connectors"
	"github.com/gsoultan/metis/server/transports/https/decisions"
	"github.com/gsoultan/metis/server/transports/https/definitions"
	"github.com/gsoultan/metis/server/transports/https/external_tasks"
	"github.com/gsoultan/metis/server/transports/https/group"
	"github.com/gsoultan/metis/server/transports/https/incidents"
	"github.com/gsoultan/metis/server/transports/https/notification"
	"github.com/gsoultan/metis/server/transports/https/organizations"
	"github.com/gsoultan/metis/server/transports/https/processes"
	"github.com/gsoultan/metis/server/transports/https/projects"
	"github.com/gsoultan/metis/server/transports/https/setup"
	"github.com/gsoultan/metis/server/transports/https/tasks"
	"github.com/gsoultan/metis/server/transports/https/users"
	"github.com/gsoultan/metis/server/transports/https/webhookadmin"
	"github.com/gsoultan/metis/server/transports/https/webhooks"
	"github.com/gsoultan/metis/ui"

	httptransport "github.com/go-kit/kit/transport/http"
)

func NewHTTPHandler(svc services.ServiceFacade, eps endpoints.Endpoints, sseObserver *impl.SSEObserver) http.Handler {
	m := http.NewServeMux()

	// Auth Middleware to extract user from token and put it in context
	f := interceptors.NewInterceptorFactory(svc)
	authMiddleware := f.NewHTTPAuth(f.NewJWTStrategy())

	options := []httptransport.ServerOption{
		httptransport.ServerErrorEncoder(common.EncodeError),
	}

	// SSE Endpoint
	if sseObserver != nil {
		m.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			if origin := corsOrigin(parseCORSOrigins(envvar.Get(envCORSOrigins)), r.Header.Get("Origin")); origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}

			ch := sseObserver.AddClient()
			defer sseObserver.RemoveClient(ch)

			ctx := r.Context()
			for {
				select {
				case <-ctx.Done():
					return
				case msg := <-ch:
					// A failed write means the client is gone. Carrying on would
					// spin this goroutine against a dead connection for as long
					// as events keep arriving — one leaked per disconnect.
					if _, err := fmt.Fprint(w, msg); err != nil {
						log.Debug().Err(err).Msg("An event stream client went away mid-write")
						return
					}
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
				}
			}
		})
	}

	// Connect RPC
	path, handler := connects.NewConnectHandler(eps)
	m.Handle("/api/v1"+path, http.StripPrefix("/api/v1", handler))

	// Register Handlers
	setup.RegisterHandlers(m, eps.Setup, options)
	organizations.RegisterHandlers(m, eps.Organization, options)
	projects.RegisterHandlers(m, eps.Project, options)
	definitions.RegisterHandlers(m, eps.Definition, options)
	processes.RegisterHandlers(m, eps.Process, options)
	tasks.RegisterHandlers(m, eps.Task, options)
	external_tasks.RegisterHandlers(m, eps.ExternalTask, options)
	users.RegisterHandlers(m, eps.User, options)
	group.RegisterHandlers(m, eps.Group, options)
	incidents.RegisterHandlers(m, eps.Incident, options)
	notification.RegisterHandlers(m, eps.Notification, options)
	decisions.RegisterHandlers(m, eps.Decision, options)
	connectors.RegisterHandlers(m, eps.Connector, options)
	collaboration.RegisterHandlers(m, eps.Collaboration, options)
	webhookadmin.RegisterHandlers(m, eps.Webhook, options)

	// The public delivery endpoint. Registered straight onto the mux rather than
	// through an endpoint: the signature is over the exact bytes delivered, so
	// the body must not be decoded and re-encoded on the way to the verifier.
	webhooks.RegisterRoutes(m, svc)

	// Serve UI
	distFS := ui.Dist()
	fileServer := http.FileServerFS(distFS)

	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// If it's the root, serve index.html
		if r.URL.Path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Try to see if the file exists in the embedded FS
		_, err := fs.Stat(distFS, r.URL.Path[1:])
		if err != nil {
			// Fallback to index.html for SPA routing if it's not an asset request
			if !strings.HasPrefix(r.URL.Path, "/assets/") {
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	})

	authenticatedHandler := authMiddleware.Wrap(m)
	return withCORS(authenticatedHandler)
}

// envCORSOrigins configures cross-origin access as a comma-separated list of
// allowed origins, or "*" to allow any.
const envCORSOrigins = "METIS_CORS_ORIGINS"

// withCORS applies cross-origin headers.
//
// The default is no CORS at all. In production the Go server serves both the
// UI bundle and the API from one origin, and the Vite dev server proxies /api
// to the backend, so neither case is cross-origin. The previous unconditional
// `Access-Control-Allow-Origin: *` therefore bought nothing and let any site
// on the internet call this API with a token it had obtained.
//
// Set METIS_CORS_ORIGINS when a separately hosted front end genuinely needs
// access.
func withCORS(next http.Handler) http.Handler {
	allowed := parseCORSOrigins(envvar.Get(envCORSOrigins))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := corsOrigin(allowed, r.Header.Get("Origin")); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers",
				"Content-Type, Authorization, Idempotency-Key, "+tenant.OrganizationHeader)
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func parseCORSOrigins(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	for _, o := range strings.Split(raw, ",") {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	return out
}

// corsOrigin returns the value to echo back, or "" when the request origin is
// not allowed.
func corsOrigin(allowed []string, requestOrigin string) string {
	if len(allowed) == 0 || requestOrigin == "" {
		return ""
	}
	for _, a := range allowed {
		if a == "*" {
			return "*"
		}
		if a == requestOrigin {
			// Echo the specific origin rather than the list, so caches key
			// correctly (paired with Vary: Origin above).
			return requestOrigin
		}
	}
	return ""
}
