package impl

import (
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

// organizationsNamedBy reads which organizations a sign-in's claim names.
//
// Absent constraint means deny. With no claim configured, or none in the
// token, nobody is placed anywhere — and the refusal says which of the two it
// was, because one is an operator's to fix and the other the identity
// provider's. The person is authenticated either way, so it is a 403, not a
// request to sign in again.
//
// A value is matched against an organization's id: the one identifier an
// organization has that is unique and never changes. Its name is neither —
// two organizations may share one, and a rename would silently move people.
// A value that is not an id is not an organization here, and is skipped.
func organizationsNamedBy(claims entities.IdentityClaims) ([]uuid.UUID, error) {
	if claims.OrganizationClaim == "" {
		return nil, apierr.Forbiddenf(
			"this installation signs people in through an identity provider but has not been told "+
				"which ID-token claim lists their organizations, so it cannot place you in one; "+
				"an operator has to set %s", auth.EnvOrganizationClaim)
	}
	if !claims.HasOrganizationClaim {
		return nil, apierr.Forbiddenf(
			"the ID token from your identity provider has no %q claim, which is where this installation "+
				"reads the organizations you belong to; ask your administrator to have the identity provider send it",
			claims.OrganizationClaim)
	}

	seen := make(map[uuid.UUID]bool, len(claims.Organizations))
	named := make([]uuid.UUID, 0, len(claims.Organizations))
	for _, value := range claims.Organizations {
		id, err := uuid.Parse(value)
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		named = append(named, id)
	}
	return named, nil
}

// refusalNamingNoOrganization refuses a claim that names nothing here.
// Organizations are never created from a claim: a value that names none is
// either a mistake at the provider or an organization since deleted, and
// neither is a reason to make one.
func refusalNamingNoOrganization(claim string) error {
	return apierr.Forbiddenf(
		"none of the organizations named in the %q claim of your ID token exists here; ask your administrator "+
			"to check that the identity provider sends the ids of this installation's organizations in it", claim)
}
