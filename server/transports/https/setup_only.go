package https

import (
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/rs/zerolog/log"

	setupendpoints "github.com/gsoultan/metis/server/endpoints/setup"
	"github.com/gsoultan/metis/server/transports/https/common"
	"github.com/gsoultan/metis/server/transports/https/setup"
	"github.com/gsoultan/metis/ui"
)

// NewSetupOnlyHandler serves the first-run wizard and nothing else: the UI
// bundle, so the wizard's page loads, and the three setup routes. Every other
// API route answers 503, because there is no database behind it yet.
//
// It is what a server with no database serves, so that the database can be
// chosen in the wizard rather than in a file on the server. The routes it
// keeps are the ones that are already anonymous on a set-up server, and they
// open only with the setup token printed in the server's log.
func NewSetupOnlyHandler(eps setupendpoints.Endpoints) http.Handler {
	m := http.NewServeMux()
	options := []httptransport.ServerOption{
		httptransport.ServerErrorEncoder(common.EncodeError),
	}
	setup.RegisterHandlers(m, eps, options)

	m.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := w.Write([]byte(`{"err":"Metis is not set up yet. Open /setup to choose its database."}` + "\n")); err != nil {
			log.Debug().Err(err).Msg("Could not write the not-set-up response")
		}
	})

	static, err := newStaticHandler(ui.Dist())
	if err != nil {
		// As in NewHTTPHandler: the bundle is embedded at build time, so this
		// is a corrupt build, and a blank wizard would hide it.
		log.Fatal().Err(err).Msg("Could not prepare the embedded UI for serving")
	}
	m.Handle("/", static)

	return securityHeaders(limitSetupBody(m))
}

// maxSetupBody bounds a request to the wizard. The largest it takes is the
// setup form, a few hundred bytes; the server's usual body limits are not in
// front of this handler.
const maxSetupBody = 64 << 10

func limitSetupBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxSetupBody)
		next.ServeHTTP(w, r)
	})
}
