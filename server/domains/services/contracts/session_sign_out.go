package contracts

import "context"

// SessionSignOut is an account's owner ending their sessions.
type SessionSignOut interface {
	// SignOut refuses, from now on, every token the caller's account was
	// issued up to this moment: the one that sent the request and those on
	// the account's other devices. Signing out used to forget the token in the
	// browser and nothing else, so a copy of it kept working for the rest of
	// its 24 hours. An account an identity provider signs in is the
	// provider's to sign out, and is refused.
	SignOut(ctx context.Context) error
}
