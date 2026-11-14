package connectors

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A mistyped Authorization header used to be dropped with a log line, and the
// call went out unauthenticated.
func TestHeadersThatAreNotJSONStopTheCall(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	var called atomic.Int32
	var authorization atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		authorization.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	testCases := []struct {
		name    string
		headers any
		wantErr bool
		want    string
	}{
		{"text that is JSON", `{"Authorization": "Bearer abc"}`, false, "Bearer abc"},
		{"an object", map[string]any{"Authorization": "Bearer abc"}, false, "Bearer abc"},
		{"nothing", "", false, ""},
		{"text that is not JSON", `{Authorization: Bearer abc}`, true, ""},
		{"a number", 42, true, ""},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			before := called.Load()
			_, err := NewHTTPConnector(nil).Execute(t.Context(),
				map[string]any{"url": server.URL, "method": "GET", "headers": tc.headers}, nil)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "headers") {
					t.Errorf("error = %v, want one about the headers", err)
				}
				if strings.Contains(errString(err), "Bearer") {
					t.Error("the error repeats the header text, which may hold a token")
				}
				if called.Load() != before {
					t.Error("the call was made without the headers")
				}
				return
			}
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got := authorization.Load(); got != tc.want {
				t.Errorf("Authorization = %v, want %q", got, tc.want)
			}
		})
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
