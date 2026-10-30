// Package archivestore keeps archived instances' records outside the
// database, when an operator chooses to: in a directory every replica sees,
// or in an S3-compatible bucket.
//
// The database always keeps the archive's index — one row per archived
// instance, what the list is searched by — and, by default, the record too.
// With a store chosen, the record goes to it instead, sealed with a key of its
// own, and the row keeps that key, sealed with ENCRYPTION_KEY, and the
// record's name. Rotating ENCRYPTION_KEY then reseals the rows and never
// rewrites a stored record.
//
// METIS_ARCHIVE_STORE picks where new records go. Every store whose settings
// are present can be read, so records written before a change of store are
// still opened from where they were written.
package archivestore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gsoultan/metis/internal/pkg/envvar"
)

// The stores, as METIS_ARCHIVE_STORE and an archived row name them.
const (
	KindDatabase  = "database"
	KindDirectory = "directory"
	KindS3        = "s3"
)

// EnvStore picks where new archive records are written.
const EnvStore = "METIS_ARCHIVE_STORE"

// ErrNotFound is a record its store does not have.
var ErrNotFound = errors.New("the archive record is not in its store")

// Store keeps archive records by name. A name is made by the archive, never
// by a caller: the project's id and the instance's.
type Store interface {
	// Kind is how the archive's rows name this store.
	Kind() string
	// Put writes a record, replacing one of the same name.
	Put(ctx context.Context, name string, record []byte) error
	// Get reads a record, or answers ErrNotFound.
	Get(ctx context.Context, name string) ([]byte, error)
	// Delete removes a record; one not there is not an error.
	Delete(ctx context.Context, name string) error
	// Where says where the store is, for the log.
	Where() string
}

var (
	mu      sync.RWMutex
	writer  Store // nil: new records are kept in the database
	readers = map[string]Store{}
)

// Configure installs the stores: writeTo receives new records, or nil keeps
// them in the database, and every store in others can be read. writeTo is
// readable too.
func Configure(writeTo Store, others ...Store) {
	mu.Lock()
	defer mu.Unlock()
	writer = writeTo
	readers = map[string]Store{}
	for _, s := range append(others, writeTo) {
		if s != nil {
			readers[s.Kind()] = s
		}
	}
}

// Writer is where new records go; nil keeps them in the database.
func Writer() Store {
	mu.RLock()
	defer mu.RUnlock()
	return writer
}

// Reader is the store of kind, if this server is configured to read it.
func Reader(kind string) (Store, bool) {
	mu.RLock()
	defer mu.RUnlock()
	s, ok := readers[kind]
	return s, ok
}

// ConfigureFromEnv opens every store whose settings are present and picks
// the one METIS_ARCHIVE_STORE names. A store that is named and cannot be
// opened stops the start: running without it would put records back in the
// database and read none already kept in it.
func ConfigureFromEnv(ctx context.Context) error {
	kind := strings.ToLower(strings.TrimSpace(envvar.Get(EnvStore)))
	if kind == "" {
		kind = KindDatabase
	}
	if kind != KindDatabase && kind != KindDirectory && kind != KindS3 {
		return fmt.Errorf("%s is %q; it is one of %s, %s or %s", EnvStore, kind, KindDatabase, KindDirectory, KindS3)
	}
	var opened []Store
	var chosen Store
	for _, open := range []struct {
		kind string
		fn   func(context.Context) (Store, error)
	}{
		{KindDirectory, directoryFromEnv},
		{KindS3, s3FromEnv},
	} {
		s, err := open.fn(ctx)
		if err != nil {
			return err
		}
		if s == nil {
			if kind == open.kind {
				return fmt.Errorf("%s is %s, and its settings are not set", EnvStore, kind)
			}
			continue
		}
		opened = append(opened, s)
		if kind == open.kind {
			chosen = s
		}
	}
	Configure(chosen, opened...)
	return nil
}

// ResetForTest keeps every record in the database again.
func ResetForTest() {
	Configure(nil)
}
