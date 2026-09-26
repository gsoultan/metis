package user_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	service_impl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// Accounts are installation-wide in the repository — an account is read by ID
// while a token is being validated, before there is a tenant to scope by — and
// the service never drew the line the repository could not. CreateUser checked
// the organization ("being an admin grants authority over your own
// organization, not over every organization"); GetUser, UpdateUser and
// DeleteUser did not. So any signed-in account could read another
// organization's accounts by ID, and an administrator of one organization
// could change the roles of, or delete, another's — its last administrator
// included.

type accountWorld struct {
	svc    contracts.UserService
	repo   repositories.Repository
	orgA   uuid.UUID
	orgB   uuid.UUID
	adminA entities.User
	userA  entities.User
	adminB entities.User
	userB  entities.User
}

func newAccountWorld(t *testing.T) accountWorld {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	w := accountWorld{
		svc:  service_impl.NewUserService(repo, "account-tenant-test-secret"),
		repo: repo,
		orgA: seedOrganization(t, repo, "Organization A"),
		orgB: seedOrganization(t, repo, "Organization B"),
	}
	w.adminA = w.seedAccount(t, "admin-a", []string{entities.RoleAdmin}, w.orgA)
	w.userA = w.seedAccount(t, "user-a", nil, w.orgA)
	w.adminB = w.seedAccount(t, "admin-b", []string{entities.RoleAdmin}, w.orgB)
	w.userB = w.seedAccount(t, "user-b", nil, w.orgB)
	return w
}

func seedOrganization(t *testing.T, repo repositories.Repository, name string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if err := repo.Organization().Create(entities.WithSystemContext(t.Context()), models.OrganizationModel{
		Base: models.Base{ID: models.UUID(id)},
		Name: name,
	}); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
	return id
}

func (w accountWorld) seedAccount(t *testing.T, username string, roles []string, orgs ...uuid.UUID) entities.User {
	t.Helper()
	account := entities.User{ID: uuid.Must(uuid.NewV7()), Username: username, FullName: username, Roles: roles}
	for _, org := range orgs {
		account.Organizations = append(account.Organizations, &entities.Organization{ID: org})
	}
	if err := w.svc.CreateUser(entities.WithSystemContext(t.Context()), account, "a-password-long-enough"); err != nil {
		t.Fatalf("seed %s: %v", username, err)
	}
	return account
}

// actingAs is a request from caller, in the organization they are working in —
// what the auth interceptor and the tenant resolver hand an endpoint.
func actingAs(t *testing.T, caller entities.User, tenant uuid.UUID) context.Context {
	t.Helper()
	ctx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: tenant.String()})
	return context.WithValue(ctx, pkgauth.UserContextKey, caller)
}

func (w accountWorld) stored(t *testing.T, id uuid.UUID) (models.UserModel, error) {
	t.Helper()
	return w.repo.User().Get(entities.WithSystemContext(t.Context()), id)
}

func TestAnotherOrganizationsAccountCannotBeReadByID(t *testing.T) {
	w := newAccountWorld(t)

	_, err := w.svc.GetUser(actingAs(t, w.userA, w.orgA), w.userB.ID)
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("an account in another organization must read as not found, got %v", err)
	}

	if _, err := w.svc.GetUser(actingAs(t, w.userA, w.orgA), w.adminA.ID); err != nil {
		t.Fatalf("an account in the caller's own organization must still be readable: %v", err)
	}
}

func TestAnotherOrganizationsAccountCannotBeChanged(t *testing.T) {
	w := newAccountWorld(t)

	err := w.svc.UpdateUser(actingAs(t, w.adminA, w.orgA), entities.User{
		ID: w.userB.ID, Username: "user-b", FullName: "taken over", Roles: []string{entities.RoleAdmin},
	})
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("changing another organization's account must be refused as not found, got %v", err)
	}
	after, err := w.stored(t, w.userB.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if entities.HasRole(after.Roles, entities.RoleAdmin) || after.FullName == "taken over" {
		t.Fatalf("the refused change was written anyway: %+v", after)
	}
}

func TestAnotherOrganizationsAccountCannotBeDeleted(t *testing.T) {
	w := newAccountWorld(t)

	if err := w.svc.DeleteUser(actingAs(t, w.adminA, w.orgA), w.adminB.ID); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("deleting another organization's administrator must be refused as not found, got %v", err)
	}
	if _, err := w.stored(t, w.adminB.ID); err != nil {
		t.Fatalf("the refused deletion happened anyway: %v", err)
	}
}

// Roles are global, so a change to an account acts in every organization it
// belongs to. Authority over one of them is not authority over the others.
func TestAnAccountSharedWithAnotherOrganizationNeedsAuthorityInBoth(t *testing.T) {
	w := newAccountWorld(t)
	shared := w.seedAccount(t, "shared", nil, w.orgA, w.orgB)

	err := w.svc.UpdateUser(actingAs(t, w.adminA, w.orgA), entities.User{
		ID: shared.ID, Username: "shared", Roles: []string{entities.RoleAdmin},
	})
	if !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("an administrator of one organization changed an account another shares, got %v", err)
	}

	both := w.seedAccount(t, "admin-both", []string{entities.RoleAdmin}, w.orgA, w.orgB)
	if err := w.svc.UpdateUser(actingAs(t, both, w.orgA), entities.User{
		ID: shared.ID, Username: "shared", FullName: "Shared Person",
	}); err != nil {
		t.Fatalf("an administrator of every organization the account is in must be able to change it: %v", err)
	}
}

// There is no default account, so an organization whose last administrator
// is deleted or demoted cannot be administered through Metis again.
func TestTheLastAdministratorOfAnOrganizationCannotBeRemoved(t *testing.T) {
	w := newAccountWorld(t)
	asAdminA := actingAs(t, w.adminA, w.orgA)

	if err := w.svc.DeleteUser(asAdminA, w.adminA.ID); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("deleting an organization's only administrator must be refused, got %v", err)
	}
	if err := w.svc.UpdateUser(asAdminA, entities.User{
		ID: w.adminA.ID, Username: "admin-a", Roles: []string{entities.RoleDesigner},
	}); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("demoting an organization's only administrator must be refused, got %v", err)
	}

	// With a second administrator in place, the first may go.
	if err := w.svc.UpdateUser(asAdminA, entities.User{
		ID: w.userA.ID, Username: "user-a", Roles: []string{entities.RoleAdmin},
	}); err != nil {
		t.Fatalf("promote a second administrator: %v", err)
	}
	if err := w.svc.DeleteUser(asAdminA, w.adminA.ID); err != nil {
		t.Fatalf("an administrator who is not the last must be removable: %v", err)
	}
}

// A refusal is shown to the administrator word for word — the role matrix on
// the Platform access page puts it in a notification as it is — and both of
// these named no organization: "admin-a is the last administrator of ; make
// somebody else an administrator first". An account is read with its
// memberships as bare ids, so the name was never there to print.
//
// The last administrator's organization is the caller's own, so it is named.
// The organization a shared account also belongs to is one the caller is not
// in, and its name is not theirs to read, so it is not.
func TestARefusedAccountChangeSaysWhichOrganizationItIsAbout(t *testing.T) {
	w := newAccountWorld(t)
	shared := w.seedAccount(t, "shared", nil, w.orgA, w.orgB)
	asAdminA := actingAs(t, w.adminA, w.orgA)

	lastAdministrator := "forbidden: admin-a is the last administrator of Organization A; " +
		"make somebody else an administrator first"
	cases := []struct {
		name   string
		change func() error
		want   string
	}{
		{"demoting the last administrator", func() error {
			return w.svc.UpdateUser(asAdminA, entities.User{ID: w.adminA.ID, Username: "admin-a", Roles: []string{}})
		}, lastAdministrator},
		{"deleting the last administrator", func() error {
			return w.svc.DeleteUser(asAdminA, w.adminA.ID)
		}, lastAdministrator},
		{"changing an account another organization shares", func() error {
			return w.svc.UpdateUser(asAdminA, entities.User{
				ID: shared.ID, Username: "shared", Roles: []string{entities.RoleOperator},
			})
		}, "forbidden: shared also belongs to another organization, which you are not a member of; " +
			"an administrator there has to make this change"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.change()
			if !errors.Is(err, apierr.ErrForbidden) {
				t.Fatalf("got %v, want a refusal", err)
			}
			if err.Error() != tc.want {
				t.Errorf("the refusal reads\n  %q\nwant\n  %q", err.Error(), tc.want)
			}
			if strings.Contains(err.Error(), "Organization B") {
				t.Errorf("the refusal names an organization the caller is not in: %q", err.Error())
			}
		})
	}
}
