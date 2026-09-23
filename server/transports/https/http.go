package https

import (
	"fmt"
	"net/http"

	"github.com/gsoultan/metis/internal/pkg/envvar"

	"github.com/rs/zerolog/log"

	"strings"

	"github.com/gsoultan/metis/server/interceptors/tenant"

	"github.com/gsoultan/metis/server/domains/entities"
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
	"github.com/gsoultan/metis/server/transports/https/environments"
	"github.com/gsoultan/metis/server/transports/https/external_tasks"
	"github.com/gsoultan/metis/server/transports/https/group"
	"github.com/gsoultan/metis/server/transports/https/incidents"
	"github.com/gsoultan/metis/server/transports/https/notification"
	"github.com/gsoultan/metis/server/transports/https/organizations"
	"github.com/gsoultan/metis/server/transports/https/participants"
	"github.com/gsoultan/metis/server/transports/https/participantsources"
	"github.com/gsoultan/metis/server/transports/https/platformusers"
	"github.com/gsoultan/metis/server/transports/https/processes"
	"github.com/gsoultan/metis/server/transports/https/projects"
	"github.com/gsoultan/metis/server/transports/https/setup"
	"github.com/gsoultan/metis/server/transports/https/simulations"
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

			// The scope is taken from the request, never from what the client
			// asked for: the organization comes from the token the auth
			// middleware validated, and the environment from the listener this
			// connection arrived on. A client that could name its own audience
			// would name somebody else's.
			// The tenant is resolved here rather than read off the context:
			// the endpoint chain that normally resolves it does not run for
			// this handler, which is mounted straight on the mux so the
			// connection can stay open.
			streamCtx := r.Context()
			if tc, ok := tenant.ResolveFromContext(streamCtx); ok {
				streamCtx = entities.WithTenantContext(streamCtx, tc)
			}
			ch := sseObserver.AddClient(entities.SSEScopeFrom(streamCtx))
			defer sseObserver.RemoveClient(ch)

			// Send the headers now rather than with the first event.
			//
			// Without this the response is buffered until something happens, so
			// EventSource stays in CONNECTING on a quiet installation: the
			// browser cannot tell a working stream from a broken one, onopen
			// never fires, and any proxy with a header-read timeout closes the
			// connection before the first event ever arrives.
			flusher, canFlush := w.(http.Flusher)
			if canFlush {
				w.WriteHeader(http.StatusOK)
				flusher.Flush()
			}

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
					if canFlush {
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
	environments.RegisterHandlers(m, eps.Environment, options)
	participants.RegisterHandlers(m, eps.Participant, options)
	participantsources.RegisterHandlers(m, eps.ParticipantSource, options)
	platformusers.RegisterHandlers(m, eps.PlatformUser, options)
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
	simulations.RegisterHandlers(m, eps.Simulation, options)

	// The public delivery endpoint. Registered straight onto the mux rather than
	// through an endpoint: the signature is over the exact bytes delivered, so
	// the body must not be decoded and re-encoded on the way to the verifier.
	webhooks.RegisterRoutes(m, svc)

	// Serve UI. The static handler compresses the embedded assets once and
	// serves them with cache headers a bare file server never set; see static.go.
	static, err := newStaticHandler(ui.Dist())
	if err != nil {
		// The dist is embedded at build time, so this can only fail if the build
		// shipped a corrupt bundle — fail loudly rather than serve a blank app.
		log.Fatal().Err(err).Msg("Could not prepare the embedded UI for serving")
	}
	m.Handle("/", static)

	authenticatedHandler := authMiddleware.Wrap(m)
	return securityHeaders(withCORS(authenticatedHandler))
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
