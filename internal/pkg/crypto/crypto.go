// Package crypto provides AES-256-GCM encryption for data at rest.
//
// The process-variable key is process-global but has no default. Until
// Configure is called, Encrypt and Decrypt return ErrKeyNotConfigured. This is
// deliberate: an earlier version shipped a hardcoded 32-byte literal as the
// default key, so every deployment that did not override it encrypted its
// process variables with a key published in a public repository. A default that
// "works" out of the box is a default that reaches production.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// ciphertextPrefix tags values produced by this package so a stored value can be
// told apart from legacy cleartext without attempting a decrypt. Bump the digit
// when the scheme changes.
const ciphertextPrefix = "gcm1:"

// ErrKeyNotConfigured is returned by Encrypt and Decrypt before Configure has
// been called. It is never a reason to fall back to cleartext.
var ErrKeyNotConfigured = errors.New("crypto: encryption key not configured; set ENCRYPTION_KEY")

var (
	keyMu sync.RWMutex
	key   []byte
)

// Configure derives and installs the process-wide encryption key from a
// passphrase. It must be called once during startup, before any repository
// reads or writes an encrypted column.
func Configure(passphrase string) error {
	if strings.TrimSpace(passphrase) == "" {
		return errors.New("crypto: encryption passphrase must not be empty")
	}
	keyMu.Lock()
	defer keyMu.Unlock()
	key = DeriveKey(passphrase)
	return nil
}

// ResetForTest clears the installed key so a test can assert the fail-closed
// behaviour. It has no production callers.
func ResetForTest() {
	keyMu.Lock()
	defer keyMu.Unlock()
	key = nil
}

// IsConfigured reports whether an encryption key has been installed.
func IsConfigured() bool {
	keyMu.RLock()
	defer keyMu.RUnlock()
	return len(key) > 0
}

func activeKey() ([]byte, error) {
	keyMu.RLock()
	defer keyMu.RUnlock()
	if len(key) == 0 {
		return nil, ErrKeyNotConfigured
	}
	return key, nil
}

// DeriveKey derives a 32-byte AES-256 key from an arbitrary-length passphrase using SHA-256.
// Always call this with a secret loaded from a secure source (environment variable, KMS, etc.).
func DeriveKey(passphrase string) []byte {
	hash := sha256.Sum256([]byte(passphrase))
	return hash[:]
}

// Encrypt encrypts plaintext with the configured key and returns a prefixed,
// base64-encoded ciphertext. It returns ErrKeyNotConfigured when no key is set —
// it never returns the plaintext.
func Encrypt(plaintext string) (string, error) {
	k, err := activeKey()
	if err != nil {
		return "", err
	}
	ciphertext, err := EncryptWithKey(plaintext, k)
	if err != nil {
		return "", err
	}
	return ciphertextPrefix + ciphertext, nil
}

// Decrypt decrypts a value produced by Encrypt.
//
// IsCiphertext should be used to check the value first: Decrypt requires the
// scheme prefix and returns an error for anything else, rather than guessing
// that unrecognised input is cleartext.
func Decrypt(stored string) (string, error) {
	k, err := activeKey()
	if err != nil {
		return "", err
	}
	if !IsCiphertext(stored) {
		return "", errors.New("crypto: value is not in the expected ciphertext format")
	}
	return DecryptWithKey(strings.TrimPrefix(stored, ciphertextPrefix), k)
}

// IsCiphertext reports whether stored was produced by Encrypt. Values written
// before the scheme prefix existed return false, letting callers apply an
// explicit migration policy instead of silently treating them as cleartext.
func IsCiphertext(stored string) bool {
	return strings.HasPrefix(stored, ciphertextPrefix)
}

// EncryptWithKey encrypts plaintext using the provided key (AES-256-GCM).
// The key must be exactly 32 bytes for AES-256.
func EncryptWithKey(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptWithKey decrypts ciphertext using the provided key (AES-256-GCM).
// The key must be exactly 32 bytes for AES-256.
func DecryptWithKey(ciphertextStr string, key []byte) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextStr)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}
