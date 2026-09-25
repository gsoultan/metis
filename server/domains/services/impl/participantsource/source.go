// Package participantsource fetches a participant directory from wherever it
// lives: a file somebody uploaded, an HTTP endpoint, or a SQL query.
//
// Fetching is all these differ in. What a participant is, which rows are
// usable, and what to do about the ones that are not, is decided once in
// userimport and shared — so an address that is refused in a CSV is refused
// from an API, for the same reason, in the same words.
package participantsource

import (
	"context"

	"github.com/gsoultan/metis/server/domains/logic/userimport"
)

// Kind names where a directory comes from.
type Kind string

const (
	// KindCSV is a file somebody uploaded. It has no Source: there is nothing
	// to fetch, because the bytes arrived with the request.
	KindCSV Kind = "csv"
	// KindHTTP is a JSON endpoint.
	KindHTTP Kind = "http"
	// KindPostgres is a query against another database.
	KindPostgres Kind = "postgres"
)

// Source fetches a directory.
//
// It returns a userimport.Result rather than raw bytes, so every source has
// already been through the same validation by the time anything downstream sees
// it — including the per-row problems, which are a normal outcome rather than a
// failure.
type Source interface {
	// Fetch reads the directory. An error means the source could not be read at
	// all; rows that could not be used come back as problems in the result.
	Fetch(ctx context.Context) (userimport.Result, error)
}
