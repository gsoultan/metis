package security

import (
	"net/netip"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/rs/zerolog/log"
)

// envTrustedProxies overrides which peers may speak for somebody else.
//
// Comma-separated CIDRs, or the single word "none" to trust nobody — correct
// when the server is exposed directly and no proxy should ever be believed.
const envTrustedProxies = "METIS_TRUSTED_PROXIES"

// defaultTrustedProxies are the ranges a load balancer, ingress controller or
// sidecar actually reaches the server from.
//
// The default matters more than it looks. `X-Forwarded-For` was previously
// believed unconditionally, and it is a request header: any client could send a
// different value on every request and be given a fresh rate-limit bucket each
// time, which made the limiter decorative. Measured before this changed, one
// address got 30 requests through a limit of 3 by varying the header alone.
//
// Trusting only private space fixes that for a directly-exposed deployment
// without breaking a proxied one, because a request arriving from the public
// internet no longer gets to name its own client. A proxy that sits in public
// address space needs METIS_TRUSTED_PROXIES set explicitly.
var defaultTrustedProxies = []string{
	"127.0.0.0/8",    // loopback
	"::1/128",        // loopback
	"10.0.0.0/8",     // RFC1918
	"172.16.0.0/12",  // RFC1918
	"192.168.0.0/16", // RFC1918
	"169.254.0.0/16", // link-local
	"fe80::/10",      // link-local
	"fc00::/7",       // unique local
}

// trustedProxySet answers whether a peer is allowed to speak for someone else.
//
// It deliberately holds two lists, because "may this peer set X-Forwarded-For"
// and "is this hop infrastructure rather than a client" are different questions
// that happen to have the same answer in one common topology and opposite
// answers in another.
//
// On an internal deployment every real client is on RFC1918, so treating
// private space as infrastructure while walking the header would skip past the
// actual clients and collapse all of them into the load balancer's bucket —
// every internal user sharing one allowance, which is a self-inflicted outage
// rather than a rate limit.
type trustedProxySet struct {
	// peers may set the header. Defaults to private space: a load balancer or
	// sidecar reaches the server from there, and a request off the public
	// internet does not get to name its own client.
	peerPrefixes []netip.Prefix

	// hops are skipped when walking the header, and are populated only when an
	// operator names them. With no configuration the rightmost entry wins,
	// which is correct for the single-proxy topology almost everyone runs and
	// is correct whether the client is public or private.
	hopPrefixes []netip.Prefix
}

// loadTrustedProxies reads the configured set, falling back to private space.
//
// An unparseable entry is skipped with a warning rather than failing the boot:
// refusing to start over one malformed CIDR would take an installation down to
// protect a rate limit, and the entries that did parse still narrow trust.
func loadTrustedProxies() *trustedProxySet {
	raw := strings.TrimSpace(envvar.Get(envTrustedProxies))

	var configured []string
	switch {
	case raw == "":
		configured = defaultTrustedProxies
	case strings.EqualFold(raw, "none"):
		configured = nil
	default:
		configured = strings.Split(raw, ",")
	}

	prefixes := parsePrefixes(configured)

	set := &trustedProxySet{peerPrefixes: prefixes}
	// Only an explicit list describes a chain of proxies. The default describes
	// where a proxy connects from, which is not the same claim — see the note
	// on the type.
	if raw != "" && !strings.EqualFold(raw, "none") {
		set.hopPrefixes = prefixes
	}
	return set
}

func parsePrefixes(entries []string) []netip.Prefix {
	var prefixes []netip.Prefix
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			// A bare address is a reasonable thing to write, and means "just
			// this one".
			if addr, addrErr := netip.ParseAddr(entry); addrErr == nil {
				prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
				continue
			}
			log.Warn().Str("entry", entry).Str("setting", envTrustedProxies).
				Msg("Ignoring an unparseable trusted-proxy entry; it is neither a CIDR nor an address.")
			continue
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes
}

// allowsPeer reports whether addr may set X-Forwarded-For.
func (s *trustedProxySet) allowsPeer(addr netip.Addr) bool {
	return matches(s.peers(), addr)
}

// isKnownHop reports whether addr is our own infrastructure appearing in the
// header, and so should be walked past rather than charged.
func (s *trustedProxySet) isKnownHop(addr netip.Addr) bool {
	return matches(s.knownHops(), addr)
}

func (s *trustedProxySet) peers() []netip.Prefix {
	if s == nil {
		return nil
	}
	return s.peerPrefixes
}

func (s *trustedProxySet) knownHops() []netip.Prefix {
	if s == nil {
		return nil
	}
	return s.hopPrefixes
}

func matches(prefixes []netip.Prefix, addr netip.Addr) bool {
	// Unmap so that an IPv4-in-IPv6 peer (::ffff:10.0.0.1, which is how a
	// dual-stack listener reports an IPv4 connection) matches an IPv4 prefix.
	addr = addr.Unmap()
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// clientAddr works out who to hold responsible for a request.
//
// The rule is "rightmost untrusted": X-Forwarded-For is appended to by each
// proxy, so entries on the right were added by infrastructure and entries on
// the left may have been written by the client. Walking from the right and
// stopping at the first address that is not one of our own proxies gives the
// address the outermost trusted proxy actually saw — the one thing in the chain
// a client cannot forge.
//
// If the immediate peer is not a trusted proxy, the header is ignored
// completely. It is a claim from a stranger about who they are.
func (s *trustedProxySet) clientAddr(remoteAddr, forwardedFor string) string {
	peer, ok := hostFromRemoteAddr(remoteAddr)
	if !ok {
		peer = remoteAddr
	}

	peerAddr, err := netip.ParseAddr(peer)
	if err != nil || !s.allowsPeer(peerAddr) {
		if peer != "" {
			return peer
		}
		return "unknown"
	}

	hops := strings.Split(forwardedFor, ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if hop == "" {
			continue
		}
		addr, err := netip.ParseAddr(hop)
		if err != nil {
			// A garbage entry cannot be trusted to be one of ours, so it is
			// treated as the client — which is the conservative direction: it
			// stops the walk rather than letting it run past into
			// client-controlled territory.
			return hop
		}
		if s.isKnownHop(addr) {
			continue
		}
		return addr.String()
	}

	// Every hop was our own infrastructure, or there were none. The peer is the
	// most specific thing we actually know.
	return peer
}
