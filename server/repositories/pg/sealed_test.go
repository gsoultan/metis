package pg

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/storm/runtime"
)

func TestASealedColumnRoundTripsAndHidesItsValue(t *testing.T) {
	t.Cleanup(crypto.ResetForTest)
	if err := crypto.Configure("a-passphrase-used-only-by-this-test"); err != nil {
		t.Fatalf("configure a key: %v", err)
	}

	sealed, err := sealedJSONOf(map[string]any{"ssn": "123-45-6789"})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(string(sealed), "123-45-6789") {
		t.Fatalf("the sealed column still carries the value: %s", sealed)
	}
	if !json.Valid(sealed) {
		t.Fatalf("a sealed value must still be valid jsonb: %s", sealed)
	}
	back, err := sealedMapOf(sealed)
	if err != nil || back["ssn"] != "123-45-6789" {
		t.Fatalf("round trip: %v %v", back, err)
	}
}

// A row written before sealing is a plain object, and must still read.
func TestARowWrittenBeforeSealingStillReads(t *testing.T) {
	back, err := sealedMapOf(runtime.JSON(`{"amount": 42}`))
	if err != nil || back["amount"] != float64(42) {
		t.Fatalf("a legacy plaintext row did not read: %v %v", back, err)
	}
}

// Without a key the write is refused rather than downgraded to plaintext.
func TestSealingWithoutAKeyIsRefused(t *testing.T) {
	crypto.ResetForTest()
	t.Cleanup(crypto.ResetForTest)
	if _, err := sealedJSONOf(map[string]any{"ssn": "123-45-6789"}); err == nil {
		t.Fatal("a value was written with no key to encrypt it")
	}
}
