package release_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// requireLiveNote is the policy of a production with a staging before it.
type requireLiveNote bool

func (r requireLiveNote) RequiresLiveNote(context.Context, uuid.UUID) (bool, error) {
	return bool(r), nil
}

func signingKey(t *testing.T, fill byte) (ed25519.PrivateKey, string) {
	t.Helper()
	seed, public := releaseKey(t, fill)
	raw, _ := base64.StdEncoding.DecodeString(seed)
	return ed25519.NewKeyFromSeed(raw), public
}

func refused(t *testing.T, err error, want, what string) {
	t.Helper()
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s got %v, want a refusal saying %q", what, err, want)
	}
}

// A production with a staging before it makes a release live only once
// staging has: staging signs a note that the release went live there, and
// production keeps it, checks it is for exactly this release and from a key
// it trusts, and checks it again when the release goes live.
func TestProductionWithAStagingTakesAReleaseLiveOnlyWithStagingsNote(t *testing.T) {
	devKey, devPublic := signingKey(t, 11)
	stagingKey, stagingPublic := signingKey(t, 12)
	_, strangerPublic := signingKey(t, 13)
	strangerKey, _ := signingKey(t, 13)

	newInstallation := func(name string, keys fixedKeys, options ...services.FacadeOption) (contracts.ReleaseService, repositories.Repository, context.Context, uuid.UUID) {
		repo := repositories.NewRepository(testutils.SetupTestDB(t))
		svc := services.NewServiceFacade(repo, impl.NewEventDispatcher(), impl.NewSSEObserver(), name, nil, nil, nil,
			append(options, services.WithReleaseKeys(keys))...)
		ctx, _, project := testutils.ScopedProject(t, repo)
		return svc, repo, ctx, project
	}

	// Development cuts the release and signs it.
	devRepo := repositories.NewRepository(testutils.SetupTestDB(t))
	dev := services.NewServiceFacade(devRepo, impl.NewEventDispatcher(), impl.NewSSEObserver(), "live-note-dev", nil, nil, nil,
		services.WithReleaseKeys(fixedKeys{signing: devKey}))
	devCtx, _, devProject := testutils.ScopedProject(t, devRepo)
	w := world{svc: dev, dev: devCtx, projectID: devProject}
	w.deployDecision(t, devCtx, "low")
	w.deployProcess(t, devCtx, "Claims", nil)
	if _, err := dev.CutRelease(devCtx, contracts.CutRelease{ProjectID: devProject, Name: "r1", Processes: []string{"claims"}}); err != nil {
		t.Fatalf("cut: %v", err)
	}
	file, err := dev.ExportRelease(devCtx, devProject, "r1")
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Staging takes it live, and only then has a note to give.
	staging, _, stagingCtx, stagingProject := newInstallation("live-note-staging",
		fixedKeys{signing: stagingKey, trusted: []string{devPublic}})
	if _, err := staging.ImportReleaseFile(stagingCtx, stagingProject, file); err != nil {
		t.Fatalf("staging import: %v", err)
	}
	_, err = staging.ReleaseLiveNote(stagingCtx, stagingProject, "r1")
	refused(t, err, "has not gone live here", "a note for a release not live in staging")
	if _, err := staging.GoLive(stagingCtx, stagingProject, "r1"); err != nil {
		t.Fatalf("staging go live: %v", err)
	}
	note, err := staging.ReleaseLiveNote(stagingCtx, stagingProject, "r1")
	if err != nil {
		t.Fatalf("staging's note: %v", err)
	}

	// Production needs that note.
	prodKeys := fixedKeys{trusted: []string{devPublic, stagingPublic, strangerPublic}}
	prod, prodRepo, prodCtx, prodProject := newInstallation("live-note-prod", prodKeys, services.WithReleasePolicy(requireLiveNote(true)))
	if _, err := prod.ImportReleaseFile(prodCtx, prodProject, file); err != nil {
		t.Fatalf("production import: %v", err)
	}
	_, err = prod.GoLive(prodCtx, prodProject, "r1")
	refused(t, err, "attach that environment's live note", "going live without staging's note")

	var parsed entities.ReleaseLiveNote
	if err := json.Unmarshal(note, &parsed); err != nil {
		t.Fatalf("read the note: %v", err)
	}
	for what, change := range map[string]func(n *entities.ReleaseLiveNote){
		"a note for other content":     func(n *entities.ReleaseLiveNote) { n.Hash = strings.Repeat("0", 64) },
		"a note with its time changed": func(n *entities.ReleaseLiveNote) { n.LiveAt = n.LiveAt.Add(-time.Hour) },
		"a note in another shape":      func(n *entities.ReleaseLiveNote) { n.Format = "metis.release-live/v9" },
	} {
		changed := parsed
		change(&changed)
		raw, _ := json.Marshal(changed)
		_, err := prod.AttachReleaseLiveNote(prodCtx, prodProject, "r1", raw)
		if !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Fatalf("%s got %v, want a refusal", what, err)
		}
	}

	// A note signed by a trusted key that is not the one it names.
	forged := parsed
	forged.Key = strangerPublic
	statement, _ := entities.ReleaseLiveNoteStatement(forged)
	forged.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(stagingKey, statement))
	raw, _ := json.Marshal(forged)
	_, err = prod.AttachReleaseLiveNote(prodCtx, prodProject, "r1", raw)
	refused(t, err, "signature is not valid", "a note whose signature is not by the key it names")
	forged.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(strangerKey, statement))
	raw, _ = json.Marshal(forged)
	if _, err := prod.AttachReleaseLiveNote(prodCtx, prodProject, "r1", raw); err != nil {
		t.Fatalf("a properly signed note from a trusted key: %v", err)
	}

	untrusting := services.NewServiceFacade(prodRepo, impl.NewEventDispatcher(), impl.NewSSEObserver(), "live-note-prod", nil, nil, nil,
		services.WithReleaseKeys(fixedKeys{trusted: []string{devPublic}}), services.WithReleasePolicy(requireLiveNote(true)))
	_, err = untrusting.AttachReleaseLiveNote(prodCtx, prodProject, "r1", note)
	refused(t, err, "does not trust", "staging's note where staging is not trusted")
	_, err = untrusting.GoLive(prodCtx, prodProject, "r1")
	refused(t, err, "does not trust", "going live on a note whose key is no longer trusted")

	attached, err := prod.AttachReleaseLiveNote(prodCtx, prodProject, "r1", note)
	if err != nil {
		t.Fatalf("attach staging's note: %v", err)
	}
	if attached.LiveNote == nil || attached.LiveNote.Key != stagingPublic {
		t.Fatalf("the release keeps note %+v, want staging's", attached.LiveNote)
	}
	got, err := prod.GetRelease(prodCtx, prodProject, "r1")
	if err != nil || got.LiveNote == nil || got.LiveNote.Hash != got.Hash {
		t.Fatalf("read back the release with note %+v (%v)", got.LiveNote, err)
	}
	live, err := prod.GoLive(prodCtx, prodProject, "r1")
	if err != nil {
		t.Fatalf("go live with staging's note: %v", err)
	}
	if live.State != entities.ReleaseLive {
		t.Fatalf("release is %s, want live", live.State)
	}
	_, err = prod.AttachReleaseLiveNote(prodCtx, prodProject, "r1", note)
	refused(t, err, "attached to a staged release", "a note for a release already live")
}

// A production that takes releases straight from development needs no note.
func TestAProductionWithNoStagingNeedsNoLiveNote(t *testing.T) {
	devKey, devPublic := signingKey(t, 21)
	devRepo := repositories.NewRepository(testutils.SetupTestDB(t))
	dev := services.NewServiceFacade(devRepo, impl.NewEventDispatcher(), impl.NewSSEObserver(), "no-staging-dev", nil, nil, nil,
		services.WithReleaseKeys(fixedKeys{signing: devKey}))
	devCtx, _, devProject := testutils.ScopedProject(t, devRepo)
	w := world{svc: dev, dev: devCtx, projectID: devProject}
	w.deployDecision(t, devCtx, "low")
	w.deployProcess(t, devCtx, "Claims", nil)
	if _, err := dev.CutRelease(devCtx, contracts.CutRelease{ProjectID: devProject, Name: "r1", Processes: []string{"claims"}}); err != nil {
		t.Fatalf("cut: %v", err)
	}
	file, err := dev.ExportRelease(devCtx, devProject, "r1")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	prodRepo := repositories.NewRepository(testutils.SetupTestDB(t))
	prod := services.NewServiceFacade(prodRepo, impl.NewEventDispatcher(), impl.NewSSEObserver(), "no-staging-prod", nil, nil, nil,
		services.WithReleaseKeys(fixedKeys{trusted: []string{devPublic}}), services.WithReleasePolicy(requireLiveNote(false)))
	prodCtx, _, prodProject := testutils.ScopedProject(t, prodRepo)
	if _, err := prod.ImportReleaseFile(prodCtx, prodProject, file); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := prod.GoLive(prodCtx, prodProject, "r1"); err != nil {
		t.Fatalf("go live without a staging: %v", err)
	}
}
