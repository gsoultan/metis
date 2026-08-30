package connector_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
)

// Contract tests: what a connector puts on the wire, and how it reads what
// comes back.
//
// This is the tier the roadmap names as missing, and it is a different question
// from the integration tests beside it. Those prove a connector can be
// installed, configured and called. These pin the *contract* — the promises a
// process author relies on when they wire a service task to a partner:
//
//   - a GET does not carry a body, and a POST carries exactly the payload;
//   - a 2xx that is not JSON is a result, not a failure;
//   - a 4xx is an error that names its status, so an incident says what happened;
//   - a manifest's own error rules decide before its success condition does,
//     because plenty of APIs report failure with a 200.
//
// Each is something a partner's API would notice if it changed, which is what
// makes it a contract rather than an implementation detail.

// partner is a stub of somebody else's API. It records what arrived, so a test
// can assert on the request as well as the response.
type partner struct {
	server *httptest.Server

	method      string
	path        string
	body        string
	contentType string
	headers     http.Header
}

func newPartner(t *testing.T, respond func(w http.ResponseWriter)) *partner {
	t.Helper()
	// The connectors that use the shared guarded client refuse loopback by
	// default — that is the SSRF policy, and TestTheEgressPolicyIsEnforced
	// below is the contract test for it. A stub partner necessarily lives on
	// loopback, so these tests opt in explicitly rather than the policy being
	// weakened for them.
	t.Setenv("GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	p := &partner{}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		p.method, p.path, p.body = r.Method, r.URL.Path, string(body)
		p.contentType = r.Header.Get("Content-Type")
		p.headers = r.Header.Clone()
		respond(w)
	}))
	t.Cleanup(p.server.Close)
	return p
}

func jsonResponse(status int, body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// ---- the built-in HTTP connector ---------------------------------------

func TestHTTPConnectorSendsWhatItPromises(t *testing.T) {
	testCases := []struct {
		name        string
		method      string
		payload     map[string]any
		wantMethod  string
		wantBody    string
		wantContent string
	}{
		{
			name: "a GET carries no body", method: "", payload: map[string]any{"ignored": 1},
			wantMethod: http.MethodGet, wantBody: "",
			// A GET with a JSON body is rejected outright by some servers and
			// silently ignored by others, so it must not be sent at all.
		},
		{
			name: "a POST carries the payload as JSON", method: http.MethodPost,
			payload:    map[string]any{"last_name": "Ada"},
			wantMethod: http.MethodPost, wantBody: `{"last_name":"Ada"}`, wantContent: "application/json",
		},
		{
			name: "a POST with no payload carries no body", method: http.MethodPost, payload: nil,
			wantMethod: http.MethodPost, wantBody: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPartner(t, jsonResponse(http.StatusOK, `{"id":"1"}`))
			config := map[string]any{"url": p.server.URL}
			if tc.method != "" {
				config["method"] = tc.method
			}

			if _, err := connectors.NewHTTPConnector(p.server.Client()).
				Execute(t.Context(), config, tc.payload); err != nil {
				t.Fatalf("execute: %v", err)
			}

			if p.method != tc.wantMethod {
				t.Errorf("method = %q, want %q", p.method, tc.wantMethod)
			}
			if strings.TrimSpace(p.body) != tc.wantBody {
				t.Errorf("body = %q, want %q", strings.TrimSpace(p.body), tc.wantBody)
			}
			if tc.wantContent != "" && !strings.HasPrefix(p.contentType, tc.wantContent) {
				t.Errorf("content-type = %q, want %q", p.contentType, tc.wantContent)
			}
		})
	}
}

func TestHTTPConnectorReadsWhatComesBack(t *testing.T) {
	testCases := []struct {
		name       string
		status     int
		body       string
		contentGen func(http.ResponseWriter)
		wantErr    string
		wantOutput map[string]any
		why        string
	}{
		{
			name: "a JSON object becomes output variables", status: http.StatusOK, body: `{"id":"lead-1"}`,
			wantOutput: map[string]any{"id": "lead-1", "status_code": 200},
			why:        "the fields are what a process reads on the next step",
		},
		{
			name: "an empty body still reports its status", status: http.StatusNoContent, body: "",
			wantOutput: map[string]any{"status_code": 204},
			why:        "a 204 is a success with nothing to say, not a failure",
		},
		{
			name: "a 4xx is an error naming the status", status: http.StatusBadRequest, body: `{"error":"bad"}`,
			wantErr: "400",
			why:     "an incident has to say what the partner actually answered",
		},
		{
			name: "a 5xx is an error naming the status", status: http.StatusBadGateway, body: "upstream down",
			wantErr: "502",
			why:     "same, and this is the one that gets retried",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPartner(t, jsonResponse(tc.status, tc.body))
			out, err := connectors.NewHTTPConnector(p.server.Client()).
				Execute(t.Context(), map[string]any{"url": p.server.URL}, nil)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("got no error, want one naming %s — %s", tc.wantErr, tc.why)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not name the status %s — %s", err, tc.wantErr, tc.why)
				}
				return
			}
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			for key, want := range tc.wantOutput {
				if got := out[key]; got != want {
					t.Errorf("output[%q] = %v (%T), want %v (%T) — %s", key, got, got, want, want, tc.why)
				}
			}
		})
	}
}

// A 200 that is not JSON is a result, not a failure. XML and plain text are
// common, and treating them as errors would strand a process on a call that
// actually succeeded.
func TestHTTPConnectorTreatsANonJSONSuccessAsAResult(t *testing.T) {
	p := newPartner(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "OK, created")
	})

	out, err := connectors.NewHTTPConnector(p.server.Client()).
		Execute(t.Context(), map[string]any{"url": p.server.URL}, nil)
	if err != nil {
		t.Fatalf("a non-JSON 200 was treated as a failure: %v", err)
	}
	if out["body"] != "OK, created" {
		t.Errorf("body = %v, want the text handed on so a process can read it", out["body"])
	}
	if out["status_code"] != 200 {
		t.Errorf("status_code = %v, want 200", out["status_code"])
	}
}

func TestHTTPConnectorRefusesAnIncompleteConfiguration(t *testing.T) {
	_, err := connectors.NewHTTPConnector(nil).Execute(t.Context(), map[string]any{}, nil)
	if err == nil {
		t.Fatal("a connector with no url reported success")
	}
	if !strings.Contains(err.Error(), "url") {
		t.Errorf("error %q does not name the missing key", err)
	}
}

// ---- the built-in Slack connector --------------------------------------

func TestSlackConnectorContract(t *testing.T) {
	t.Run("posts the text to the configured webhook", func(t *testing.T) {
		p := newPartner(t, jsonResponse(http.StatusOK, "ok"))

		out, err := connectors.NewSlackConnector().Execute(t.Context(),
			map[string]any{"webhook_url": p.server.URL}, map[string]any{"text": "deploy finished"})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}

		var sent map[string]any
		if err := json.Unmarshal([]byte(p.body), &sent); err != nil {
			t.Fatalf("Slack was not sent JSON: %q", p.body)
		}
		if sent["text"] != "deploy finished" {
			t.Errorf("sent %v, want the payload's text", sent["text"])
		}
		if out["ok"] != true {
			t.Errorf("output = %v, want ok:true", out)
		}
	})

	t.Run("a non-200 is a failure", func(t *testing.T) {
		p := newPartner(t, jsonResponse(http.StatusForbidden, "invalid_token"))
		_, err := connectors.NewSlackConnector().Execute(t.Context(),
			map[string]any{"webhook_url": p.server.URL}, map[string]any{"text": "hello"})
		if err == nil {
			t.Fatal("a rejected webhook reported success")
		}
	})

	t.Run("refuses an incomplete configuration", func(t *testing.T) {
		if _, err := connectors.NewSlackConnector().Execute(t.Context(),
			map[string]any{}, map[string]any{"text": "hello"}); err == nil {
			t.Error("no webhook_url reported success")
		}
		p := newPartner(t, jsonResponse(http.StatusOK, "ok"))
		if _, err := connectors.NewSlackConnector().Execute(t.Context(),
			map[string]any{"webhook_url": p.server.URL}, map[string]any{}); err == nil {
			t.Error("no text reported success")
		}
	})
}

// ---- manifests ----------------------------------------------------------

// manifestFor builds a manifest whose contract mirrors the one documented in
// docs/integration.md, so this test breaks if that example stops working.
func manifestFor(url string) connectors.Manifest {
	return connectors.Manifest{
		Key:     "partner.create-lead",
		Version: 1,
		Name:    "Partner — Create Lead",
		Request: connectors.Request{
			Method: http.MethodPost,
			URL:    url,
			Body:   map[string]any{"LastName": "{{input.last_name}}", "Amount": "{{input.amount}}"},
		},
		Response: connectors.Response{
			Success: "status >= 200 and status < 300",
			Outputs: map[string]string{"lead_id": "body.id"},
		},
		Errors: []connectors.ErrorRule{
			{When: "status = 429", BPMNError: "RATE_LIMITED", Retryable: true, RetryAfter: "headers['Retry-After']"},
			{When: "status = 401", BPMNError: "AUTH_FAILED", Retryable: false},
			{When: "body.status = \"rejected\"", BPMNError: "REJECTED", Retryable: false},
		},
	}
}

func TestManifestContract(t *testing.T) {
	t.Run("a template that is one expression keeps its type", func(t *testing.T) {
		// A number arriving as the string "500" is rejected by most APIs that
		// care, and this is the promise docs/integration.md makes.
		p := newPartner(t, jsonResponse(http.StatusOK, `{"id":"lead-9"}`))
		_, err := connectors.RunManifest(t.Context(), manifestFor(p.server.URL), nil,
			map[string]any{"last_name": "Ada", "amount": 500}, p.server.Client())
		if err != nil {
			t.Fatalf("run: %v", err)
		}

		var sent map[string]any
		if err := json.Unmarshal([]byte(p.body), &sent); err != nil {
			t.Fatalf("body was not JSON: %q", p.body)
		}
		if _, isString := sent["Amount"].(string); isString {
			t.Errorf("Amount was sent as the text %q, not a number", sent["Amount"])
		}
		if sent["LastName"] != "Ada" {
			t.Errorf("LastName = %v, want Ada", sent["LastName"])
		}
	})

	t.Run("outputs are mapped out of the response", func(t *testing.T) {
		p := newPartner(t, jsonResponse(http.StatusOK, `{"id":"lead-9","noise":"ignored"}`))
		out, err := connectors.RunManifest(t.Context(), manifestFor(p.server.URL), nil,
			map[string]any{"last_name": "Ada", "amount": 1}, p.server.Client())
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if out["lead_id"] != "lead-9" {
			t.Errorf("lead_id = %v, want lead-9", out["lead_id"])
		}
		if _, leaked := out["noise"]; leaked {
			t.Error("a field the manifest did not map reached the process")
		}
	})

	t.Run("an error rule decides before the success condition", func(t *testing.T) {
		// Plenty of APIs report failure with a 200. The manifest says so, and
		// the runner has to believe the manifest over the status line.
		p := newPartner(t, jsonResponse(http.StatusOK, `{"status":"rejected"}`))
		_, err := connectors.RunManifest(t.Context(), manifestFor(p.server.URL), nil,
			map[string]any{"last_name": "Ada", "amount": 1}, p.server.Client())
		if err == nil {
			t.Fatal("a 200 the manifest calls a rejection was reported as success")
		}
		var manifestErr *connectors.ManifestError
		if !errors.As(err, &manifestErr) || manifestErr.BPMNErrorCode() != "REJECTED" {
			t.Fatalf("error = %v, want a ManifestError carrying REJECTED", err)
		}
	})

	t.Run("a rule carries its BPMN code and retryability", func(t *testing.T) {
		testCases := []struct {
			name          string
			status        int
			retryAfter    string
			wantCode      string
			wantRetryable bool
			wantWait      time.Duration
		}{
			{"429 is retryable and honours Retry-After", http.StatusTooManyRequests, "30", "RATE_LIMITED", true, 30 * time.Second},
			{"401 is not retryable", http.StatusUnauthorized, "", "AUTH_FAILED", false, 0},
		}
		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				p := newPartner(t, func(w http.ResponseWriter) {
					if tc.retryAfter != "" {
						w.Header().Set("Retry-After", tc.retryAfter)
					}
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, `{}`)
				})

				_, err := connectors.RunManifest(t.Context(), manifestFor(p.server.URL), nil,
					map[string]any{"last_name": "Ada", "amount": 1}, p.server.Client())
				var manifestErr *connectors.ManifestError
				if !errors.As(err, &manifestErr) {
					t.Fatalf("error = %v, want a ManifestError", err)
				}
				if manifestErr.BPMNErrorCode() != tc.wantCode {
					t.Errorf("code = %q, want %q — this is what a boundary event catches", manifestErr.BPMNErrorCode(), tc.wantCode)
				}
				if manifestErr.Retryable != tc.wantRetryable {
					t.Errorf("retryable = %v, want %v — retrying a 401 spends the instance's attempts on the same answer",
						manifestErr.Retryable, tc.wantRetryable)
				}
				if tc.wantWait != 0 && manifestErr.RetryAfter != tc.wantWait {
					t.Errorf("retry after = %v, want %v — honouring what a partner asks is the difference between backing off and being blocked",
						manifestErr.RetryAfter, tc.wantWait)
				}
			})
		}
	})

	t.Run("an unmatched failure is still a failure", func(t *testing.T) {
		// A status no rule names and the success condition rejects must not be
		// reported as success just because nothing described it.
		p := newPartner(t, jsonResponse(http.StatusInternalServerError, `{}`))
		if _, err := connectors.RunManifest(t.Context(), manifestFor(p.server.URL), nil,
			map[string]any{"last_name": "Ada", "amount": 1}, p.server.Client()); err == nil {
			t.Fatal("a 500 no rule mentions was reported as success")
		}
	})
}

// TestTheEgressPolicyIsEnforced is the contract that matters most and is
// easiest to lose: a connector's URL can come from a manifest an administrator
// installed, and a process variable can reach a request template. Without an
// egress policy that is a request forger pointed at the cloud metadata service
// or anything else on the private network.
//
// Deliberately *not* opting in, unlike every test above.
func TestTheEgressPolicyIsEnforced(t *testing.T) {
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(p.Close)

	_, err := connectors.NewSlackConnector().Execute(t.Context(),
		map[string]any{"webhook_url": p.URL}, map[string]any{"text": "hello"})
	if err == nil {
		t.Fatal("a connector reached a loopback address with the egress policy at its default")
	}
	if !strings.Contains(err.Error(), "blocked by egress policy") {
		t.Fatalf("error %q does not identify the egress policy, so an operator cannot tell this from a network fault", err)
	}
}
