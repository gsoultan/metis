package task

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gsoultan/metis/internal/pkg/apierr"
)

// DueDateField is a due date as an edit carries it: not mentioned, removed, or
// set.
//
// The three are different instructions and a *time.Time has room for two. An
// edit that did not mention the due date and one that removed it both decoded
// to nil, so the first was applied as the second.
type DueDateField struct {
	// Present: the request had a due_date key at all.
	Present bool
	// Value is the due date to set; nil with Present means remove it.
	Value *time.Time
}

// UnmarshalJSON is called only when the key is there, null included.
//
// Empty text removes the due date, as null does: the inbox holds a task's due
// date as text, "" when it has none, and sends that back with every save.
func (f *DueDateField) UnmarshalJSON(data []byte) error {
	f.Present, f.Value = true, nil
	text := strings.TrimSpace(string(data))
	if text == "null" || text == `""` {
		return nil
	}
	var at time.Time
	if err := json.Unmarshal(data, &at); err != nil {
		return apierr.Invalidf("due_date %s is not a date and time the server can read; send RFC 3339, "+
			"such as \"2026-10-01T17:00:00Z\", or null to remove the due date", text)
	}
	f.Value = &at
	return nil
}
