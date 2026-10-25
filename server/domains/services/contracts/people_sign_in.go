package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// PeopleSignInService signs People in, separately from Users.
//
// A person signs in to one project with the username a process names them by
// and the password an administrator set. What they get back is a People token,
// which nothing on the platform side accepts.
type PeopleSignInService interface {
	// SignInPerson checks a person's password and returns them with a token
	// and when it expires. Every refusal that could tell an unknown name from
	// a wrong password reads the same.
	SignInPerson(ctx context.Context, projectID uuid.UUID, username, password string) (entities.Person, string, time.Time, error)

	// AuthenticatePerson reads a People token and returns who it signs in, as
	// the directory has them now: removed, deactivated or with a changed
	// password, they are refused.
	AuthenticatePerson(ctx context.Context, token string) (entities.Person, error)
}
