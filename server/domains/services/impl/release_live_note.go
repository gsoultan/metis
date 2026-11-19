package impl

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// MaxReleaseLiveNoteBytes bounds a live note. A note names a release; it does
// not carry one.
const MaxReleaseLiveNoteBytes = 4 << 10

// ReleasePolicy says which releases need another environment's live note
// before they go live here.
//
// The server the engine runs in knows how its environments are connected:
// a production with a staging before it requires staging's note, and one
// that takes releases straight from development requires none. The engine
// only enforces the answer.
type ReleasePolicy interface {
	// RequiresLiveNote is whether a release of the project goes live here
	// only with another environment's note that it went live there.
	RequiresLiveNote(ctx context.Context, projectID uuid.UUID) (bool, error)
}

// ReleaseLiveNote is this environment's signed note that a release went live
// here, for the next environment to attach.
func (s *releaseService) ReleaseLiveNote(ctx context.Context, projectID uuid.UUID, name string) ([]byte, error) {
	rel, err := s.repo.Release().GetByName(ctx, projectID, strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	if rel.LiveAt == nil {
		return nil, apierr.Invalidf("release %s has not gone live here, so there is no note that it did", rel.Name)
	}
	private, err := s.keys.SigningKey(ctx)
	if err != nil {
		return nil, err
	}
	note := entities.ReleaseLiveNote{
		Format:  entities.ReleaseLiveNoteFormat,
		Release: rel.Name,
		Hash:    rel.Hash,
		LiveAt:  rel.LiveAt.UTC(),
		Key:     publicKeyText(private),
	}
	statement, err := entities.ReleaseLiveNoteStatement(note)
	if err != nil {
		return nil, err
	}
	note.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, statement))
	raw, err := json.MarshalIndent(note, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("could not write the release's live note: %w", err)
	}
	return raw, nil
}

// AttachReleaseLiveNote keeps another environment's note that a staged
// release went live there.
func (s *releaseService) AttachReleaseLiveNote(ctx context.Context, projectID uuid.UUID, name string, raw []byte) (entities.Release, error) {
	if len(raw) > MaxReleaseLiveNoteBytes {
		return entities.Release{}, apierr.Invalidf("a release's live note is at most %d KiB", MaxReleaseLiveNoteBytes>>10)
	}
	rel, err := s.repo.Release().GetByName(ctx, projectID, strings.TrimSpace(name))
	if err != nil {
		return entities.Release{}, err
	}
	if rel.State != entities.ReleaseStaged {
		return entities.Release{}, apierr.Invalidf("release %s is %s here; a live note is attached to a staged release", rel.Name, rel.State)
	}
	var note entities.ReleaseLiveNote
	if err := json.Unmarshal(raw, &note); err != nil {
		return entities.Release{}, apierr.Invalidf("this is not a release's live note: %v", err)
	}
	if err := s.verifyLiveNote(ctx, rel, note); err != nil {
		return entities.Release{}, err
	}
	rel.LiveNote = &note
	rel.ActedBy = releaseActor(ctx)
	if err := s.repo.Release().Update(ctx, rel); err != nil {
		return entities.Release{}, err
	}
	return rel, nil
}

// requireLiveNote refuses to make a release live here when the policy wants
// another environment's note and the release has no note this installation
// still trusts. Checked again at go live, because trust can be withdrawn
// after the note was attached.
func (s *releaseService) requireLiveNote(ctx context.Context, rel entities.Release) error {
	if s.policy == nil {
		return nil
	}
	required, err := s.policy.RequiresLiveNote(ctx, rel.Project.ID)
	if err != nil {
		return err
	}
	if !required {
		return nil
	}
	if rel.LiveNote == nil {
		return apierr.Invalidf("release %s goes live here once it has gone live in the environment before this one; attach that environment's live note", rel.Name)
	}
	return s.verifyLiveNote(ctx, rel, *rel.LiveNote)
}

// verifyLiveNote refuses a note for other content, in another shape, by a key
// this installation does not trust, or with a signature that key did not make.
func (s *releaseService) verifyLiveNote(ctx context.Context, rel entities.Release, note entities.ReleaseLiveNote) error {
	if note.Format != entities.ReleaseLiveNoteFormat {
		return apierr.Invalidf("this live note is %q, which this server does not read", note.Format)
	}
	if note.Hash != rel.Hash {
		return apierr.Invalidf("this live note is for other content than release %s; it went live as a different release", rel.Name)
	}
	if note.LiveAt.IsZero() {
		return apierr.Invalidf("this live note does not say when the release went live")
	}
	trusted, err := s.keys.TrustedKeys(ctx)
	if err != nil {
		return err
	}
	if !slices.Contains(trusted, note.Key) {
		return apierr.Invalidf("this live note is signed by a key this installation does not trust (%s)", note.Key)
	}
	invalid := apierr.Invalidf("this live note's signature is not valid")
	public, err := base64.StdEncoding.DecodeString(note.Key)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return invalid
	}
	signature, err := base64.StdEncoding.DecodeString(note.Signature)
	if err != nil {
		return invalid
	}
	statement, err := entities.ReleaseLiveNoteStatement(note)
	if err != nil || !ed25519.Verify(public, statement, signature) {
		return invalid
	}
	return nil
}
