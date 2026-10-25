package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// PersonSignInRepository reads what signing a person in needs from the
// project directory: their password hash, and when their credentials last
// changed.
//
// Apart from WorkflowUserRepository so that the hash is reachable from the
// sign-in and from nothing else that lists or imports people.
type PersonSignInRepository interface {
	// GetForSignIn returns a live participant of the project by username, with
	// their password hash, empty when none is set. apierr.ErrNotFound when the
	// project has nobody of that name.
	GetForSignIn(ctx context.Context, projectID uuid.UUID, username string) (entities.WorkflowUser, string, error)

	// GetSession returns a live participant by id, with the moment their
	// credentials last changed (zero when never). A token issued before it no
	// longer signs them in. apierr.ErrNotFound when there is nobody live with
	// that id.
	GetSession(ctx context.Context, id uuid.UUID) (entities.WorkflowUser, time.Time, error)
}

// PeopleDirectoryRepository reads the People directory as task work needs
// it: which groups a person is in, which decides the pool tasks they may take,
// and who a task names, which decides who is sent a link to it.
//
// Never a password hash: that is PersonSignInRepository's alone.
type PeopleDirectoryRepository interface {
	// GroupsOf returns the live groups a participant belongs to.
	GroupsOf(ctx context.Context, participantID uuid.UUID) ([]entities.WorkflowGroup, error)

	// Named returns the live participant of the project a task names by
	// username. apierr.ErrNotFound when the project has nobody of that name.
	Named(ctx context.Context, projectID uuid.UUID, username string) (entities.WorkflowUser, error)

	// Participant returns a live participant by id. apierr.ErrNotFound when
	// there is nobody live with that id.
	Participant(ctx context.Context, id uuid.UUID) (entities.WorkflowUser, error)

	// MembersOf returns the live members of the project's group a task names,
	// by its name or its id. None when the project has no such group.
	MembersOf(ctx context.Context, projectID uuid.UUID, group string) ([]entities.WorkflowUser, error)
}
