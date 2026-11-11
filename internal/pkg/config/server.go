package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// The variables the server section stands in for. The environment still wins
// wherever it sets one: a deployment's own configuration is the last word.
const (
	EnvHTTPAddress = "METIS_HTTP_ADDRESS"
	EnvPublicURL   = "METIS_PUBLIC_URL"
)

// ServerConfig is the part of config.yaml that says where this server is.
//
// It is here, in a file only the operator can write, rather than among the
// settings an administrator saves from the UI: a listen address saved wrong
// from the UI would leave the UI unreachable, with no way back through it.
// The setup wizard writes it because the wizard is the operator's — it opens
// only with the setup token printed in the server's log.
type ServerConfig struct {
	// HTTPAddress is the listen address of the API and the UI, such as
	// ":8080" (every interface) or "10.0.0.5:8080" (one).
	HTTPAddress string `yaml:"http_address,omitempty" json:"http_address,omitzero"`
	// PublicURL is the address people reach this server at, which links in
	// e-mails are written against.
	PublicURL string `yaml:"public_url,omitempty" json:"public_url,omitzero"`
}

// Values returns the server settings this file sets, keyed by the variable
// each stands in for. Empty fields are left out, so the server falls back to
// its default for them.
func (s ServerConfig) Values() map[string]string {
	values := map[string]string{}
	if v := strings.TrimSpace(s.HTTPAddress); v != "" {
		values[EnvHTTPAddress] = v
	}
	if v := strings.TrimSpace(s.PublicURL); v != "" {
		values[EnvPublicURL] = v
	}
	return values
}

// ValidateListenAddress checks a listen address the way the server will use
// it: a host (or nothing, for every interface) and a port from 1 to 65535.
//
// Checked before it is written, because a value the server cannot bind is a
// server that does not come back after the restart that applies it.
func ValidateListenAddress(address string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("%q is not a listen address; use host:port, such as :8080 or 0.0.0.0:8080", address)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("the port in %q must be a number from 1 to 65535", address)
	}
	if strings.ContainsAny(host, " /") {
		return fmt.Errorf("the host in %q is not a host name or an IP address", address)
	}
	return nil
}
