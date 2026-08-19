package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProfilingEnabled(t *testing.T) {
	t.Setenv(envPprofEnabled, "")
	if profilingEnabled() {
		t.Fatal("profiling should be disabled when env is empty")
	}

	t.Setenv(envPprofEnabled, "true")
	if !profilingEnabled() {
		t.Fatal("profiling should be enabled for true")
	}

	t.Setenv(envPprofEnabled, "1")
	if !profilingEnabled() {
		t.Fatal("profiling should be enabled for 1")
	}

	t.Setenv(envPprofEnabled, "invalid")
	if profilingEnabled() {
		t.Fatal("profiling should be disabled for invalid bool values")
	}
}

func TestResolvePprofAddress(t *testing.T) {
	t.Setenv(envPprofAddress, "")
	if got := resolvePprofAddress(); got != defaultPprofAddress {
		t.Fatalf("unexpected default pprof address: got %q want %q", got, defaultPprofAddress)
	}

	t.Setenv(envPprofAddress, "127.0.0.1:7070")
	if got := resolvePprofAddress(); got != "127.0.0.1:7070" {
		t.Fatalf("unexpected custom pprof address: got %q", got)
	}
}

func TestNewPprofHandler(t *testing.T) {
	handler := newPprofHandler()

	testCases := []struct {
		name   string
		path   string
		method string
	}{
		{name: "index", path: "/debug/pprof/", method: http.MethodGet},
		{name: "heap profile", path: "/debug/pprof/heap?debug=1", method: http.MethodGet},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("unexpected status code: got %d want %d", rr.Code, http.StatusOK)
			}
		})
	}
}

func TestNewHTTPServer(t *testing.T) {
	handler := http.NewServeMux()
	server := newHTTPServer("127.0.0.1:8080", handler)

	if server.Addr != "127.0.0.1:8080" {
		t.Fatalf("unexpected addr: got %q", server.Addr)
	}

	if server.Handler != handler {
		t.Fatal("expected configured handler to be used")
	}

	if server.ReadHeaderTimeout != defaultHTTPReadHeaderTimeout {
		t.Fatalf("unexpected read header timeout: got %s want %s", server.ReadHeaderTimeout, defaultHTTPReadHeaderTimeout)
	}

	if server.IdleTimeout != defaultHTTPIdleTimeout {
		t.Fatalf("unexpected idle timeout: got %s want %s", server.IdleTimeout, defaultHTTPIdleTimeout)
	}

	if server.MaxHeaderBytes != defaultHTTPMaxHeaderBytes {
		t.Fatalf("unexpected max header bytes: got %d want %d", server.MaxHeaderBytes, defaultHTTPMaxHeaderBytes)
	}
}
