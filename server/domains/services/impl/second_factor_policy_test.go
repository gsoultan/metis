package impl

import (
	"testing"
	"time"
)

// Password-only sign-in is a development convenience, so it takes two
// settings to switch it on, and anything unclear leaves the code required.
func TestTheSecondFactorIsRequiredUnlessADevelopmentStackSaysOtherwise(t *testing.T) {
	cases := []struct {
		name, policy, devMode string
		want                  SecondFactorPolicy
		says                  bool
	}{
		{name: "unset", want: SecondFactorRequired},
		{name: "required", policy: "required", want: SecondFactorRequired},
		{name: "optional on a development stack", policy: "optional", devMode: "true", want: SecondFactorOptional, says: true},
		{name: "optional elsewhere", policy: "optional", want: SecondFactorRequired, says: true},
		{name: "optional with dev mode off", policy: "optional", devMode: "false", want: SecondFactorRequired, says: true},
		{name: "a typo", policy: "of", devMode: "true", want: SecondFactorRequired, says: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(EnvTwoFactor, c.policy)
			t.Setenv(EnvDevMode, c.devMode)
			got, problem := ReadSecondFactorPolicy()
			if got != c.want {
				t.Errorf("policy %v, want %v", got, c.want)
			}
			if (problem != "") != c.says {
				t.Errorf("startup log line %q; want one: %v", problem, c.says)
			}
		})
	}
}

// A challenge and a session are signed with different keys, so neither is
// ever taken for the other.
func TestAChallengeIsNeverASession(t *testing.T) {
	s := &userService{jwtSecret: []byte("secret")}
	challenge, _, err := s.mintChallenge([16]byte{1}, "code", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateToken(t.Context(), challenge); err == nil {
		t.Fatal("a challenge was taken as a session")
	}
	if _, _, err := s.readChallenge(challenge, "enrol"); err == nil {
		t.Fatal("a challenge for one step was taken for another")
	}
	if _, _, err := s.readChallenge(challenge, "code"); err != nil {
		t.Fatalf("a challenge was refused for its own step: %v", err)
	}
}
