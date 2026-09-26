package crypto_test

import (
	"errors"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/crypto"
)

const (
	oldKey   = "the-key-that-leaked-9f2c7a41e0b3d865"
	newKey   = "the-key-that-replaces-it-4be81d07c2f9a653"
	thirdKey = "a-key-nobody-configured-2e5d90c1a7f8b346"
)

func sealUnder(t *testing.T, passphrase, plain string) string {
	t.Helper()
	if err := crypto.Configure(passphrase); err != nil {
		t.Fatalf("configure: %v", err)
	}
	sealed, err := crypto.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	return sealed
}

// There was one key, used both to write and to read, so changing it made
// everything sealed so far unreadable — a key that had leaked could never be
// retired. The key it replaces is kept for reading only, while the data is
// sealed again under the new one.
func TestAValueSealedUnderThePreviousKeyStillReads(t *testing.T) {
	t.Cleanup(crypto.ResetForTest)
	sealed := sealUnder(t, oldKey, "the order is for 40,000")

	if err := crypto.Configure(newKey); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if _, err := crypto.Decrypt(sealed); err == nil {
		t.Fatal("the new key alone opened a value sealed under the old one")
	}
	if err := crypto.ConfigurePrevious(oldKey); err != nil {
		t.Fatalf("configure the previous key: %v", err)
	}
	plain, err := crypto.Decrypt(sealed)
	if err != nil || plain != "the order is for 40,000" {
		t.Fatalf("with the previous key installed: %q, %v", plain, err)
	}

	// Writing uses the new key only: nothing new is sealed under the one
	// being retired.
	fresh, err := crypto.Encrypt("written after the rotation")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	crypto.ResetForTest()
	if err := crypto.Configure(newKey); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if plain, err := crypto.Decrypt(fresh); err != nil || plain != "written after the rotation" {
		t.Fatalf("a value written after the rotation needs the old key: %q, %v", plain, err)
	}
}

func TestResealMovesAValueToTheCurrentKey(t *testing.T) {
	t.Cleanup(crypto.ResetForTest)
	sealed := sealUnder(t, oldKey, "the variables")
	if err := crypto.Configure(newKey); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if err := crypto.ConfigurePrevious(oldKey); err != nil {
		t.Fatalf("configure the previous key: %v", err)
	}

	resealed, changed, err := crypto.Reseal(sealed)
	if err != nil || !changed {
		t.Fatalf("reseal: changed=%v err=%v", changed, err)
	}
	current, err := crypto.Encrypt("already current")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, changed, err := crypto.Reseal(current); err != nil || changed {
		t.Fatalf("a value already under the current key was resealed: changed=%v err=%v", changed, err)
	}
	if _, changed, err := crypto.Reseal("not sealed at all"); err != nil || changed {
		t.Fatalf("a plain value was touched: changed=%v err=%v", changed, err)
	}

	// The old key can go now.
	crypto.ResetForTest()
	if err := crypto.Configure(newKey); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if plain, err := crypto.Decrypt(resealed); err != nil || plain != "the variables" {
		t.Fatalf("the resealed value still needs the old key: %q, %v", plain, err)
	}
}

// A value neither key opens is data this installation can no longer read.
// Saying so is the point: reported as "nothing to do", it would be found
// only when somebody opens the instance.
func TestAValueNoConfiguredKeyOpensIsReported(t *testing.T) {
	t.Cleanup(crypto.ResetForTest)
	sealed := sealUnder(t, thirdKey, "sealed by somebody else")
	if err := crypto.Configure(newKey); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if err := crypto.ConfigurePrevious(oldKey); err != nil {
		t.Fatalf("configure the previous key: %v", err)
	}
	if _, _, err := crypto.Reseal(sealed); !errors.Is(err, crypto.ErrUnreadable) {
		t.Fatalf("reseal: %v, want ErrUnreadable", err)
	}
	if _, err := crypto.Decrypt(sealed); err == nil {
		t.Fatal("decrypt opened a value no configured key sealed")
	}
}

// The connection string in config.yaml is sealed under the same key, with an
// explicit key rather than the process-wide one. After a rotation the server
// has to be able to read it to reach its database at all.
func TestTheConfigConnectionStringReadsUnderThePreviousKey(t *testing.T) {
	t.Cleanup(crypto.ResetForTest)
	sealed, err := crypto.EncryptWithKey("postgres://metis@db/metis", crypto.DeriveKey(oldKey))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := crypto.ConfigurePrevious(oldKey); err != nil {
		t.Fatalf("configure the previous key: %v", err)
	}
	plain, err := crypto.DecryptWithKeyOrPrevious(sealed, crypto.DeriveKey(newKey))
	if err != nil || plain != "postgres://metis@db/metis" {
		t.Fatalf("got %q, %v", plain, err)
	}
}
