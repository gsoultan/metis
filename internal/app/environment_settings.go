package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"sync"

	"github.com/gsoultan/metis/server/repositories/models"
)

// environmentSettings is what an environment is served with, as far as
// deciding whether it has to be started again goes: the port it listens on,
// and a digest of the connection its database is opened with.
//
// A digest rather than the connection, because the connection carries the
// database password, and a running environment is compared against its row at
// every check for as long as it runs. The row is read, compared and dropped;
// only the digest stays.
type environmentSettings struct {
	port       int
	connection [sha256.Size]byte
}

// settingsDigestKey keys the digests.
//
// Keyed, with a secret made up when the process starts and never written
// anywhere: a bare hash of a connection string is something the password can
// be guessed against offline by anybody who reads it out of the process, and a
// keyed one is good for nothing but the comparison it exists for.
var settingsDigestKey = sync.OnceValue(func() []byte {
	key := make([]byte, sha256.Size)
	// crypto/rand does not fail on any platform Go supports; it crashes the
	// program instead of returning a short read.
	_, _ = rand.Read(key)
	return key
})

// settingsOf digests what row would be served with. It fails for a row that
// could not be opened at all — one that names no database.
func settingsOf(row models.EnvironmentModel) (environmentSettings, error) {
	dsn, err := environmentDSN(row)
	if err != nil {
		return environmentSettings{}, err
	}
	mac := hmac.New(sha256.New, settingsDigestKey())
	mac.Write([]byte(row.Driver))
	mac.Write([]byte{0})
	mac.Write([]byte(dsn))
	settings := environmentSettings{port: row.Port}
	copy(settings.connection[:], mac.Sum(nil))
	return settings, nil
}
