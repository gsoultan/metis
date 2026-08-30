package crypto_test

import (
	"errors"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/crypto"
)

func TestDeriveKey(t *testing.T) {
	key := crypto.DeriveKey("test-passphrase")
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d bytes", len(key))
	}

	// Same passphrase should produce the same key
	key2 := crypto.DeriveKey("test-passphrase")
	if string(key) != string(key2) {
		t.Fatal("same passphrase should produce identical keys")
	}

	// Different passphrase should produce different key
	key3 := crypto.DeriveKey("different-passphrase")
	if string(key) == string(key3) {
		t.Fatal("different passphrases should produce different keys")
	}
}

func TestEncryptWithKey_DecryptWithKey(t *testing.T) {
	key := crypto.DeriveKey("my-secure-encryption-key")

	tests := []struct {
		name      string
		plaintext string
	}{
		{name: "simple string", plaintext: "hello world"},
		{name: "empty string", plaintext: ""},
		{name: "connection string", plaintext: "host=localhost port=5432 user=gobpm password=secret dbname=gobpm"},
		{name: "unicode", plaintext: "日本語テスト 🔐"},
		{name: "long string", plaintext: "sqlserver://admin:SuperS3cretP@ssw0rd!@db-server.example.com:1433?database=production&encrypt=true&trustServerCertificate=false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := crypto.EncryptWithKey(tt.plaintext, key)
			if err != nil {
				t.Fatalf("encryption failed: %v", err)
			}

			if encrypted == tt.plaintext && tt.plaintext != "" {
				t.Fatal("encrypted text should differ from plaintext")
			}

			decrypted, err := crypto.DecryptWithKey(encrypted, key)
			if err != nil {
				t.Fatalf("decryption failed: %v", err)
			}

			if decrypted != tt.plaintext {
				t.Fatalf("expected %q, got %q", tt.plaintext, decrypted)
			}
		})
	}
}

func TestDecryptWithKey_WrongKey(t *testing.T) {
	key1 := crypto.DeriveKey("key-one")
	key2 := crypto.DeriveKey("key-two")

	encrypted, err := crypto.EncryptWithKey("secret data", key1)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	_, err = crypto.DecryptWithKey(encrypted, key2)
	if err == nil {
		t.Fatal("decryption with wrong key should fail")
	}
}

func TestDecryptWithKey_InvalidInput(t *testing.T) {
	key := crypto.DeriveKey("some-key")

	_, err := crypto.DecryptWithKey("not-valid-base64!!!", key)
	if err == nil {
		t.Fatal("decryption of invalid base64 should fail")
	}

	_, err = crypto.DecryptWithKey("", key)
	if err == nil {
		t.Fatal("decryption of empty string should fail")
	}
}

func TestEncryptDecrypt_GlobalKey(t *testing.T) {
	// crypto ships no default key: Encrypt/Decrypt fail closed until Configure
	// is called. This is deliberate — a working default would mean production
	// deployments encrypting with a key published in the repository.
	if err := crypto.Configure("global-key-test-passphrase"); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	plaintext := "test with global key"

	encrypted, err := crypto.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	decrypted, err := crypto.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestEncryptWithKey_ProducesDifferentCiphertexts(t *testing.T) {
	key := crypto.DeriveKey("determinism-test-key")
	plaintext := "same input"

	encrypted1, err := crypto.EncryptWithKey(plaintext, key)
	if err != nil {
		t.Fatalf("first encryption failed: %v", err)
	}

	encrypted2, err := crypto.EncryptWithKey(plaintext, key)
	if err != nil {
		t.Fatalf("second encryption failed: %v", err)
	}

	// AES-GCM uses random nonces, so same plaintext should produce different ciphertexts
	if encrypted1 == encrypted2 {
		t.Fatal("two encryptions of the same plaintext should produce different ciphertexts")
	}
}

func TestEncrypt_FailsClosedWithoutConfiguredKey(t *testing.T) {
	crypto.ResetForTest()

	if _, err := crypto.Encrypt("some data"); !errors.Is(err, crypto.ErrKeyNotConfigured) {
		t.Fatalf("Encrypt without a configured key: got %v, want ErrKeyNotConfigured", err)
	}
	if _, err := crypto.Decrypt("gcm1:whatever"); !errors.Is(err, crypto.ErrKeyNotConfigured) {
		t.Fatalf("Decrypt without a configured key: got %v, want ErrKeyNotConfigured", err)
	}
}

func TestEncrypt_OutputIsTaggedAndRecognisable(t *testing.T) {
	if err := crypto.Configure("tagging-test-passphrase"); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	enc, err := crypto.Encrypt("payload")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if !crypto.IsCiphertext(enc) {
		t.Fatalf("Encrypt output %q is not recognised as ciphertext", enc)
	}
	// Legacy rows written before the scheme prefix existed must be
	// distinguishable so callers can apply an explicit migration policy rather
	// than silently treating ciphertext as cleartext.
	if crypto.IsCiphertext(`{"plain":"json"}`) {
		t.Fatal("cleartext JSON was misidentified as ciphertext")
	}
}
