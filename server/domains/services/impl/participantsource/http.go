package participantsource

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/httpclient"
	"github.com/gsoultan/metis/server/domains/logic/userimport"
	"github.com/rs/zerolog/log"
)

// HTTPConfig describes a directory endpoint.
type HTTPConfig struct {
	URL string
	// Method defaults to GET. A directory that only answers POST is unusual but
	// not rare, and the alternative is telling somebody to change their API.
	Method string
	// Headers carry whatever authenticates the call — a bearer token, an API
	// key. Held encrypted at rest and never returned to a browser.
	Headers map[string]string
}

// HTTPSource reads a directory from a JSON endpoint.
type HTTPSource struct{ Config HTTPConfig }

func NewHTTPSource(config HTTPConfig) *HTTPSource { return &HTTPSource{Config: config} }

// Fetch calls the endpoint and reads what it returned.
//
// Through the shared guarded client, which is the point. The URL is
// operator-supplied, so without the guard this would be a request the server
// makes to any address the operator names — including its own metadata service
// and anything else on the private network it happens to sit in. The guard runs
// on the *resolved* address, because a hostname that resolves to a private
// address is the way past a check that only reads the URL.
func (s *HTTPSource) Fetch(ctx context.Context) (userimport.Result, error) {
	target, err := url.Parse(strings.TrimSpace(s.Config.URL))
	if err != nil {
		return userimport.Result{}, fmt.Errorf("the endpoint address is not a URL: %w", err)
	}
	if err := httpclient.CheckURL(target); err != nil {
		return userimport.Result{}, fmt.Errorf("the endpoint address is not allowed: %w", err)
	}

	method := strings.ToUpper(strings.TrimSpace(s.Config.Method))
	if method == "" {
		method = http.MethodGet
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
	if err != nil {
		return userimport.Result{}, fmt.Errorf("could not build the request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	for name, value := range s.Config.Headers {
		request.Header.Set(name, value)
	}

	response, err := httpclient.Shared().Do(request)
	if err != nil {
		return userimport.Result{}, fmt.Errorf("the endpoint could not be reached: %w", err)
	}
	defer func() {
		// Logged rather than discarded: a body that will not close is a
		// connection this process keeps, and a sync that runs on a schedule
		// would leak one every time.
		if err := response.Body.Close(); err != nil {
			log.Warn().Err(err).Msg("Could not close a participant directory response")
		}
	}()

	// Bounded: the body is whatever a third party chose to send, and reading it
	// unbounded is how one directory with a runaway export exhausts the server.
	body, err := httpclient.ReadResponseBody(response.Body)
	if err != nil {
		return userimport.Result{}, fmt.Errorf("could not read the response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		// The status, not the body: an error page from somebody else's API is
		// not something to quote back into our own logs and UI.
		return userimport.Result{}, fmt.Errorf("the endpoint answered %s", response.Status)
	}

	result, err := userimport.FromJSON(body)
	if err != nil {
		return userimport.Result{}, err
	}
	return result, nil
}
