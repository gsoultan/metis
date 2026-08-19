package entities

import "github.com/google/uuid"

// mustID mints the identifier for a new entity.
//
// uuid.NewV7 can only fail when the system's random source is unusable, which
// crypto/rand treats as fatal in its own right. The alternative these
// constructors used was to discard the error and carry on with uuid.Nil — and
// uuid.Nil is not "no id", it is *the same id every time*: every token, every
// subscription would collide on one primary key, and the engine would appear to
// work while quietly overwriting its own state.
//
// A process engine that cannot mint a unique identifier has nothing safe left to
// do, so it stops here rather than at the first corrupted instance.
func mustID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}
