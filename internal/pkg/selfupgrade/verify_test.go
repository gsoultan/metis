package selfupgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/testing/ca"

	"github.com/gsoultan/metis/internal/pkg/distribution"
)

func TestVerifyChecksums(t *testing.T) {
	sigstore, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatal(err)
	}
	checksums := []byte("abc  metis_0.9.0_amd64.deb\n")
	verifier := SignatureVerifier{Trusted: sigstore, WorkflowIdentityPrefix: distribution.Metis.WorkflowIdentityPrefix(), withoutSCTs: true}
	server := distribution.Distribution{Name: "metis-server", Repository: "gsoultan/metis-server"}
	metisIdentity := verifier.WorkflowIdentityPrefix

	tests := []struct {
		name     string
		identity string
		issuer   string
		signed   []byte
		tag      string
		ok       bool
	}{
		{"the release workflow for this tag", metisIdentity + "v0.9.0", OIDCIssuer, checksums, "v0.9.0", true},
		{"the release workflow for another tag", metisIdentity + "v0.8.0", OIDCIssuer, checksums, "v0.9.0", false},
		{"another workflow", "https://github.com/gsoultan/metis/.github/workflows/ci.yml@refs/tags/v0.9.0", OIDCIssuer, checksums, "v0.9.0", false},
		{"a fork", "https://github.com/someone/metis/.github/workflows/release.yml@refs/tags/v0.9.0", OIDCIssuer, checksums, "v0.9.0", false},
		{"another issuer", metisIdentity + "v0.9.0", "https://accounts.google.com", checksums, "v0.9.0", false},
		{"other bytes", metisIdentity + "v0.9.0", OIDCIssuer, []byte("tampered"), "v0.9.0", false},
		// A release of another distribution, even of the same owner, is not
		// this one's: metis-server's signature does not install over metis.
		{"another distribution", server.WorkflowIdentityPrefix() + "v0.9.0", OIDCIssuer, checksums, "v0.9.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entity, err := sigstore.Sign(tt.identity, tt.issuer, tt.signed)
			if err != nil {
				t.Fatal(err)
			}
			err = verifier.verifyEntity(entity, checksums, tt.tag)
			if tt.ok && err != nil {
				t.Fatalf("expected it verified: %v", err)
			}
			if !tt.ok && !errors.Is(err, ErrNotVerified) {
				t.Fatalf("expected ErrNotVerified, got %v", err)
			}
		})
	}
}

func TestVerifyChecksumsRefusesWithoutARoot(t *testing.T) {
	if err := (SignatureVerifier{}).VerifyChecksums([]byte("x"), []byte("{}"), "v1.0.0"); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("got %v", err)
	}
}

func TestVerifyChecksumsRefusesWithoutAnIdentity(t *testing.T) {
	sigstore, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatal(err)
	}
	verifier := SignatureVerifier{Trusted: sigstore, withoutSCTs: true}
	entity, err := sigstore.Sign("https://github.com/gsoultan/metis/.github/workflows/release.yml@refs/tags/v0.9.0", OIDCIssuer, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.verifyEntity(entity, []byte("x"), "v0.9.0"); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("got %v", err)
	}
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestChecksums(t *testing.T) {
	raw := fmt.Sprintf("%s  metis_0.9.0_amd64.deb\n%s  release.json\n", sum("deb"), sum("manifest"))
	c, err := ParseChecksums([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Check("metis_0.9.0_amd64.deb", []byte("deb")); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"metis_0.9.0_amd64.deb": "tampered",
		"metis_0.9.0_arm64.deb": "deb",
	} {
		if err := c.Check(name, []byte(content)); !errors.Is(err, ErrNotVerified) {
			t.Fatalf("Check(%s) = %v, want refused", name, err)
		}
	}
}

func TestParseChecksumsRefuses(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":        "",
		"not hex":      "zz  a\n",
		"short digest": "abcd  a\n",
		"no name":      sum("a") + "\n",
		"twice":        sum("a") + "  a\n" + sum("b") + "  a\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseChecksums([]byte(raw)); err == nil || !strings.Contains(err.Error(), "not verified") {
				t.Fatalf("got %v", err)
			}
		})
	}
}
