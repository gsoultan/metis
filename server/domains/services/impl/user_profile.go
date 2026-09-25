package impl

import (
	"context"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// The longest name and email a profile may hold. A name is shown to everybody
// in the organization, so it is bounded like the username column; 320 is the
// longest address mail allows, and the bound the other account tables already
// put on theirs.
const (
	maxProfileNameLength  = 255
	maxProfileEmailLength = 320
)

// GetOwnProfile returns the signed-in account's own record.
//
// No visibility check: the ID comes from the verified session, and the tenant
// a request runs in is always one of its own account's organizations.
func (s *userService) GetOwnProfile(ctx context.Context, userID uuid.UUID) (entities.User, error) {
	m, err := s.repo.User().Get(ctx, userID)
	if err != nil {
		return entities.User{}, err
	}
	return adapters.UserEntityAdapter{Model: m}.ToEntity(), nil
}

// UpdateOwnProfile changes how the signed-in account is named and reached.
//
// The Profile page saved through UpdateUser, which only an administrator may
// call, so for everybody else it failed. This writes the profile's three
// fields and nothing else; each is written as given, empty included, because
// clearing a name is an edit UpdateUser allows too.
func (s *userService) UpdateOwnProfile(ctx context.Context, userID uuid.UUID, profile entities.Profile) error {
	profile, err := validProfile(profile)
	if err != nil {
		return err
	}
	if err := s.repo.User().SetProfile(ctx, models.UserModel{
		Base:        models.Base{ID: models.UUID(userID)},
		FullName:    profile.FullName,
		DisplayName: profile.DisplayName,
		Email:       profile.Email,
	}); err != nil {
		return err
	}
	// The cached principal carries the names; the next request should see the
	// new ones rather than wait out the entry.
	s.principals.forget(userID)
	return nil
}

// validProfile trims a profile and refuses what cannot be stored or mailed.
//
// The email is where notifications go, and it is written into a mail header,
// so it has to be one bare address. A name around it, or a line break after
// it, is nothing a person types into an email field by accident.
func validProfile(p entities.Profile) (entities.Profile, error) {
	p = entities.Profile{
		FullName:    strings.TrimSpace(p.FullName),
		DisplayName: strings.TrimSpace(p.DisplayName),
		Email:       strings.TrimSpace(p.Email),
	}
	if utf8.RuneCountInString(p.FullName) > maxProfileNameLength ||
		utf8.RuneCountInString(p.DisplayName) > maxProfileNameLength {
		return entities.Profile{}, apierr.Invalidf("a name can be at most %d characters", maxProfileNameLength)
	}
	if p.Email == "" {
		return p, nil
	}
	if len(p.Email) > maxProfileEmailLength {
		return entities.Profile{}, apierr.Invalidf("an email address can be at most %d characters", maxProfileEmailLength)
	}
	if address, err := mail.ParseAddress(p.Email); err != nil || address.Address != p.Email {
		return entities.Profile{}, apierr.Invalidf("the email has to be a single address, like name@example.com")
	}
	return p, nil
}
