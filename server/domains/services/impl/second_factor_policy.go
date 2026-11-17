package impl

import (
	"strings"

	"github.com/gsoultan/metis/internal/pkg/envvar"
)

// Settings that decide whether a password sign-in asks for a second factor.
const (
	// EnvTwoFactor is "required" (the default) or "optional".
	EnvTwoFactor = "METIS_TWO_FACTOR"
	// EnvDevMode marks a development stack. "optional" is honoured only beside
	// it, so a production server cannot be switched to password-only sign-in
	// by one setting copied from a developer's machine.
	EnvDevMode = "METIS_DEV_MODE"
)

// SecondFactorPolicy is whether every staff password sign-in asks for a code.
type SecondFactorPolicy int

const (
	// SecondFactorOptional asks for a code only from accounts that set up an
	// authenticator app. It is what a facade built without
	// services.WithSecondFactor gets, which is every test's; the server reads
	// its policy with ReadSecondFactorPolicy, whose default is required.
	SecondFactorOptional SecondFactorPolicy = iota
	// SecondFactorRequired asks every account: one without an authenticator
	// app sets one up before it gets a session, and a session minted without
	// a code is refused.
	SecondFactorRequired
)

// ReadSecondFactorPolicy reads the policy from the environment, and a
// sentence for the startup log when what was written is not what applies.
//
// Required unless METIS_TWO_FACTOR is "optional" and METIS_DEV_MODE is "true".
// Anything else asked for is refused in favour of required, never of optional:
// a typo must not switch a control off.
func ReadSecondFactorPolicy() (SecondFactorPolicy, string) {
	asked := strings.ToLower(strings.TrimSpace(envvar.Get(EnvTwoFactor)))
	switch asked {
	case "", "required":
		return SecondFactorRequired, ""
	case "optional":
		if strings.EqualFold(strings.TrimSpace(envvar.Get(EnvDevMode)), "true") {
			return SecondFactorOptional, "Two-factor sign-in is optional: accounts without an authenticator app " +
				"sign in with a password alone. This is for development only; unset METIS_TWO_FACTOR anywhere else."
		}
		return SecondFactorRequired, "METIS_TWO_FACTOR=optional is honoured only beside METIS_DEV_MODE=true, " +
			"so two-factor sign-in stays required."
	default:
		return SecondFactorRequired, "METIS_TWO_FACTOR is \"" + asked + "\", which is neither \"required\" nor " +
			"\"optional\", so two-factor sign-in stays required."
	}
}
