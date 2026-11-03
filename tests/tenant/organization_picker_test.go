package tenant

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// The organization picker lists every organization the signed-in account
// belongs to, whichever one the request is for, and no other.
//
// It listed only the one being worked in, so somebody in three organizations
// saw one, and the header had nothing to switch to.
func TestThePickerListsEveryOrganizationTheAccountIsIn(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx, home := testutils.ScopedContext(t, repo)

	second := seedOrganization(t, repo, "Second")
	stranger := seedOrganization(t, repo, "Not theirs")

	account := uuid.Must(uuid.NewV7())

	// The principal as token validation leaves it: the account, with the
	// organizations it is in.
	signedIn := context.WithValue(ctx, auth.UserContextKey, entities.User{
		ID:            account,
		Username:      "picker",
		Organizations: []*entities.Organization{{ID: home}, {ID: second}},
	})
	listed, err := impl.NewOrganizationService(repo, nil).ListOrganizations(signedIn)
	if err != nil {
		t.Fatalf("list organizations: %v", err)
	}

	got := map[uuid.UUID]bool{}
	for _, o := range listed {
		got[o.ID] = true
	}
	if !got[home] || !got[second] {
		t.Errorf("the picker lists %v; want both organizations the account is in (%s, %s)", listed, home, second)
	}
	if got[stranger] {
		t.Errorf("the picker lists %s, which the account is not in", stranger)
	}
	if len(listed) != 2 {
		t.Errorf("the picker lists %d organizations; want 2", len(listed))
	}
}

// Without anybody signed in — setup, or system work — the list is the
// repository's, which for system work is every organization.
func TestTheOrganizationListWithNobodySignedInIsUnchanged(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	_, home := testutils.ScopedContext(t, repo)
	other := seedOrganization(t, repo, "Other")

	listed, err := impl.NewOrganizationService(repo, nil).ListOrganizations(entities.WithSystemContext(t.Context()))
	if err != nil {
		t.Fatalf("list organizations: %v", err)
	}
	got := map[uuid.UUID]bool{}
	for _, o := range listed {
		got[o.ID] = true
	}
	if !got[home] || !got[other] {
		t.Errorf("system work lists %v; want every organization", listed)
	}
}

func seedOrganization(t *testing.T, repo repositories.Repository, name string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if err := repo.Organization().Create(entities.WithSystemContext(t.Context()), models.OrganizationModel{
		Base: models.Base{ID: models.UUID(id)},
		Name: name,
	}); err != nil {
		t.Fatalf("seed organization %s: %v", name, err)
	}
	return id
}
