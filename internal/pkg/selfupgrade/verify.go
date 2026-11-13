package selfupgrade

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// The identity a release's signature must carry. Pinned here, in the binary
// doing the verifying, and never read from anything downloaded: a signature is
// only worth what the identity it is checked against is worth.
const (
	// OIDCIssuer is GitHub Actions' token issuer.
	OIDCIssuer = "https://token.actions.githubusercontent.com"
	// WorkflowIdentityPrefix is the release workflow, run for a tag; the tag
	// itself is appended, so a signature made for one release does not pass
	// for another.
	WorkflowIdentityPrefix = "https://github.com/gsoultan/metis/.github/workflows/release.yml@refs/tags/"
)

// ErrNotVerified wraps every reason a download is refused.
var ErrNotVerified = errors.New("not verified")

// SignatureVerifier checks checksums.txt against its cosign bundle.
type SignatureVerifier struct {
	// Trusted is Sigstore's public root of trust: the Fulcio certificate
	// authorities, the Rekor transparency logs and the CT logs.
	Trusted root.TrustedMaterial

	// withoutSCTs drops the certificate transparency check, which the test
	// Sigstore cannot produce. Unexported, and false unless a test sets it.
	withoutSCTs bool
}

// VerifyChecksums checks that checksums was signed by the metis release
// workflow running for tag, using the bundle published beside it.
func (v SignatureVerifier) VerifyChecksums(checksums, bundleJSON []byte, tag string) error {
	var b bundle.Bundle
	if err := b.UnmarshalJSON(bundleJSON); err != nil {
		return fmt.Errorf("%w: the signature bundle could not be read: %v", ErrNotVerified, err)
	}
	return v.verifyEntity(&b, checksums, tag)
}

func (v SignatureVerifier) verifyEntity(entity verify.SignedEntity, checksums []byte, tag string) error {
	if v.Trusted == nil {
		return fmt.Errorf("%w: no Sigstore root of trust to check against", ErrNotVerified)
	}
	// The signature was logged in Rekor, Rekor's entry is what dates it
	// inside the certificate's ten minutes, and the certificate was logged
	// publicly when it was issued.
	options := []verify.VerifierOption{verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1)}
	if !v.withoutSCTs {
		options = append(options, verify.WithSignedCertificateTimestamps(1))
	}
	verifier, err := verify.NewVerifier(v.Trusted, options...)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotVerified, err)
	}
	identity, err := verify.NewShortCertificateIdentity(OIDCIssuer, "", WorkflowIdentityPrefix+tag, "")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotVerified, err)
	}
	if _, err := verifier.Verify(entity, verify.NewPolicy(
		verify.WithArtifact(bytes.NewReader(checksums)),
		verify.WithCertificateIdentity(identity),
	)); err != nil {
		return fmt.Errorf("%w: checksums.txt is not signed by the metis release workflow for %s: %v", ErrNotVerified, tag, err)
	}
	return nil
}

// Checksums is checksums.txt, read: file name to SHA-256.
type Checksums map[string][]byte

// ParseChecksums reads GoReleaser's checksums.txt: "<hex sha256>  <name>" a
// line. A name listed twice is refused rather than one of them believed.
func ParseChecksums(raw []byte) (Checksums, error) {
	out := Checksums{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		sum, name, ok := strings.Cut(line, "  ")
		name = strings.TrimSpace(name)
		digest, err := hex.DecodeString(sum)
		if !ok || name == "" || err != nil || len(digest) != sha256.Size {
			return nil, fmt.Errorf("%w: checksums.txt has a line that is not a SHA-256 and a name", ErrNotVerified)
		}
		if _, seen := out[name]; seen {
			return nil, fmt.Errorf("%w: checksums.txt lists %s twice", ErrNotVerified, name)
		}
		out[name] = digest
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotVerified, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: checksums.txt lists nothing", ErrNotVerified)
	}
	return out, nil
}

// Check compares content with the checksum listed for name.
func (c Checksums) Check(name string, content []byte) error {
	want, ok := c[name]
	if !ok {
		return fmt.Errorf("%w: checksums.txt does not list %s", ErrNotVerified, name)
	}
	got := sha256.Sum256(content)
	if subtle.ConstantTimeCompare(want, got[:]) != 1 {
		return fmt.Errorf("%w: %s does not match its checksum", ErrNotVerified, name)
	}
	return nil
}
