package httpclient

import (
	"errors"
	"net/url"
	"testing"
	"time"
)

// Connector and service-task URLs come from process definitions, which are
// user-authored. Without egress policy a definition author could point a
// service task at the cloud metadata endpoint and exfiltrate instance
// credentials, or sweep internal services the engine can reach but they cannot.
func TestCheckURL_BlocksSSRFTargets(t *testing.T) {
	t.Setenv(envAllowPrivate, "")
	t.Setenv(envAllowHosts, "")

	blocked := []struct {
		name string
		raw  string
	}{
		{"cloud metadata by IP", "http://169.254.169.254/latest/meta-data/"},
		{"gcp metadata by name", "http://metadata.google.internal/computeMetadata/v1/"},
		{"loopback IPv4", "http://127.0.0.1:8080/admin"},
		{"loopback by name", "http://localhost:8080/admin"},
		{"IPv6 loopback", "http://[::1]:8080/admin"},
		{"RFC1918 10/8", "http://10.0.0.5/internal"},
		{"RFC1918 192.168/16", "http://192.168.1.1/router"},
		{"RFC1918 172.16/12", "http://172.16.0.1/internal"},
		{"unspecified", "http://0.0.0.0/"},
		{"file scheme", "file:///etc/passwd"},
		{"gopher scheme", "gopher://evil/"},
	}

	for _, tc := range blocked {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatalf("bad test URL: %v", err)
			}
			if err := CheckURL(u); !errors.Is(err, ErrBlockedAddress) {
				t.Fatalf("CheckURL(%q) = %v, want ErrBlockedAddress", tc.raw, err)
			}
		})
	}
}

func TestCheckURL_AllowsPublicDestinations(t *testing.T) {
	t.Setenv(envAllowPrivate, "")
	t.Setenv(envAllowHosts, "")

	for _, raw := range []string{
		"https://api.example.com/v1/leads",
		"https://hooks.slack.com/services/T0/B0/XXXX",
		"http://93.184.216.34/",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("bad test URL: %v", err)
		}
		if err := CheckURL(u); err != nil {
			t.Fatalf("CheckURL(%q) = %v, want nil", raw, err)
		}
	}
}

// Operators running the engine alongside internal services need an escape
// hatch, but it must be an explicit, opt-in decision.
func TestCheckURL_PrivateNetworksAllowedWhenOptedIn(t *testing.T) {
	t.Setenv(envAllowHosts, "")
	t.Setenv(envAllowPrivate, "true")

	u, _ := url.Parse("http://10.0.0.5/internal")
	if err := CheckURL(u); err != nil {
		t.Fatalf("with %s=true, CheckURL = %v, want nil", envAllowPrivate, err)
	}
}

func TestCheckURL_AllowlistIsExclusive(t *testing.T) {
	t.Setenv(envAllowPrivate, "")
	t.Setenv(envAllowHosts, "api.example.com")

	allowed, _ := url.Parse("https://api.example.com/v1")
	if err := CheckURL(allowed); err != nil {
		t.Fatalf("allowlisted host rejected: %v", err)
	}

	denied, _ := url.Parse("https://evil.example.net/v1")
	if err := CheckURL(denied); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("non-allowlisted host: got %v, want ErrBlockedAddress", err)
	}
}

// The engine's job pool is bounded, so a call with no timeout does not just
// hang one request — it permanently consumes a worker slot.
func TestClient_AlwaysHasATimeout(t *testing.T) {
	if got := New(0).Timeout; got <= 0 {
		t.Fatalf("New(0).Timeout = %v, want a positive default", got)
	}
	if got := New(3 * time.Second).Timeout; got != 3*time.Second {
		t.Fatalf("New(3s).Timeout = %v, want 3s", got)
	}
	if got := Shared().Timeout; got <= 0 {
		t.Fatalf("Shared().Timeout = %v, want a positive default", got)
	}
}
