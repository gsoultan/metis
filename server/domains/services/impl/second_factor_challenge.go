package impl

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

const (
	// challengeLifetime is how long the step after a password has.
	challengeLifetime = 5 * time.Minute
	// challengeAudience is what a challenge says it is for.
	challengeAudience = "metis-second-factor"
	// challengeKeyLabel derives the key challenges are signed with from the
	// session key.
	challengeKeyLabel = "metis second factor challenge v1"
	// claimStep names the step a challenge is good for.
	claimStep = "step"
	// claimMethods lists how a session's owner proved who they were, as
	// RFC 8176 names them: "pwd" for a password, "otp" for a code.
	claimMethods   = "amr"
	methodPassword = "pwd"
	methodCode     = "otp"
)

// errChallenge refuses a challenge that is not one, is for another step, or
// has run out. It says no more than that: the sign-in starts again.
var errChallenge = fmt.Errorf("%w: the sign-in has expired or is not valid; sign in again", auth.ErrAuthenticationFailed)

// challengeKey is a key of its own, derived from the session key, so that a
// challenge can never be checked as a session and a session never as a
// challenge: each fails the other's signature.
func challengeKey(sessionKey []byte) []byte {
	mac := hmac.New(sha256.New, sessionKey)
	mac.Write([]byte(challengeKeyLabel))
	return mac.Sum(nil)
}

// mintChallenge proves, for a few minutes, that userID gave the right
// password, and that the next step is step.
func (s *userService) mintChallenge(userID uuid.UUID, step entities.SignInStep, now time.Time) (string, time.Time, error) {
	expires := now.Add(challengeLifetime)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":     userID.String(),
		"aud":     challengeAudience,
		claimStep: string(step),
		"iat":     now.Unix(),
		"exp":     expires.Unix(),
	})
	signed, err := token.SignedString(challengeKey(s.jwtSecret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("could not sign the challenge: %w", err)
	}
	return signed, expires, nil
}

// readChallenge returns the account a challenge is for, if it is a live
// challenge for step, and when it was issued.
func (s *userService) readChallenge(challenge string, step entities.SignInStep) (uuid.UUID, time.Time, error) {
	token, err := jwt.Parse(challenge, func(*jwt.Token) (any, error) {
		return challengeKey(s.jwtSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithAudience(challengeAudience),
		jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !token.Valid {
		return uuid.Nil, time.Time{}, errChallenge
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || claims[claimStep] != string(step) {
		return uuid.Nil, time.Time{}, errChallenge
	}
	subject, err := claims.GetSubject()
	if err != nil {
		return uuid.Nil, time.Time{}, errChallenge
	}
	issued, err := claims.GetIssuedAt()
	if err != nil || issued == nil {
		return uuid.Nil, time.Time{}, errChallenge
	}
	id, err := uuid.Parse(subject)
	if err != nil {
		return uuid.Nil, time.Time{}, errChallenge
	}
	return id, issued.Time, nil
}

// provedWithCode reports whether a session's owner gave a code as well as a
// password.
func provedWithCode(claims jwt.MapClaims) bool {
	methods, ok := claims[claimMethods].([]any)
	if !ok {
		return false
	}
	for _, method := range methods {
		if method == methodCode {
			return true
		}
	}
	return false
}
