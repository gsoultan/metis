package impl

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// recoveryCodeCount is how many recovery codes an account is given.
	recoveryCodeCount = 10
	// recoveryCodeHalf is the length of each half of "xxxxx-xxxxx".
	recoveryCodeHalf = 5
	// recoveryAlphabet leaves out 0, 1, l and o, which are read as each
	// other. 32 symbols: ten of them are fifty bits.
	recoveryAlphabet = "23456789abcdefghijkmnpqrstuvwxyz"
)

// newRecoveryCodes returns fresh recovery codes, as shown to their owner, and
// the hashes that are kept of them.
func newRecoveryCodes() (codes, hashes []string, err error) {
	codes = make([]string, 0, recoveryCodeCount)
	hashes = make([]string, 0, recoveryCodeCount)
	for range recoveryCodeCount {
		code, err := newRecoveryCode()
		if err != nil {
			return nil, nil, err
		}
		codes = append(codes, code)
		hashes = append(hashes, hashRecoveryCode(code))
	}
	return codes, hashes, nil
}

func newRecoveryCode() (string, error) {
	raw := make([]byte, 2*recoveryCodeHalf)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("could not draw a recovery code: %w", err)
	}
	var b strings.Builder
	for i, v := range raw {
		if i == recoveryCodeHalf {
			b.WriteByte('-')
		}
		// 256 is a multiple of 32, so every symbol is equally likely.
		b.WriteByte(recoveryAlphabet[int(v)%len(recoveryAlphabet)])
	}
	return b.String(), nil
}

// hashRecoveryCode is what is kept of a code: its SHA-256, after the forms a
// person might type it in — capitals, spaces, no dash — are made one.
//
// A plain hash and not bcrypt: a code carries fifty random bits, which no
// guessing gets through, and a sign-in compares against ten of them.
func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(normaliseRecoveryCode(code)))
	return hex.EncodeToString(sum[:])
}

func normaliseRecoveryCode(code string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '-' || r == ' ':
			return -1
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return r
	}, code)
}

// looksLikeRecoveryCode tells a recovery code from an app's six digits.
func looksLikeRecoveryCode(code string) bool {
	return len(normaliseRecoveryCode(code)) == 2*recoveryCodeHalf
}
