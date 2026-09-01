// Package httpclient provides the outbound HTTP client used for every call a
// process makes to a third-party system.
//
// It exists because the engine previously used http.DefaultClient, whose
// Timeout is zero. A service task pointed at an endpoint that accepts the
// connection and never responds blocked its worker goroutine indefinitely, and
// because the job pool is bounded, a handful of such calls stopped the engine
// from processing any work at all.
package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gsoultan/metis/internal/pkg/envvar"
)

const (
	defaultTimeout       = 30 * time.Second
	defaultDialTimeout   = 5 * time.Second
	defaultTLSTimeout    = 5 * time.Second
	defaultMaxRedirects  = 5
	defaultMaxIdleConns  = 100
	defaultIdleConnTimeo = 90 * time.Second

	envTimeout      = "METIS_HTTP_TIMEOUT"
	envAllowPrivate = "METIS_HTTP_ALLOW_PRIVATE_NETWORKS"
	envAllowHosts   = "METIS_HTTP_ALLOWED_HOSTS"
)

// ErrBlockedAddress is returned when a request targets an address that egress
// policy forbids.
var ErrBlockedAddress = errors.New("httpclient: destination address is blocked by egress policy")

// Timeout returns the per-request wall-clock budget for outbound calls.
// Override with METIS_HTTP_TIMEOUT (a Go duration such as "10s").
func Timeout() time.Duration {
	if raw := envvar.Get(envTimeout); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	return defaultTimeout
}

// allowPrivateNetworks reports whether requests to loopback, link-local and
// RFC1918 addresses are permitted.
//
// The default is false. Connector URLs come from process definitions, which are
// user-authored, so without this an author could point a service task at
// 169.254.169.254 and read cloud instance credentials, or sweep internal
// services the engine can reach but they cannot.
func allowPrivateNetworks() bool {
	v, err := strconv.ParseBool(envvar.Get(envAllowPrivate))
	return err == nil && v
}

// allowedHosts returns the explicit egress allowlist, if one is configured.
// METIS_HTTP_ALLOWED_HOSTS is a comma-separated list of hostnames; when set,
// only these hosts may be contacted.
func allowedHosts() map[string]struct{} {
	raw := strings.TrimSpace(envvar.Get(envAllowHosts))
	if raw == "" {
		return nil
	}
	set := make(map[string]struct{})
	for _, h := range strings.Split(raw, ",") {
		if h = strings.TrimSpace(strings.ToLower(h)); h != "" {
			set[h] = struct{}{}
		}
	}
	return set
}

var (
	sharedOnce sync.Once
	shared     *http.Client
)

// Shared returns the process-wide outbound client. Reusing one client keeps the
// connection pool shared; constructing one per call would leak connections.
func Shared() *http.Client {
	sharedOnce.Do(func() { shared = New(Timeout()) })
	return shared
}

// New builds an outbound HTTP client with an explicit timeout, bounded
// redirects and SSRF guards on every dialled address.
func New(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = Timeout()
	}

	dialer := &net.Dialer{
		Timeout:   defaultDialTimeout,
		KeepAlive: 30 * time.Second,
		// ControlContext is the only place the *resolved* address is visible.
		//
		// Checking the URL's host does not protect anything on its own: a host
		// is usually a name, net.ParseIP returns nil for it, and the IP checks
		// are skipped entirely. A definition naming a host whose A record is
		// 169.254.169.254 therefore reached the cloud metadata endpoint — and
		// definitions are untrusted input. Verified before this existed: a
		// request to a public hostname resolving to 127.0.0.1 read the body of
		// a loopback-only server.
		//
		// This runs after resolution and immediately before connect, once per
		// address attempted, so it also closes genuine DNS rebinding: there is
		// no second lookup between the check and the connection.
		ControlContext: guardedControl,
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		// The name is still checked here, because a name can be refused for
		// reasons an address cannot — an allowlist, or metadata.google.internal.
		DialContext:           guardedDialContext(dialer),
		MaxIdleConns:          defaultMaxIdleConns,
		IdleConnTimeout:       defaultIdleConnTimeo,
		TLSHandshakeTimeout:   defaultTLSTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= defaultMaxRedirects {
				return fmt.Errorf("stopped after %d redirects", defaultMaxRedirects)
			}
			return CheckURL(req.URL)
		},
	}
}

func guardedDialContext(dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if err := checkHost(host); err != nil {
			return nil, err
		}
		// An operator naming a host in the allowlist has decided it may be
		// reached even though it is private — that is usually the entire point
		// of the setting, since the alternative is no internal integrations at
		// all. The address check below cannot see the name that was allowed, so
		// the decision is carried to it.
		if hostIsExplicitlyAllowed(host) {
			ctx = context.WithValue(ctx, allowedHostKey{}, struct{}{})
		}
		return dialer.DialContext(ctx, network, addr)
	}
}

// allowedHostKey marks a dial whose hostname the operator allowlisted.
type allowedHostKey struct{}

// guardedControl refuses a connection to an address the egress policy blocks.
//
// It is given the concrete address the kernel is about to connect to, which is
// what makes it worth having: every earlier check sees a name, and a name says
// nothing about where it points.
func guardedControl(ctx context.Context, _, address string, _ syscall.RawConn) error {
	if ctx.Value(allowedHostKey{}) != nil {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: could not read the resolved address %q", ErrBlockedAddress, address)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Unreachable in practice — resolution has already happened — so
		// refusing is right: an address this cannot parse is one it cannot
		// vouch for.
		return fmt.Errorf("%w: could not parse the resolved address %q", ErrBlockedAddress, host)
	}
	return checkIP(ip)
}

// hostIsExplicitlyAllowed reports whether the operator named this host.
func hostIsExplicitlyAllowed(host string) bool {
	allow := allowedHosts()
	if allow == nil {
		return false
	}
	_, ok := allow[strings.ToLower(strings.TrimSuffix(host, "."))]
	return ok
}

// CheckURL validates a destination URL against egress policy before a request
// is made. Callers should use it to fail fast with a clear message.
func CheckURL(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("%w: missing URL", ErrBlockedAddress)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("%w: scheme %q is not permitted", ErrBlockedAddress, u.Scheme)
	}
	return checkHost(u.Hostname())
}

func checkHost(host string) error {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return fmt.Errorf("%w: empty host", ErrBlockedAddress)
	}

	if allow := allowedHosts(); allow != nil {
		if _, ok := allow[host]; !ok {
			return fmt.Errorf("%w: host %q is not in %s", ErrBlockedAddress, host, envAllowHosts)
		}
		// An explicit allowlist is a deliberate operator decision; honour it
		// even for private addresses.
		return nil
	}

	if allowPrivateNetworks() {
		return nil
	}

	// Block the metadata endpoint by name as well as by address, since it is
	// the highest-value SSRF target.
	if host == "metadata.google.internal" || host == "localhost" {
		return fmt.Errorf("%w: host %q", ErrBlockedAddress, host)
	}

	if ip := net.ParseIP(host); ip != nil {
		return checkIP(ip)
	}
	return nil
}

func checkIP(ip net.IP) error {
	if allowPrivateNetworks() {
		return nil
	}
	switch {
	case ip.IsLoopback():
		return fmt.Errorf("%w: loopback address %s", ErrBlockedAddress, ip)
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		// Covers 169.254.169.254, the cloud instance-metadata endpoint.
		return fmt.Errorf("%w: link-local address %s", ErrBlockedAddress, ip)
	case ip.IsPrivate():
		return fmt.Errorf("%w: private address %s", ErrBlockedAddress, ip)
	case ip.IsUnspecified():
		return fmt.Errorf("%w: unspecified address %s", ErrBlockedAddress, ip)
	case carrierGradeNAT.Contains(ip):
		// net.IP.IsPrivate covers the RFC1918 ranges and fc00::/7, but not
		// this one. It is not exotic: several Kubernetes CNIs and cloud
		// providers put internal service networks in 100.64/10, so leaving it
		// out means the ranges that matter most on exactly the platforms this
		// ships to are the ranges left open.
		return fmt.Errorf("%w: carrier-grade NAT address %s", ErrBlockedAddress, ip)
	}
	return nil
}

// carrierGradeNAT is RFC 6598 100.64.0.0/10.
var carrierGradeNAT = &net.IPNet{
	IP:   net.IPv4(100, 64, 0, 0),
	Mask: net.CIDRMask(10, 32),
}
