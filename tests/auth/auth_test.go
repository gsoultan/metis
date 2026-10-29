package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/stretchr/testify/assert"
)

func TestUserAuthentication(t *testing.T) {
	// 1. Setup DB
	db := testutils.SetupTestDB(t)

	// 2. Setup Repo & Service
	repo := repositories.NewRepository(db)
	jwtSecret := "test-secret"
	userSvc := impl.NewUserService(repo, jwtSecret)

	ctx := t.Context()
	// A real organization, not an invented id. user_organizations references
	// organizations, so a membership pointing at one that does not exist is
	// refused by the database — which is right, and which the previous schema
	// let through.
	orgID := seedOrganization(t, repo)

	// 3. Register a user
	user := entities.User{
		ID:            uuid.Must(uuid.NewV7()),
		Organizations: []*entities.Organization{{ID: orgID}},
		Username:      "testuser",
		FullName:      "Test User",
		Email:         "test@example.com",
		Roles:         []string{"user"},
		CreatedAt:     time.Now(),
	}
	password := "password123"

	err := userSvc.CreateUser(ctx, user, password)
	assert.NoError(t, err)

	// 4. Login successfully
	loggedInUser, token, err := userSvc.Login(ctx, "testuser", password)
	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.Equal(t, user.Username, loggedInUser.Username)

	// 5. Login with wrong password
	_, _, err = userSvc.Login(ctx, "testuser", "wrongpassword")
	assert.Error(t, err)

	// 6. Validate token
	validatedUser, err := userSvc.ValidateToken(ctx, token)
	assert.NoError(t, err)
	assert.Equal(t, user.Username, validatedUser.Username)
	assert.Equal(t, user.ID, validatedUser.ID)

	// 7. Validate invalid token
	_, err = userSvc.ValidateToken(ctx, "invalid-token")
	assert.Error(t, err)
}

func TestGroupManagement(t *testing.T) {
	// 1. Setup DB
	db := testutils.SetupTestDB(t)

	// 2. Setup Repo & Service
	repo := repositories.NewRepository(db)
	userSvc := impl.NewUserService(repo, "test-secret")
	groupSvc := impl.NewGroupService(repo, nil)

	ctx := t.Context()
	// A real organization, not an invented id. user_organizations references
	// organizations, so a membership pointing at one that does not exist is
	// refused by the database — which is right, and which the previous schema
	// let through.
	orgID := seedOrganization(t, repo)
	// Inside the tenant, the way a request creates a group. The strict scope
	// refuses a bare context, and the fixture would fail before the behaviour
	// under test had run.
	ctx = entities.WithTenantContext(ctx, entities.TenantContext{TenantID: orgID.String()})

	// 3. Create a group
	group := entities.Group{
		ID:           uuid.Must(uuid.NewV7()),
		Organization: &entities.Organization{ID: orgID},
		Name:         "Developers",
		Description:  "Group for developers",
		CreatedAt:    time.Now(),
	}
	err := groupSvc.CreateGroup(ctx, group)
	assert.NoError(t, err)

	// 4. List groups
	//
	// Read as the organization, which is what the auth interceptor puts on a
	// real request. The group list is tenant-scoped, so a bare context is
	// answered with nothing once the strict scope is on — this used to pass only
	// because the unscoped read fell open.
	groups, err := groupSvc.ListGroups(asTenant(ctx, orgID), orgID)
	assert.NoError(t, err)
	assert.Len(t, groups, 1)
	assert.Equal(t, group.Name, groups[0].Name)

	// 5. Create a user and add to group
	user := entities.User{
		ID:            uuid.Must(uuid.NewV7()),
		Organizations: []*entities.Organization{{ID: orgID}},
		Username:      "devuser",
		CreatedAt:     time.Now(),
	}
	err = userSvc.CreateUser(ctx, user, "password")
	assert.NoError(t, err)

	err = groupSvc.AddMembership(ctx, user.ID, group.ID)
	assert.NoError(t, err)

	// 6. List user groups
	userGroups, err := groupSvc.ListUserGroups(ctx, user.ID)
	assert.NoError(t, err)
	assert.Len(t, userGroups, 1)
	assert.Equal(t, group.Name, userGroups[0].Name)

	// 7. Remove membership
	err = groupSvc.RemoveMembership(ctx, user.ID, group.ID)
	assert.NoError(t, err)

	userGroups, err = groupSvc.ListUserGroups(ctx, user.ID)
	assert.NoError(t, err)
	assert.Len(t, userGroups, 0)
}

// asTenant returns ctx carrying an organization as the active tenant, which is
// what the auth interceptor injects on a real request.
func asTenant(ctx context.Context, organizationID uuid.UUID) context.Context {
	return entities.WithTenantContext(ctx, entities.TenantContext{TenantID: organizationID.String()})
}

// seedOrganization creates a tenant for an account to belong to.
func seedOrganization(t *testing.T, repo repositories.Repository) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if err := repo.Organization().Create(entities.WithSystemContext(t.Context()), models.OrganizationModel{
		Base: models.Base{ID: models.FromUUID(id)}, Name: "Auth Org " + id.String()[:8],
	}); err != nil {
		t.Fatalf("seed the organization: %v", err)
	}
	return id
}
