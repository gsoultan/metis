package impl

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// maxUsernameBase leaves room, under the username column's 255 characters, for
// the suffix a clash adds.
const maxUsernameBase = 200

// createLinkedAccount gives an identity its account, at the first sign-in.
//
// The username is a label, not the identity, so a clash is settled by choosing
// another — never by linking to the account that holds it. A clash can also be
// another request signing the same person in at the same moment; looking the
// identity up again finds the account that one created.
func (s *userService) createLinkedAccount(ctx context.Context, claims entities.IdentityClaims, organizations []uuid.UUID) (uuid.UUID, error) {
	for _, username := range usernameCandidates(claims) {
		created, err := s.repo.User().CreateLinked(ctx,
			newLinkedAccount(claims, username, organizations), claims.Issuer, claims.Subject)
		if err == nil {
			return uuid.UUID(created.ID), nil
		}
		if !errors.Is(err, repocontracts.ErrAccountTaken) {
			return uuid.Nil, err
		}
		if linked, err := s.repo.User().GetByIdentity(ctx, claims.Issuer, claims.Subject); err == nil {
			return s.keepInStep(ctx, uuid.UUID(linked.ID), organizations)
		}
	}
	return uuid.Nil, errors.New("could not give the account a username: every one tried is taken")
}

// newLinkedAccount is the account a first sign-in is given.
func newLinkedAccount(claims entities.IdentityClaims, username string, organizations []uuid.UUID) models.UserModel {
	profile := profileFromClaims(claims)
	return adapters.UserModelAdapter{User: entities.User{
		ID:          uuid.Must(uuid.NewV7()),
		Username:    username,
		FullName:    profile.FullName,
		DisplayName: profile.DisplayName,
		Email:       profile.Email,
		// No role at all. The task inbox asks for none — every endpoint on it
		// needs only a signed-in member of the organization — and an account
		// that has just appeared should hold nothing an administrator did not
		// decide on. One can grant DESIGNER, OPERATOR or ADMIN afterwards.
		Roles:         []string{},
		Organizations: organizationShells(organizations),
	}}.ToModel()
}

// usernameCandidates are the usernames a new linked account may take, best
// first: the provider's preferred username, else the email, else the subject —
// then the same with a random suffix, for when somebody holds it already.
// Usernames are never reissued, so the suffix is also what lets a person whose
// earlier account was deleted be given a new one.
func usernameCandidates(claims entities.IdentityClaims) []string {
	base := cmp.Or(strings.TrimSpace(claims.Username), strings.TrimSpace(claims.Email), claims.Subject)
	base = truncateRunes(base, maxUsernameBase)
	return []string{base, base + "-" + strings.ToLower(rand.Text()[:8])}
}

// profileFromClaims keeps what the provider says about how somebody is named
// and reached, where it is something a profile may hold. A sign-in is not
// refused over it; the person can put it right on their Profile page.
func profileFromClaims(claims entities.IdentityClaims) entities.Profile {
	name := truncateRunes(strings.TrimSpace(claims.Name), maxProfileNameLength)
	profile := entities.Profile{FullName: name, DisplayName: name}
	if withEmail, err := validProfile(entities.Profile{Email: claims.Email}); err == nil {
		profile.Email = withEmail.Email
	}
	return profile
}

func organizationShells(ids []uuid.UUID) []*entities.Organization {
	shells := make([]*entities.Organization, 0, len(ids))
	for _, id := range ids {
		shells = append(shells, &entities.Organization{ID: id})
	}
	return shells
}

func truncateRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
}
