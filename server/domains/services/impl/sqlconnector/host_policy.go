package sqlconnector

import (
	"errors"
	"net"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/envvar"
)

// allowedHostsEnv lists the database hosts a lookup may reach, comma-separated.
const allowedHostsEnv = "METIS_SQL_LOOKUP_ALLOWED_HOSTS"

var errHostNotAllowed = errors.New("the connection's database host is not one this installation lets a lookup reach; " +
	"ask whoever runs Metis to add it to " + allowedHostsEnv)

// hostPolicy is the operator's list of database hosts a lookup may reach.
//
// Empty — the default — means any. A lookup's database is normally on a
// private network, so refusing private addresses the way outbound HTTP does
// would make the connector useless as installed. But the connection is set up
// by a project's administrator, not by whoever runs the installation, and on a
// shared installation those are different people: without this, one project
// could point a lookup at any database the server can reach. An operator who
// needs that closed lists the hosts that are allowed.
type hostPolicy struct {
	allowed map[string]struct{}
}

func resolveHostPolicy() hostPolicy {
	return hostPolicyOf(envvar.Get(allowedHostsEnv))
}

func hostPolicyOf(list string) hostPolicy {
	policy := hostPolicy{allowed: map[string]struct{}{}}
	for _, host := range strings.Split(list, ",") {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			policy.allowed[host] = struct{}{}
		}
	}
	return policy
}

// permit refuses a connection that can reach a host the operator has not
// listed. A socket path is compared as written.
func (p hostPolicy) permit(hosts ...string) error {
	if len(p.allowed) == 0 {
		return nil
	}
	for _, host := range hosts {
		if _, listed := p.allowed[strings.ToLower(strings.TrimSpace(host))]; !listed {
			return errHostNotAllowed
		}
	}
	return nil
}

// hostOf takes the host out of host:port, and leaves a socket path alone.
func hostOf(address string) string {
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
}
