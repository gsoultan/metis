package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The egress guard has to run on the address, not the name.
//
// Before this, the IP checks only executed when a URL literally contained an IP:
// for a hostname, net.ParseIP returned nil and they were skipped entirely. A
// process definition — untrusted input — naming a host whose A record pointed at
// 169.254.169.254 therefore reached the cloud metadata endpoint, and one
// pointing at 127.0.0.1 reached anything bound to loopback.
//
// This runs after resolution, which is also what makes DNS rebinding
// uninteresting: there is no second lookup between the check and the connect.
func TestGuardedControlRefusesResolvedPrivateAddresses(t *testing.T) {
	blocked := []struct {
		name    string
		address string
	}{
		{"loopback", "127.0.0.1:80"},
		{"loopback IPv6", "[::1]:80"},
		{"the cloud metadata endpoint", "169.254.169.254:80"},
		{"private 10/8", "10.0.0.1:80"},
		{"private 172.16/12", "172.16.0.1:80"},
		{"private 192.168/16", "192.168.1.1:80"},
		{"unique local IPv6", "[fc00::1]:80"},
		{"link-local IPv6", "[fe80::1]:80"},
		{"unspecified", "0.0.0.0:80"},
		// RFC 6598. net.IP.IsPrivate does not cover it, and several Kubernetes
		// CNIs put internal service networks here.
		{"carrier-grade NAT", "100.64.0.1:80"},
		// A dual-stack listener reports an IPv4 peer this way, and it is the
		// obvious thing to try once the plain form is refused.
		{"IPv4-mapped loopback", "[::ffff:127.0.0.1]:80"},
	}

	for _, target := range blocked {
		t.Run(target.name, func(t *testing.T) {
			err := guardedControl(t.Context(), "tcp", target.address, nil)
			if err == nil {
				t.Fatalf("%s was allowed; a definition naming a host that resolves here would reach it", target.address)
			}
			if !errors.Is(err, ErrBlockedAddress) {
				t.Errorf("refused %s with %v, want ErrBlockedAddress", target.address, err)
			}
		})
	}

	// The guard must not block the ordinary case it exists to permit.
	if err := guardedControl(t.Context(), "tcp", "93.184.216.34:443", nil); err != nil {
		t.Errorf("a public address was refused: %v", err)
	}
}

// An operator who names a host in the allowlist has decided it may be reached
// even though it is private — internal integrations are the usual reason the
// setting exists at all. The address check cannot see the name that was
// allowed, so the decision has to travel with the dial.
func TestAnAllowlistedHostReachesItsPrivateAddress(t *testing.T) {
	t.Setenv(envAllowHosts, "internal.example.com")

	ctx := context.WithValue(t.Context(), allowedHostKey{}, struct{}{})
	if err := guardedControl(ctx, "tcp", "10.0.0.5:8080", nil); err != nil {
		t.Fatalf("an allowlisted host was refused at its private address: %v", err)
	}

	// Without the marker the same address is refused, so the exemption is
	// carried by the allowlist decision rather than granted to everyone.
	if err := guardedControl(t.Context(), "tcp", "10.0.0.5:8080", nil); err == nil {
		t.Fatal("a private address was allowed without an allowlisted host")
	}
}

// The end-to-end version of the same bug, against a server that only loopback
// can reach. Needs public DNS, so it skips rather than fails when there is none.
func TestAHostnameResolvingToLoopbackCannotReachIt(t *testing.T) {
	if _, err := net.DefaultResolver.LookupHost(t.Context(), loopbackHostname); err != nil {
		t.Skipf("%s does not resolve here, so this cannot be exercised: %v", loopbackHostname, err)
	}

	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "internal-only")
	}))
	defer internal.Close()

	_, port, err := net.SplitHostPort(strings.TrimPrefix(internal.URL, "http://"))
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	target := fmt.Sprintf("http://%s:%s/", loopbackHostname, port)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	// CheckURL is a fail-fast courtesy on the name and deliberately still
	// allows this — it cannot know where the name points. Pinned so that its
	// permissiveness is never read as the guard having approved the request.
	if err := CheckURL(req.URL); err != nil {
		t.Fatalf("CheckURL refused a name it cannot resolve: %v", err)
	}

	resp, err := New(5 * time.Second).Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("a hostname resolving to loopback reached a loopback-only server")
	}
	if !strings.Contains(err.Error(), "egress policy") {
		t.Errorf("blocked, but not by the egress policy: %v", err)
	}
}

// loopbackHostname is a public DNS name whose address record is 127.0.0.1. It
// is how this proves the bug without editing /etc/hosts, which a test may not.
const loopbackHostname = "localtest.me"
