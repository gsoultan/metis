// Package secrets refuses a secret that is not one.
//
// ENCRYPTION_KEY and JWT_SECRET were accepted on the single condition that they
// were not empty. Both are load-bearing and both fail quietly when weak: a
// guessable JWT_SECRET is forged into an administrator's token offline, and a
// guessable ENCRYPTION_KEY turns every encrypted variable in a stolen backup
// back into plaintext. Neither produces an error at the time — the system
// behaves exactly as if the secret were strong.
package secrets

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// MinLength is the shortest secret accepted.
//
// Thirty-two, because that is what `openssl rand -hex 16` produces and what the
// documentation has always told operators to run. A threshold that rejected the
// command in our own instructions would be a threshold nobody could satisfy.
const MinLength = 32

// minDistinctChars rejects a secret that is long by repetition.
//
// Thirty-two characters of "a" satisfies any length rule and is worth nothing.
// Eight is comfortably below what any generated secret produces — hex, the
// narrowest alphabet in the documented commands, yields sixteen distinct
// characters in a random 32-character string — so this catches padding without
// rejecting anything real.
const minDistinctChars = 8

// ErrWeak is returned for a secret that would not survive an offline guess.
var ErrWeak = errors.New("secret is too weak")

// publiclyKnown are values that appear in this repository, in its
// documentation, or in everybody else's.
//
// The dangerous one is the evaluation ENCRYPTION_KEY from docker-compose.yml.
// It is thirty-two characters, so it passes every length check — and it is
// published in a public repository, so it protects nothing at all. A compose
// file is the easiest thing in the world to carry from an evaluation into
// production, and it is the one place a length rule cannot help.
var publiclyKnown = map[string]struct{}{
	"0123456789abcdef0123456789abcdef": {}, // docker-compose.yml, evaluation only
	"evaluation-only-change-me":        {}, // docker-compose.yml, evaluation only
	"ci-only-not-a-real-secret":        {}, // the image smoke test in CI
	"changeme":                         {},
	"change-me":                        {},
	"password":                         {},
	"secret":                           {},
	"changemechangemechangemechangeme": {},
}

// Validate reports why a secret is unacceptable, naming the setting so the
// message says which of several to go and fix.
func Validate(name, value string) error {
	trimmed := strings.TrimSpace(value)

	if trimmed == "" {
		return fmt.Errorf("%w: %s is empty", ErrWeak, name)
	}
	if _, known := publiclyKnown[strings.ToLower(trimmed)]; known {
		return fmt.Errorf(
			"%w: %s is a placeholder published in this repository, so it is known to everyone. Generate one: openssl rand -base64 48",
			ErrWeak, name)
	}
	if len(trimmed) < MinLength {
		return fmt.Errorf(
			"%w: %s is %d characters and the minimum is %d. Generate one: openssl rand -base64 48",
			ErrWeak, name, len(trimmed), MinLength)
	}
	if distinct(trimmed) < minDistinctChars {
		return fmt.Errorf(
			"%w: %s is long but repetitive, which is no harder to guess than a short one. Generate one: openssl rand -base64 48",
			ErrWeak, name)
	}
	return nil
}

func distinct(s string) int {
	seen := make(map[rune]struct{}, len(s))
	for _, r := range s {
		seen[r] = struct{}{}
	}
	return len(seen)
}

// EnvAllowWeak names the override that lets a weak secret through.
//
// It exists because refusing outright can be worse than the weakness: a weak
// ENCRYPTION_KEY has no safe remedy, since rotating it does not re-encrypt
// anything, it makes every stored variable unreadable. An operator who meets
// this on an upgrade needs a way to boot while planning a re-encryption.
const EnvAllowWeak = "METIS_ALLOW_WEAK_SECRETS"

// Allowed reports whether a weak secret has been explicitly permitted.
//
// Lives here rather than at each call site so that the boot path and the setup
// wizard cannot disagree about the policy — which they did: validation was
// added to one and not the other, so the wizard would accept a secret the
// server then refused to start with, and an installation could be configured
// successfully and never restart.
func Allowed() bool {
	allowed, err := strconv.ParseBool(os.Getenv(EnvAllowWeak))
	return err == nil && allowed
}
