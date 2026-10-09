package impl

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// What the server's log says of a change to who holds which role.
const (
	accountCreated                  = "An account was created."
	accountRolesChanged             = "An account's roles were changed."
	accountOrganizationRolesChanged = "An account's roles in an organization were changed."
	accountDeleted                  = "An account was deleted."
	// accountPasswordSetOnServer is said of a password set by
	// --reset-password. It changes no role; it is here because it is the one
	// way into an account that needs no session.
	accountPasswordSetOnServer = "An account's password was set from the server's command line."
)

// What made a change that nobody signed in made, for the line's made_through
// field. A change somebody signed in made carries none: the actor says it.
const (
	madeThroughSetup         = "set-up"
	madeThroughResetPassword = "--reset-password, run on the server"
)

// accountRoles is the roles an account holds: on the account, which it holds
// in every organization it belongs to, and in each organization alone.
type accountRoles struct {
	global         []string
	byOrganization map[uuid.UUID][]string
}

// rolesOf is the roles a stored account holds.
func rolesOf(account models.UserModel) accountRoles {
	held := accountRoles{global: account.Roles, byOrganization: make(map[uuid.UUID][]string, len(account.RolesByOrganization))}
	for organization, roles := range account.RolesByOrganization {
		held.byOrganization[uuid.UUID(organization)] = roles
	}
	return held
}

// holdingIn is the roles with those held in one organization alone replaced.
// The map is copied: what was held before is not changed by saying what is
// held after.
func (r accountRoles) holdingIn(organization uuid.UUID, roles []string) accountRoles {
	held := accountRoles{global: r.global, byOrganization: make(map[uuid.UUID][]string, len(r.byOrganization)+1)}
	for other, there := range r.byOrganization {
		held.byOrganization[other] = there
	}
	held.byOrganization[organization] = roles
	return held
}

// written is the roles held in each organization alone as the log writes
// them: by organization id, and only where a role is held.
func (r accountRoles) written() map[string][]string {
	out := make(map[string][]string, len(r.byOrganization))
	for organization, roles := range r.byOrganization {
		if len(roles) > 0 {
			out[organization.String()] = roles
		}
	}
	return out
}

// traceAccountChange says in the server's log that who holds which role was
// changed, and by whom.
//
// Who administers an organization is changed through this service — an
// account is created holding a role, a role is given or taken away, an
// account is deleted — and nothing else records who did it: there is no trail
// an account's changes are written to. A control that rests on two
// administrators being two people can be walked round by an administrator who
// makes a second account, or removes a colleague's role and gives it back; it
// cannot be prevented by the accounts themselves, so what is left is
// evidence, and until an account-level trail exists this line is it.
//
// It names who made the change and whose account it was, by id as well as by
// name — a name can be changed, and reissued — the organization the request
// was for when it was for one, and the roles before and after: on the
// account, and in each organization alone. Nothing else: no password and no
// token, and of a person nothing but the account's username. A username can
// be an email address — an account an identity provider signs in is named by
// the address when the provider gives no other name — and it is written all
// the same: it is what tells one account from another to whoever reads the
// log, and other lines of this service already carry it.
//
// The actor is whoever is signed in. A change made with nobody signed in —
// an account the server creates as its own work — names none, and both
// fields are empty: nothing is invented. Two changes that nobody signed in
// makes are not made through this service's usual paths, and say what made
// them instead (traceUnattended): the installation's first administrator,
// which set-up writes itself, and a password set from the server's command
// line.
//
// It is written after the change has been made, and cannot fail it.
func traceAccountChange(ctx context.Context, what string, target uuid.UUID, name string, before, after accountRoles) {
	actor := signedIn(ctx)
	if actor == nil {
		actor = &entities.User{}
	}
	actorID := ""
	if actor.ID != uuid.Nil {
		actorID = actor.ID.String()
	}
	line := log.Info().Str("actor_id", actorID).Str("actor", actor.Username)
	writeAccountChange(line, entities.ActingOrganization(ctx), what, target, name, before, after)
}

// traceUnattended says in the server's log that an account was changed by
// something nobody was signed in to: set-up, or a command run on the server.
// The line has the shape of every other (traceAccountChange) — the same
// fields, the actor's two empty — and one more, made_through, that says what
// made the change.
//
// organization is the one the change was for, or none.
func traceUnattended(through, what string, organization, target uuid.UUID, name string, before, after accountRoles) {
	line := log.Info().Str("actor_id", "").Str("actor", "").Str("made_through", through)
	writeAccountChange(line, organization, what, target, name, before, after)
}

// writeAccountChange finishes and writes a line about a change to an account:
// whose it was, the organization it was for when it was for one, and the
// roles before and after.
func writeAccountChange(line *zerolog.Event, organization uuid.UUID, what string, target uuid.UUID, name string, before, after accountRoles) {
	line = line.Str("target_id", target.String()).Str("target", name)
	if organization != uuid.Nil {
		line = line.Str("organization", organization.String())
	}
	line.Strs("roles_before", before.global).Strs("roles_after", after.global).
		Interface("organization_roles_before", before.written()).
		Interface("organization_roles_after", after.written()).
		Msg(what)
}
