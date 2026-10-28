package model

import "github.com/gsoultan/storm"

// The values a column's NULLs become when a database GORM created is brought
// to this model's shape, which makes them NOT NULL.
//
// Each is what GORM read a NULL in that column as, so the application sees
// the row it always saw: an empty string, false, and for JSON the null its
// decoder turned into an empty list or map. A reference has none: a row whose
// reference is NULL fails the conversion, which then changes nothing.
const (
	blank    storm.RawSQL = "''"
	no       storm.RawSQL = "false"
	jsonNull storm.RawSQL = "'null'::jsonb"
)

// The defaults GORM's tables carried, kept where the model would otherwise
// drop them: a writer that leaves one of these columns out — an older
// replica during an upgrade, a script — is given what it was given before,
// rather than refused.
const (
	zero storm.RawSQL = "0"
	yes  storm.RawSQL = "true"
)
