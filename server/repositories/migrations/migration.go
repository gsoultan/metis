// Package migrations brings a database to the schema this release reads and
// writes, and records how far it has come.
//
// The schema is the storm model's. An empty database is created from it. A
// database the GORM releases made, which recorded versions 1 to 49, is
// converted to it once, as version 50: its NULLs become the values GORM read
// them as, its JSON held as text becomes jsonb, and its indexes and keys are
// renamed to the model's. A database part-way through GORM's versions has to
// finish the climb on the last release that still had them, because what each
// did to its rows cannot be repeated here. Changes after that are Migrations,
// numbered on from 50, and written against storm.
package migrations

import (
	"context"
	"sync"

	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/storm"
	"github.com/gsoultan/storm/runtime"
	"github.com/gsoultan/storm/schema"
)

// GORMVersion is the last version the GORM releases recorded. A database at
// it is converted to the model's shape; one before it is refused.
const GORMVersion = 49

// ConvertedVersion is the database in the model's shape: converted from
// GORMVersion, or created that way.
const ConvertedVersion = 50

// What schema_migrations records for the two ways a database comes to
// ConvertedVersion.
const (
	createdName   = "created from the storm model"
	convertedName = "converted from GORM's tables to the storm model's"
)

// Migration is one ordered, recorded change to the database after
// ConvertedVersion.
//
// Version must be unique and never reused or renumbered once released: it is
// the identity recorded in schema_migrations, and a database that has applied
// version 51 will never look at a different version 51 again.
//
// Run is given the transaction the change is recorded in, so a change and its
// record commit together or not at all.
type Migration struct {
	Version int64
	Name    string
	Run     func(ctx context.Context, ex runtime.Executor) error
}

var target = sync.OnceValues(func() (*schema.Schema, error) {
	return storm.Build(model.All()...)
})

// Target is the schema every database is brought to: the storm model's.
func Target() (*schema.Schema, error) {
	return target()
}

// Steps is every migration after ConvertedVersion, for every database this
// release reaches: the main one, each environment's, and the one the setup
// wizard configures. None yet.
func Steps() []Migration {
	return nil
}
