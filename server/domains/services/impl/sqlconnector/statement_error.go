package sqlconnector

import "fmt"

// StatementError is a lookup's query refused before it reached the database.
//
// Its message is written for the person who wrote the query, because it lands
// in an incident they are going to read: what was found, where, and what to do
// instead. It never repeats the query itself, which may hold a literal someone
// would rather not see copied into an incident.
type StatementError struct {
	// Reason says what was refused and why, in plain words.
	Reason string
	// Position is the 1-based character the problem starts at, or 0 when it is
	// about the query as a whole.
	Position int
}

func (e *StatementError) Error() string {
	if e.Position > 0 {
		return fmt.Sprintf("the lookup's query was refused at character %d: %s", e.Position, e.Reason)
	}
	return "the lookup's query was refused: " + e.Reason
}

// refused builds a StatementError at a byte offset, or about the whole query
// when offset is negative.
func refused(offset int, format string, args ...any) *StatementError {
	position := 0
	if offset >= 0 {
		position = offset + 1
	}
	return &StatementError{Reason: fmt.Sprintf(format, args...), Position: position}
}
