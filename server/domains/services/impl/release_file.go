package impl

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/domains/entities"
)

// The settings a release file is signed and trusted by. A release file is how
// a release reaches another installation, so whether it is believed rests on
// a key, not on who uploads it.
const (
	// EnvReleaseSigningKey is the Ed25519 seed this installation signs the
	// releases it exports with: 32 bytes, base64.
	EnvReleaseSigningKey = "METIS_RELEASE_SIGNING_KEY"
	// EnvReleaseTrustedKeys are the Ed25519 public keys, base64 and comma
	// separated, of the installations whose releases this one imports.
	EnvReleaseTrustedKeys = "METIS_RELEASE_TRUSTED_KEYS"
)

// ReleaseFileFormat names the shape of a release file.
const ReleaseFileFormat = "metis.release-file/v1"

// MaxReleaseFileBytes bounds a release file.
const MaxReleaseFileBytes = 16 << 20

// releaseFile is a release as it travels between installations: the bundle,
// its hash, and a signature over the hash by the key named.
type releaseFile struct {
	Format    string                 `json:"format"`
	Hash      string                 `json:"hash"`
	Key       string                 `json:"key"`
	Signature string                 `json:"signature"`
	Release   entities.ReleaseBundle `json:"release"`
}

// ExportRelease is a release cut here as a signed file another installation
// can import.
func (s *releaseService) ExportRelease(ctx context.Context, projectID uuid.UUID, name string) ([]byte, error) {
	private, err := releaseSigningKey()
	if err != nil {
		return nil, err
	}
	rel, err := s.repo.Release().GetByName(ctx, projectID, strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	hash, err := entities.HashReleaseBundle(rel.Bundle)
	if err != nil {
		return nil, err
	}
	if hash != rel.Hash {
		return nil, apierr.Invalidf("release %s has changed since it was cut, so it is not exported; cut a new one", rel.Name)
	}
	digest, _ := hex.DecodeString(hash)
	file := releaseFile{
		Format:    ReleaseFileFormat,
		Hash:      hash,
		Key:       base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey)),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, digest)),
		Release:   rel.Bundle,
	}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("could not write the release file: %w", err)
	}
	return raw, nil
}

// ImportReleaseFile imports a release file exported by a trusted
// installation, staged, as ImportReleaseFrom does.
func (s *releaseService) ImportReleaseFile(ctx context.Context, projectID uuid.UUID, raw []byte) (entities.Release, error) {
	if len(raw) > MaxReleaseFileBytes {
		return entities.Release{}, apierr.Invalidf("a release file is at most %d MiB", MaxReleaseFileBytes>>20)
	}
	var file releaseFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return entities.Release{}, apierr.Invalidf("this is not a release file: %v", err)
	}
	if file.Format != ReleaseFileFormat {
		return entities.Release{}, apierr.Invalidf("this release file is %q, which this server does not read", file.Format)
	}
	if err := verifyReleaseFile(file); err != nil {
		return entities.Release{}, err
	}
	return s.importBundle(ctx, projectID, file.Release, file.Hash)
}

// verifyReleaseFile refuses a file whose content is not what its hash says,
// whose key this installation does not trust, or whose signature that key
// did not make.
func verifyReleaseFile(file releaseFile) error {
	hash, err := entities.HashReleaseBundle(file.Release)
	if err != nil {
		return err
	}
	if hash != file.Hash {
		return apierr.Invalidf("this release file's content is not what it was signed as; it has been changed")
	}
	trusted, err := releaseTrustedKeys()
	if err != nil {
		return err
	}
	if len(trusted) == 0 {
		return apierr.Invalidf("this installation trusts no release keys; set %s to the public key of the installation the release comes from", EnvReleaseTrustedKeys)
	}
	if !slices.Contains(trusted, file.Key) {
		return apierr.Invalidf("this release file is signed by a key this installation does not trust (%s)", file.Key)
	}
	public, _ := base64.StdEncoding.DecodeString(file.Key)
	signature, err := base64.StdEncoding.DecodeString(file.Signature)
	digest, _ := hex.DecodeString(file.Hash)
	if err != nil || len(public) != ed25519.PublicKeySize || !ed25519.Verify(public, digest, signature) {
		return apierr.Invalidf("this release file's signature is not valid")
	}
	return nil
}

// releaseSigningKey reads this installation's release signing key.
func releaseSigningKey() (ed25519.PrivateKey, error) {
	raw, set := envvar.FromEnvironment(EnvReleaseSigningKey)
	if !set || strings.TrimSpace(raw) == "" {
		return nil, apierr.Invalidf("this installation has no release signing key; set %s to export releases", EnvReleaseSigningKey)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s is not a base64 Ed25519 seed of %d bytes", EnvReleaseSigningKey, ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// releaseTrustedKeys reads the public keys whose releases this installation
// imports. A key that is not one is an error, not skipped: a typo would
// otherwise be found only when a release is refused.
func releaseTrustedKeys() ([]string, error) {
	raw, _ := envvar.FromEnvironment(EnvReleaseTrustedKeys)
	var keys []string
	for _, part := range strings.Split(raw, ",") {
		key := strings.TrimSpace(part)
		if key == "" {
			continue
		}
		if decoded, err := base64.StdEncoding.DecodeString(key); err != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%s holds %q, which is not a base64 Ed25519 public key", EnvReleaseTrustedKeys, key)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// ReleasePublicKey is the public key of this installation's release signing
// key, to give to the installations that import its releases.
func ReleasePublicKey() (string, error) {
	private, err := releaseSigningKey()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey)), nil
}
