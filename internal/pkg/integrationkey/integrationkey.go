// Package integrationkey says what an organization's integration API key
// looks like, so one is told from a sign-in token at sight: by the
// interceptor that chooses which rules check a bearer token, and by a secret
// scanner.
package integrationkey

import "strings"

// Prefix starts every integration API key.
const Prefix = "mik_"

// Is reports whether token has the shape of an integration API key. It says
// nothing of whether the key is good.
func Is(token string) bool {
	return strings.HasPrefix(token, Prefix)
}
