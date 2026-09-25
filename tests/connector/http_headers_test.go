package connector_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
)

// The http-json catalogue entry declares headers as a text field — "Headers
// (JSON)", default "{}" — so the Connectors page stores what the administrator
// types as a string. The connector the application runs read headers only when
// they were already a map, and treated anything else as none. Every header
// configured through the page was dropped: an Authorization header typed into
// the form never left the server, and the call went out without it.
//
// Root cause: the catalogue and the executor disagreed about the type of the
// headers setting, and the only test of headers ran a different executor — one
// that was registered, then overwritten, and never ran in the application.
func TestHeadersTypedIntoTheConnectionFormAreSent(t *testing.T) {
	allowLoopbackEgress(t)

	for name, headers := range map[string]any{
		// What the Connectors page stores.
		"typed into the form": `{"X-Auth-Token": "secret-token"}`,
		// What an API caller or a test may pass.
		"passed as an object": map[string]any{"X-Auth-Token": "secret-token"},
	} {
		t.Run(name, func(t *testing.T) {
			var got string
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get("X-Auth-Token")
				_, _ = w.Write([]byte(`{"status": "success"}`))
			}))
			defer api.Close()

			_, err := connectors.NewHTTPConnector(nil).Execute(context.Background(),
				map[string]any{"url": api.URL, "method": "POST", "headers": headers},
				map[string]any{"message": "hello"})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != "secret-token" {
				t.Fatalf("the configured header did not reach the partner; it received %q", got)
			}
		})
	}
}

// The form's default is "{}", and a field somebody cleared is "". Neither is a
// mistake, and neither should stop the call.
func TestAnEmptyHeadersSettingSendsNoHeaders(t *testing.T) {
	allowLoopbackEgress(t)
	for _, headers := range []any{"{}", "", nil} {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		}))
		_, err := connectors.NewHTTPConnector(nil).Execute(context.Background(),
			map[string]any{"url": api.URL, "method": "POST", "headers": headers},
			map[string]any{"message": "hello"})
		api.Close()
		if err != nil {
			t.Fatalf("headers %#v: %v", headers, err)
		}
	}
}
