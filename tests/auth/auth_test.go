package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
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
	orgID := uuid.Must(uuid.NewV7())

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
	groupSvc := impl.NewGroupService(repo)

	ctx := t.Context()
	orgID := uuid.Must(uuid.NewV7())

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
	groups, err := groupSvc.ListGroups(ctx, orgID)
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
