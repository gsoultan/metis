package gorms

import (
	"sync"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The open connection for each environment, keyed by environment id.
//
// A package-level registry rather than a value threaded through every
// repository: the repositories are constructed once at boot over a single
// handle, and the thing that varies per request is which database that handle
// should resolve to. Putting the choice here means GetTx can make it — see
// EnvironmentDB — without changing the signature of every method that reads a
// row.
//
// Populated at boot from the environments table and left alone afterwards.
// Reads vastly outnumber writes, so RWMutex rather than a channel.
var (
	environmentMu  sync.RWMutex
	environmentDBs = map[uuid.UUID]*gorm.DB{}
)

// RegisterEnvironmentDB records the open connection for one environment.
//
// Replacing an existing entry is deliberate: an environment whose connection
// details changed is re-opened, and the new handle has to take over. The old
// one is returned so the caller can close it once nothing is using it — closing
// it here would break requests still in flight on it.
func RegisterEnvironmentDB(id uuid.UUID, db *gorm.DB) (replaced *gorm.DB) {
	environmentMu.Lock()
	defer environmentMu.Unlock()
	replaced = environmentDBs[id]
	environmentDBs[id] = db
	return replaced
}

// EnvironmentDB returns the connection for one environment, if it is open.
//
// Not-open is a normal answer rather than an error: an environment can be
// disabled, or its database can have been unreachable at boot. The caller
// decides what that means — for a request bound to that environment it is a
// refusal, and for a background sweep it is one runtime to skip.
func EnvironmentDB(id uuid.UUID) (*gorm.DB, bool) {
	environmentMu.RLock()
	defer environmentMu.RUnlock()
	db, ok := environmentDBs[id]
	return db, ok
}

// ForgetEnvironmentDB drops an environment's connection and hands it back to be
// closed. Used when an environment is disabled or removed.
func ForgetEnvironmentDB(id uuid.UUID) (*gorm.DB, bool) {
	environmentMu.Lock()
	defer environmentMu.Unlock()
	db, ok := environmentDBs[id]
	delete(environmentDBs, id)
	return db, ok
}

// OpenEnvironmentIDs returns every environment with a live connection.
//
// For the boot summary and for the workers that have to run once per runtime.
func OpenEnvironmentIDs() []uuid.UUID {
	environmentMu.RLock()
	defer environmentMu.RUnlock()
	ids := make([]uuid.UUID, 0, len(environmentDBs))
	for id := range environmentDBs {
		ids = append(ids, id)
	}
	return ids
}

// ResetEnvironmentDBs drops every registered connection. For tests, which build
// a fresh registry per case and would otherwise inherit the previous one's.
func ResetEnvironmentDBs() {
	environmentMu.Lock()
	defer environmentMu.Unlock()
	environmentDBs = map[uuid.UUID]*gorm.DB{}
}
