package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// fingerprintLabel separates a fingerprint's key from the sealing key, so the
// one is never the other.
const fingerprintLabel = "metis fingerprint v1"

// Fingerprint names data by its content, under the current key: equal data
// has an equal fingerprint while the key stays, and one cannot be told from
// the data without the key. A plain hash would let anybody who can list the
// names test a guess at what one holds.
//
// Not stable across a key change. A caller that stores by fingerprint finds
// what it stored under an earlier key by the name it recorded then.
func Fingerprint(data []byte) (string, error) {
	k, err := activeKey()
	if err != nil {
		return "", err
	}
	derive := hmac.New(sha256.New, k)
	derive.Write([]byte(fingerprintLabel))
	mac := hmac.New(sha256.New, derive.Sum(nil))
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil)), nil
}
