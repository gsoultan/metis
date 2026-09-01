package user_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	service_impl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

func TestUserCRUD(t *testing.T) {
	ctx := t.Context()
	db := testutils.SetupTestDB(t)

	repo := repositories.NewRepository(db)
	orgSvc := service_impl.NewOrganizationService(repo)
	userSvc := service_impl.NewUserService(repo, "test-jwt-secret")

	org, err := orgSvc.CreateOrganization(ctx, "Test Org", "")
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	tests := []struct {
		name     string
		username string
		fullName string
		email    string
		roles    []string
	}{
		{name: "admin user", username: "admin", fullName: "Admin User", email: "admin@test.com", roles: []string{"admin"}},
		{name: "regular user", username: "john", fullName: "John Doe", email: "john@test.com", roles: []string{"user"}},
		{name: "multi-role user", username: "jane", fullName: "Jane Smith", email: "jane@test.com", roles: []string{"admin", "manager"}},
	}

	for _, tc := range tests {
		t.Run("Create_"+tc.name, func(t *testing.T) {
			err := userSvc.CreateUser(ctx, entities.User{
				Organizations: []*entities.Organization{{ID: org.ID}},
				Username:      tc.username,
				FullName:      tc.fullName,
				Email:         tc.email,
				Roles:         tc.roles,
			}, "password123")
			if err != nil {
				t.Fatalf("failed to create user: %v", err)
			}
		})
	}

	t.Run("ListUsers", func(t *testing.T) {
		users, err := userSvc.ListUsers(ctx, org.ID)
		if err != nil {
			t.Fatalf("failed to list users: %v", err)
		}
		if len(users) != 3 {
			t.Fatalf("expected 3 users, got %d", len(users))
		}
	})

	t.Run("GetAndUpdate", func(t *testing.T) {
		users, err := userSvc.ListUsers(ctx, org.ID)
		if err != nil {
			t.Fatalf("failed to list users: %v", err)
		}
		var john entities.User
		for _, u := range users {
			if u.Username == "john" {
				john = u
				break
			}
		}
		if john.ID == uuid.Nil {
			t.Fatal("john not found")
		}

		user, err := userSvc.GetUser(ctx, john.ID)
		if err != nil {
			t.Fatalf("failed to get user: %v", err)
		}
		if user.Username != "john" {
			t.Fatalf("expected username john, got %s", user.Username)
		}

		// UpdateConnectorInstance with full entity to avoid GORM Save overwriting fields with zero values
		john.FullName = "John Updated"
		john.Email = "john.updated@test.com"
		john.Roles = []string{"user", "developer"}
		err = userSvc.UpdateUser(ctx, john)
		if err != nil {
			t.Fatalf("failed to update user: %v", err)
		}
		updated, err := userSvc.GetUser(ctx, john.ID)
		if err != nil {
			t.Fatalf("failed to get updated user: %v", err)
		}
		if updated.FullName != "John Updated" {
			t.Fatalf("expected full name 'John Updated', got %s", updated.FullName)
		}
	})

	t.Run("DeleteUser", func(t *testing.T) {
		users, err := userSvc.ListUsers(ctx, org.ID)
		if err != nil {
			t.Fatalf("failed to list users: %v", err)
		}
		var janeID uuid.UUID
		for _, u := range users {
			if u.Username == "jane" {
				janeID = u.ID
				break
			}
		}

		err = userSvc.DeleteUser(ctx, janeID)
		if err != nil {
			t.Fatalf("failed to delete user: %v", err)
		}
		remaining, err := userSvc.ListUsers(ctx, org.ID)
		if err != nil {
			t.Fatalf("failed to list users after delete: %v", err)
		}
		if len(remaining) != 2 {
			t.Fatalf("expected 2 users after delete, got %d", len(remaining))
		}
	})

	t.Run("Login", func(t *testing.T) {
		_, token, err := userSvc.Login(ctx, "admin", "password123")
		if err != nil {
			t.Fatalf("failed to login: %v", err)
		}
		if token == "" {
			t.Fatal("expected non-empty token")
		}
	})
}

func TestGroupCRUD(t *testing.T) {
	ctx := t.Context()
	db := testutils.SetupTestDB(t)

	repo := repositories.NewRepository(db)
	orgSvc := service_impl.NewOrganizationService(repo)
	groupSvc := service_impl.NewGroupService(repo)

	org, err := orgSvc.CreateOrganization(ctx, "Test Org", "")
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	tests := []struct {
		name        string
		groupName   string
		description string
	}{
		{name: "engineering group", groupName: "Engineering", description: "Engineering team"},
		{name: "sales group", groupName: "Sales", description: "Sales team"},
		{name: "empty description", groupName: "Support", description: ""},
	}

	for _, tc := range tests {
		t.Run("Create_"+tc.name, func(t *testing.T) {
			err := groupSvc.CreateGroup(ctx, entities.Group{
				Organization: &entities.Organization{ID: org.ID},
				Name:         tc.groupName,
				Description:  tc.description,
			})
			if err != nil {
				t.Fatalf("failed to create group: %v", err)
			}
		})
	}

	t.Run("ListGroups", func(t *testing.T) {
		groups, err := groupSvc.ListGroups(ctx, org.ID)
		if err != nil {
			t.Fatalf("failed to list groups: %v", err)
		}
		if len(groups) != 3 {
			t.Fatalf("expected 3 groups, got %d", len(groups))
		}
	})

	t.Run("GetAndUpdateGroup", func(t *testing.T) {
		groups, err := groupSvc.ListGroups(ctx, org.ID)
		if err != nil {
			t.Fatalf("failed to list groups: %v", err)
		}
		if len(groups) == 0 {
			t.Fatal("no groups found")
		}
		groupID := groups[0].ID

		group, err := groupSvc.GetGroup(ctx, groupID)
		if err != nil {
			t.Fatalf("failed to get group: %v", err)
		}
		if group.ID == uuid.Nil {
			t.Fatal("expected non-nil group ID")
		}

		err = groupSvc.UpdateGroup(ctx, entities.Group{
			ID:           groupID,
			Organization: &entities.Organization{ID: org.ID},
			Name:         "Engineering Updated",
			Description:  "Updated description",
		})
		if err != nil {
			t.Fatalf("failed to update group: %v", err)
		}
		updated, err := groupSvc.GetGroup(ctx, groupID)
		if err != nil {
			t.Fatalf("failed to get updated group: %v", err)
		}
		if updated.Name != "Engineering Updated" {
			t.Fatalf("expected name 'Engineering Updated', got %s", updated.Name)
		}
	})

	t.Run("DeleteGroup", func(t *testing.T) {
		groups, err := groupSvc.ListGroups(ctx, org.ID)
		if err != nil {
			t.Fatalf("failed to list groups: %v", err)
		}
		if len(groups) < 2 {
			t.Skip("not enough groups")
		}
		lastID := groups[len(groups)-1].ID

		err = groupSvc.DeleteGroup(ctx, lastID)
		if err != nil {
			t.Fatalf("failed to delete group: %v", err)
		}
		remaining, err := groupSvc.ListGroups(ctx, org.ID)
		if err != nil {
			t.Fatalf("failed to list groups after delete: %v", err)
		}
		if len(remaining) != len(groups)-1 {
			t.Fatalf("expected %d groups after delete, got %d", len(groups)-1, len(remaining))
		}
	})
}

func TestGroupMembership(t *testing.T) {
	ctx := t.Context()
	db := testutils.SetupTestDB(t)

	repo := repositories.NewRepository(db)
	orgSvc := service_impl.NewOrganizationService(repo)
	userSvc := service_impl.NewUserService(repo, "test-jwt-secret")
	groupSvc := service_impl.NewGroupService(repo)

	org, err := orgSvc.CreateOrganization(ctx, "Test Org", "")
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	// CreateAuditEntry users
	for _, u := range []struct {
		username string
		fullName string
		email    string
	}{
		{"alice", "Alice", "alice@test.com"},
		{"bob", "Bob", "bob@test.com"},
	} {
		err := userSvc.CreateUser(ctx, entities.User{
			Organizations: []*entities.Organization{{ID: org.ID}},
			Username:      u.username,
			FullName:      u.fullName,
			Email:         u.email,
			Roles:         []string{"user"},
		}, "pass123")
		if err != nil {
			t.Fatalf("failed to create user %s: %v", u.username, err)
		}
	}

	users, err := userSvc.ListUsers(ctx, org.ID)
	if err != nil {
		t.Fatalf("failed to list users: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
	aliceID := users[0].ID
	bobID := users[1].ID

	// CreateAuditEntry group
	err = groupSvc.CreateGroup(ctx, entities.Group{
		Organization: &entities.Organization{ID: org.ID},
		Name:         "Developers",
		Description:  "Dev team",
	})
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}

	groups, err := groupSvc.ListGroups(ctx, org.ID)
	if err != nil {
		t.Fatalf("failed to list groups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	groupID := groups[0].ID

	t.Run("AddMembership", func(t *testing.T) {
		err := groupSvc.AddMembership(ctx, aliceID, groupID)
		if err != nil {
			t.Fatalf("failed to add alice: %v", err)
		}
		err = groupSvc.AddMembership(ctx, bobID, groupID)
		if err != nil {
			t.Fatalf("failed to add bob: %v", err)
		}
	})

	t.Run("ListGroupMembers", func(t *testing.T) {
		members, err := groupSvc.ListGroupMembers(ctx, groupID)
		if err != nil {
			t.Fatalf("failed to list group members: %v", err)
		}
		if len(members) != 2 {
			t.Fatalf("expected 2 members, got %d", len(members))
		}
	})

	t.Run("ListUserGroups", func(t *testing.T) {
		userGroups, err := groupSvc.ListUserGroups(ctx, aliceID)
		if err != nil {
			t.Fatalf("failed to list user groups: %v", err)
		}
		if len(userGroups) != 1 {
			t.Fatalf("expected 1 group, got %d", len(userGroups))
		}
		if userGroups[0].Name != "Developers" {
			t.Fatalf("expected group name 'Developers', got %s", userGroups[0].Name)
		}
	})

	t.Run("RemoveMembership", func(t *testing.T) {
		err := groupSvc.RemoveMembership(ctx, bobID, groupID)
		if err != nil {
			t.Fatalf("failed to remove membership: %v", err)
		}
		members, err := groupSvc.ListGroupMembers(ctx, groupID)
		if err != nil {
			t.Fatalf("failed to list group members after remove: %v", err)
		}
		if len(members) != 1 {
			t.Fatalf("expected 1 member after remove, got %d", len(members))
		}
	})

	t.Run("DeleteGroupRemovesMemberships", func(t *testing.T) {
		err := groupSvc.DeleteGroup(ctx, groupID)
		if err != nil {
			t.Fatalf("failed to delete group: %v", err)
		}
		userGroups, err := groupSvc.ListUserGroups(ctx, aliceID)
		if err != nil {
			t.Fatalf("failed to list user groups after group delete: %v", err)
		}
		if len(userGroups) != 0 {
			t.Fatalf("expected 0 groups after group delete, got %d", len(userGroups))
		}
	})
}
