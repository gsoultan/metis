package release_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

func releaseKey(t *testing.T, fill byte) (seed, public string) {
	t.Helper()
	raw := make([]byte, ed25519.SeedSize)
	for i := range raw {
		raw[i] = fill
	}
	key := ed25519.NewKeyFromSeed(raw)
	return base64.StdEncoding.EncodeToString(raw), base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey))
}

// A release reaches a production installation of its own as a file signed
// by the installation it was cut in, and is imported only when production
// trusts that key and the content is what was signed.
func TestAReleaseFileMovesAReleaseBetweenInstallations(t *testing.T) {
	dev := newWorld(t)
	seed, public := releaseKey(t, 7)
	t.Setenv(serviceimpl.EnvReleaseSigningKey, seed)
	t.Setenv(serviceimpl.EnvReleaseTrustedKeys, public)

	dev.deployDecision(t, dev.dev, "low")
	dev.deployProcess(t, dev.dev, "Claims", nil)
	if _, err := dev.svc.CutRelease(dev.dev, contracts.CutRelease{ProjectID: dev.projectID, Name: "r1", Processes: []string{"claims"}}); err != nil {
		t.Fatalf("cut: %v", err)
	}
	file, err := dev.svc.ExportRelease(dev.dev, dev.projectID, "r1")
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Production: another installation, its own database and its own project.
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	sse := impl.NewSSEObserver()
	prod := services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "release-file-test", nil, nil, nil)
	prodCtx, _, prodProject := testutils.ScopedProject(t, repo)

	tampered := strings.Replace(string(file), `"low"`, `"unlimited"`, 1)
	if _, err := prod.ImportReleaseFile(prodCtx, prodProject, []byte(tampered)); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("a changed file got %v, want a refusal", err)
	}

	_, stranger := releaseKey(t, 9)
	t.Setenv(serviceimpl.EnvReleaseTrustedKeys, stranger)
	if _, err := prod.ImportReleaseFile(prodCtx, prodProject, file); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "does not trust") {
		t.Fatalf("a file signed by an untrusted key got %v, want a refusal", err)
	}

	t.Setenv(serviceimpl.EnvReleaseTrustedKeys, stranger+", "+public)
	staged, err := prod.ImportReleaseFile(prodCtx, prodProject, file)
	if err != nil {
		t.Fatalf("import the file: %v", err)
	}
	if staged.Name != "r1" || staged.Hash == "" {
		t.Fatalf("imported %q with hash %q", staged.Name, staged.Hash)
	}
	if _, err := prod.GoLive(prodCtx, prodProject, "r1"); err != nil {
		t.Fatalf("go live: %v", err)
	}
	res, err := prod.Evaluate(prodCtx, prodProject, "limit", 0, map[string]any{})
	if err != nil || res.Values["limit"] != "low" {
		t.Fatalf("production's decision answers %v (%v), want the release's", res.Values, err)
	}
}
