package impl

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/peopletoken"
	"github.com/gsoultan/metis/server/domains/entities"
	"golang.org/x/crypto/bcrypt"
)

type directoryEntry struct {
	participant entities.WorkflowUser
	hash        string
	validFrom   time.Time
	removed     bool
}

// fakePeopleDirectory is the directory as the sign-in reads it.
type fakePeopleDirectory struct {
	entries map[uuid.UUID]*directoryEntry
}

func (d *fakePeopleDirectory) GetForSignIn(_ context.Context, projectID uuid.UUID, username string) (entities.WorkflowUser, string, error) {
	for _, e := range d.entries {
		if !e.removed && e.participant.Project.ID == projectID && e.participant.Username == username {
			return e.participant, e.hash, nil
		}
	}
	return entities.WorkflowUser{}, "", fmt.Errorf("%w: nobody", apierr.ErrNotFound)
}

func (d *fakePeopleDirectory) GetSession(_ context.Context, id uuid.UUID) (entities.WorkflowUser, time.Time, error) {
	e, ok := d.entries[id]
	if !ok || e.removed {
		return entities.WorkflowUser{}, time.Time{}, fmt.Errorf("%w: nobody", apierr.ErrNotFound)
	}
	return e.participant, e.validFrom, nil
}

type signInFixture struct {
	svc       *peopleSignInService
	directory *fakePeopleDirectory
	ada       *directoryEntry
	project   uuid.UUID
}

func newSignInFixture(t *testing.T) signInFixture {
	t.Helper()
	keys, err := peopletoken.NewKeys("people-key-one-7c1f0e9a4b2d8f63e5a0c9b1d7e3f2a4", "")
	if err != nil {
		t.Fatalf("NewKeys: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("correct horse"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	project := uuid.New()
	ada := &directoryEntry{
		participant: entities.WorkflowUser{
			ID: uuid.New(), Project: &entities.Project{ID: project},
			Username: "ada", DisplayName: "Ada", Active: true, HasCredentials: true,
		},
		hash: string(hash),
	}
	directory := &fakePeopleDirectory{entries: map[uuid.UUID]*directoryEntry{ada.participant.ID: ada}}
	svc := NewPeopleSignInService(directory, keys).(*peopleSignInService)
	return signInFixture{svc: svc, directory: directory, ada: ada, project: project}
}

func TestAPersonSignsInToTheirProjectAndTheTokenSignsThemIn(t *testing.T) {
	f := newSignInFixture(t)
	person, token, expires, err := f.svc.SignInPerson(t.Context(), f.project, "ada", "correct horse")
	if err != nil {
		t.Fatalf("SignInPerson: %v", err)
	}
	if person.ID != f.ada.participant.ID || person.ProjectID != f.project || expires.IsZero() {
		t.Fatalf("person = %+v, expires = %v", person, expires)
	}
	got, err := f.svc.AuthenticatePerson(t.Context(), token)
	if err != nil {
		t.Fatalf("AuthenticatePerson: %v", err)
	}
	if got.ID != person.ID || got.Username != "ada" {
		t.Fatalf("authenticated %+v, want ada", got)
	}
}

func TestEveryFailedSignInReadsTheSame(t *testing.T) {
	f := newSignInFixture(t)
	otherProject := uuid.New()
	noPassword := &directoryEntry{participant: entities.WorkflowUser{
		ID: uuid.New(), Project: &entities.Project{ID: f.project}, Username: "bob", Active: true,
	}}
	inactive := &directoryEntry{participant: entities.WorkflowUser{
		ID: uuid.New(), Project: &entities.Project{ID: f.project}, Username: "cy", Active: false,
	}, hash: f.ada.hash}
	f.directory.entries[noPassword.participant.ID] = noPassword
	f.directory.entries[inactive.participant.ID] = inactive

	cases := []struct {
		name, username, password string
		project                  uuid.UUID
	}{
		{"wrong password", "ada", "wrong", f.project},
		{"nobody of that name", "zed", "correct horse", f.project},
		{"right name, another project", "ada", "correct horse", otherProject},
		{"no password set", "bob", "", f.project},
		{"no password set, one given", "bob", "anything", f.project},
		{"deactivated, right password", "cy", "correct horse", f.project},
	}
	var first string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, token, _, err := f.svc.SignInPerson(t.Context(), tc.project, tc.username, tc.password)
			if !errors.Is(err, auth.ErrAuthenticationFailed) || token != "" {
				t.Fatalf("SignInPerson = %q, %v; want no token and ErrAuthenticationFailed", token, err)
			}
			if first == "" {
				first = err.Error()
			} else if err.Error() != first {
				t.Fatalf("refusal %q reads differently from %q", err, first)
			}
		})
	}
}

func TestRepeatedWrongPasswordsAreThrottledPerProjectAndName(t *testing.T) {
	f := newSignInFixture(t)
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	f.svc.now = func() time.Time { return now }
	for range 10 {
		_, _, _, _ = f.svc.SignInPerson(t.Context(), f.project, "ada", "wrong")
	}
	_, _, _, err := f.svc.SignInPerson(t.Context(), f.project, "ada", "correct horse")
	if err == nil {
		t.Fatal("the right password got through a throttled name")
	}
	// The same name in another project is somebody else, and is not slowed.
	other := uuid.New()
	ada2 := &directoryEntry{participant: entities.WorkflowUser{
		ID: uuid.New(), Project: &entities.Project{ID: other}, Username: "ada", Active: true,
	}, hash: f.ada.hash}
	f.directory.entries[ada2.participant.ID] = ada2
	if _, _, _, err := f.svc.SignInPerson(t.Context(), other, "ada", "correct horse"); err != nil {
		t.Fatalf("ada in another project was throttled: %v", err)
	}
}

func TestATokenStopsSigningInWhenTheDirectorySaysSo(t *testing.T) {
	cases := []struct {
		name   string
		change func(f signInFixture)
	}{
		{"removed", func(f signInFixture) { f.ada.removed = true }},
		{"deactivated", func(f signInFixture) { f.ada.participant.Active = false }},
		{"password changed afterwards", func(f signInFixture) { f.ada.validFrom = time.Now().Add(time.Hour) }},
		{"moved to another project", func(f signInFixture) { f.ada.participant.Project = &entities.Project{ID: uuid.New()} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSignInFixture(t)
			_, token, _, err := f.svc.SignInPerson(t.Context(), f.project, "ada", "correct horse")
			if err != nil {
				t.Fatalf("SignInPerson: %v", err)
			}
			tc.change(f)
			if _, err := f.svc.AuthenticatePerson(t.Context(), token); !errors.Is(err, auth.ErrUnauthorized) {
				t.Fatalf("AuthenticatePerson = %v, want ErrUnauthorized", err)
			}
		})
	}
}

func TestWithoutAKeyNobodySignsInAsAPerson(t *testing.T) {
	svc := NewUnavailablePeopleSignInService("people cannot sign in: set " + peopletoken.EnvKey)
	if _, token, _, err := svc.SignInPerson(t.Context(), uuid.New(), "ada", "x"); err == nil || token != "" {
		t.Fatalf("SignInPerson = %q, %v; want a refusal", token, err)
	}
	if _, err := svc.AuthenticatePerson(t.Context(), "v4.local.x"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("AuthenticatePerson = %v, want ErrUnauthorized", err)
	}
}
