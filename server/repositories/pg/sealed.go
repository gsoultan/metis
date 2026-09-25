package pg

import (
	"encoding/json"
	"fmt"

	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/storm/runtime"
)

// Process variables are encrypted at rest — on the instance and on the task —
// and the engine keeps copies of them: the variable history, the audit trail,
// a queued job's payload, an external task's variables, a compensation's
// variables. Those copies were plain jsonb, so every value the instance column
// protected could be read from five other tables. They are sealed the same
// way now.
//
// The ciphertext is stored as a JSON string, so each column keeps its type and
// a row written before this — a plain object — still reads. Nothing queries
// inside these columns in SQL; every reader decodes them here.

// sealedJSONOf encrypts a map for a jsonb column that holds business data.
// Like jsonOf, an absent map is a NULL column. Without a key it refuses rather
// than writing the plaintext.
func sealedJSONOf(m map[string]any) (runtime.JSON, error) {
	if len(m) == 0 {
		return nil, nil
	}
	plain, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	sealed, err := crypto.Encrypt(string(plain))
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}
	return json.Marshal(sealed)
}

// sealedMapOf decodes a column sealedJSONOf wrote, or one written before it
// existed.
func sealedMapOf(raw runtime.JSON) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var m map[string]any
	if err := unseal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// unseal decodes raw into dst, decrypting it first if it is sealed. A value
// with the ciphertext prefix must decrypt: handing ciphertext to a caller as
// though it were business data is the failure EncryptedMap refuses too.
func unseal(raw runtime.JSON, dst any) error {
	var sealed string
	if err := json.Unmarshal(raw, &sealed); err == nil && crypto.IsCiphertext(sealed) {
		plain, err := crypto.Decrypt(sealed)
		if err != nil {
			return fmt.Errorf("decrypt: %w", err)
		}
		return json.Unmarshal([]byte(plain), dst)
	}
	return json.Unmarshal(raw, dst)
}

// sealedText encrypts a text column that carries business data — a broadcast
// event, which holds the variables of the process event it announces.
func sealedText(plain string) (string, error) {
	sealed, err := crypto.Encrypt(plain)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}
	return sealed, nil
}

// openedText reverses sealedText, and passes a value written before sealing
// existed through as it was — the policy EncryptedMap applies to its column.
func openedText(stored string) (string, error) {
	if !crypto.IsCiphertext(stored) {
		return stored, nil
	}
	plain, err := crypto.Decrypt(stored)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return plain, nil
}
