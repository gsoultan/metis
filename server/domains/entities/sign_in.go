package entities

import "time"

// SignInStep is what a password sign-in still needs before it is a session.
type SignInStep string

const (
	// SignInDone is a session: the token is in Token.
	SignInDone SignInStep = ""
	// SignInCode asks for a code from the account's authenticator app, or one
	// of its recovery codes.
	SignInCode SignInStep = "code"
	// SignInEnrol asks the account to set up an authenticator app first.
	SignInEnrol SignInStep = "enrol"
)

// SignIn is how far a password sign-in got.
//
// When Next is SignInDone, Token is the session. Otherwise Challenge is a
// short-lived token that proves the password was right and does nothing else:
// it is good only for the step Next names, until ChallengeExpiresAt.
type SignIn struct {
	User               User
	Token              string
	Next               SignInStep
	Challenge          string
	ChallengeExpiresAt time.Time
}

// SecondFactorEnrolment is what an authenticator app is set up from, shown
// once: the secret, and the otpauth:// link a QR code carries.
type SecondFactorEnrolment struct {
	Secret string `json:"secret"`
	URI    string `json:"otpauth_uri"`
}

// SecondFactorStatus is whether an account signs in with an authenticator
// app, as its owner sees it.
type SecondFactorStatus struct {
	Enrolled          bool       `json:"enrolled"`
	ConfirmedAt       *time.Time `json:"confirmed_at,omitzero"`
	RecoveryCodesLeft int        `json:"recovery_codes_left"`
	Required          bool       `json:"required"`
}
