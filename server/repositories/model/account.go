package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// User is a person who can sign in.
//
// The `users` table, modelled as it exists rather than as it should be. The
// platform/workflow split introduced PlatformUser and WorkflowUser as the
// destination, and moving login onto them is a change to who can authenticate —
// which is not the same change as replacing the ORM, and doing both at once
// would mean an authentication bug and a persistence bug being indistinguishable
// from each other.
//
// So this exists so that nothing has to keep a second ORM alive for one table.
// See PlatformUser for where it is going.
type User struct {
	storm.Model

	Username     string
	PasswordHash string

	// TokensValidFrom is when this account's credentials last changed. A token
	// issued before it is refused, which is what makes a password change end
	// every session rather than only the one that changed it.
	TokensValidFrom *time.Time

	FullName    string
	DisplayName string

	// Organization is the free-text name the account was created under. It is
	// not a reference — the memberships below are — and it predates them.
	Organization string
	Email        string

	// Roles is a JSON array of role names. A table would be better and is what
	// PlatformRole is; changing it here would mean migrating the column in the
	// same breath as the ORM.
	Roles storm.JSON

	// IdentityIssuer and IdentitySubject link the account to the identity
	// provider that signs it in: the provider's issuer URL, and the subject it
	// gave the person. The pair is the identity, and nothing else is — an
	// email address is not, since two providers can each vouch for one.
	//
	// Both NULL for a local account, which signs in with the password above.
	IdentityIssuer  *string
	IdentitySubject *string

	DeletedAt *time.Time
}

// The type is named User so storm derives the table and every foreign key
// target from it. t.Name("users") renames the table and does not rename what
// the relations point at, so the join tables referenced a table called
// "accounts" that nothing creates — which PostgreSQL only says at apply time.
func (a *User) Schema(t *storm.Table) {
	t.Col(&a.Username).Size(255)
	t.Col(&a.DeletedAt).Index()
	t.SoftDelete(&a.DeletedAt)
	// Across the deleted rows: the audit trail records who did what, and a
	// username reissued to a different person makes two people indistinguishable
	// in it.
	t.UniqueAcrossDeleted(&a.Username)
	// Over the live rows only: one account per identity, and an administrator
	// deleting a linked account ends that account without locking the person
	// out for ever — their next sign-in is given a new one, under a new
	// username, so nothing of the old account's history is inherited.
	t.Unique(&a.IdentityIssuer, &a.IdentitySubject)
}

// UserOrganization is an account's membership of a tenant.
//
// The join table GORM declared as a many2many. Named explicitly because storm
// derives a name from the two types and this one already exists.
type UserOrganization struct {
	// The field is named User because storm derives the column from it, and the
	// column is user_id. Renaming with Named() detaches it from the relation,
	// which storm refuses: a column that is not a foreign key cannot carry an
	// ON DELETE.
	User         User
	Organization Organization
}

func (a *UserOrganization) Schema(t *storm.Table) {
	t.Name("user_organizations")
	t.PrimaryKey(&a.User, &a.Organization)
	t.Col(&a.User).OnDelete(storm.Cascade)
	t.Col(&a.Organization).OnDelete(storm.Cascade)
}

// UserProject is an account's membership of a project.
type UserProject struct {
	User    User
	Project Project
}

func (a *UserProject) Schema(t *storm.Table) {
	t.Name("user_projects")
	t.PrimaryKey(&a.User, &a.Project)
	t.Col(&a.User).OnDelete(storm.Cascade)
	t.Col(&a.Project).OnDelete(storm.Cascade)
}

// Group is a named set of people inside an organization, with roles.
//
// Distinct from WorkflowGroup, which groups the participants a process assigns
// work to. This one grants what its members may do on the platform.
type Group struct {
	storm.Model

	Organization Organization
	Name         string
	Description  string

	// Roles is a JSON array, for the same reason Account.Roles is.
	Roles storm.JSON

	DeletedAt *time.Time
}

func (g *Group) Schema(t *storm.Table) {
	t.Name("groups")
	t.Col(&g.Name).Size(255)
	t.Col(&g.Organization).OnDelete(storm.Cascade).Index()
	t.Col(&g.DeletedAt).Index()
	t.SoftDelete(&g.DeletedAt)
}

// Membership puts an account in a group.
//
// The pair is the primary key, so adding somebody twice is refused by the
// database rather than by whoever remembered to check first.
type Membership struct {
	User  User
	Group Group
}

func (m *Membership) Schema(t *storm.Table) {
	t.Name("memberships")
	t.PrimaryKey(&m.User, &m.Group)
	t.Col(&m.User).OnDelete(storm.Cascade)
	t.Col(&m.Group).OnDelete(storm.Cascade)
}
