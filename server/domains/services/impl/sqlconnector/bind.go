package sqlconnector

import (
	"math"
	"strings"
	"time"
)

// maxExactInteger is the largest whole number a float64 holds exactly. A
// process variable is JSON, so every number in it is a float64; one this size
// or smaller is bound as an integer, so it matches an integer column the way
// the author meant.
const maxExactInteger = 1 << 53

// bind replaces each :name in a validated query with the server's placeholder
// and lists the values in the order the driver wants them.
//
// The query is read with the same lexer that validated it, so a :name inside a
// string literal is text, not a parameter. Values are only ever handed to the
// driver as parameters; nothing a step supplies is written into the query.
//
// A trailing semicolon is dropped: validation allows one, and not every
// server's prepared statements do.
func bind(statement string, params map[string]any, d dialect) (string, []any, error) {
	tokens, err := lex(statement, d.lexModes()[0])
	if err != nil {
		return "", nil, err
	}
	style := d.placeholders()
	var (
		query    strings.Builder
		args     []any
		ordinals = map[string]int{}
		written  = 0
	)
	for _, t := range tokens {
		if t.kind == tokenSeparator {
			query.WriteString(statement[written:t.start])
			written = t.end
			continue
		}
		if t.kind != tokenParam {
			continue
		}
		value, err := paramValue(t, params)
		if err != nil {
			return "", nil, err
		}
		query.WriteString(statement[written:t.start])
		written = t.end
		ordinal, known := ordinals[t.text]
		if !known || !style.reuse {
			ordinal = len(args) + 1
			ordinals[t.text] = ordinal
			args = append(args, style.arg(ordinal, value))
		}
		query.WriteString(style.format(ordinal))
	}
	query.WriteString(statement[written:])
	return query.String(), args, nil
}

// paramValue is the value a step gave a parameter, in a form every driver
// binds the same way.
//
// A parameter the step gave no value is refused rather than bound as NULL: a
// lookup on nothing finds nothing, and "customer not found" is the wrong answer
// to "the step forgot to say which customer". A list or an object is refused
// because the three servers bind them three different ways.
func paramValue(t token, params map[string]any) (any, error) {
	value, supplied := params[t.text]
	if !supplied || value == nil {
		return nil, refused(t.start, "the query uses :%s and the step gives it no value", t.text)
	}
	switch v := value.(type) {
	case string, bool, int64, time.Time:
		return v, nil
	case int:
		return int64(v), nil
	case float64:
		return wholeWhereExact(v), nil
	}
	return nil, refused(t.start, ":%s is a %s; a lookup's value must be text, a number, true or false, or a date",
		t.text, kindOf(value))
}

// wholeWhereExact binds a whole number as an integer and anything else as it is.
func wholeWhereExact(v float64) any {
	if v == math.Trunc(v) && math.Abs(v) <= maxExactInteger {
		return int64(v)
	}
	return v
}

// kindOf names a value's shape the way a process author thinks of it.
func kindOf(value any) string {
	switch value.(type) {
	case []any:
		return "list"
	case map[string]any:
		return "set of named values"
	case time.Duration:
		return "duration"
	}
	return "value of an unsupported kind"
}
