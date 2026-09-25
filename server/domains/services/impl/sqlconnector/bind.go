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

// maxListValues bounds how many values one list parameter may expand to. A list
// comes from process data, so its length is somebody else's choice, and each
// value is one more placeholder the server has to parse.
const maxListValues = 1000

// binding is what bind has handed the driver so far.
type binding struct {
	style    placeholderStyle
	args     []any
	ordinals map[string][]int
}

// bind replaces each :name in a validated query with the server's placeholder
// and lists the values in the order the driver wants them.
//
// The query is read with the same lexer that validated it, so a :name inside a
// string literal is text, not a parameter. Values are only ever handed to the
// driver as parameters; nothing a step supplies is written into the query. A
// list value becomes one placeholder per element — for WHERE id IN (:ids) —
// still bound, never spliced.
//
// A trailing semicolon is dropped: validation allows one, and not every
// server's prepared statements do.
func bind(statement string, params map[string]any, d dialect) (string, []any, error) {
	tokens, err := lex(statement, d.lexModes()[0])
	if err != nil {
		return "", nil, err
	}
	b := &binding{style: d.placeholders(), ordinals: map[string][]int{}}
	var query strings.Builder
	written := 0
	for _, t := range tokens {
		if t.kind != tokenSeparator && t.kind != tokenParam {
			continue
		}
		query.WriteString(statement[written:t.start])
		written = t.end
		if t.kind == tokenSeparator {
			continue
		}
		values, err := paramValues(t, params)
		if err != nil {
			return "", nil, err
		}
		query.WriteString(b.placeholdersFor(t.text, values))
	}
	query.WriteString(statement[written:])
	return query.String(), b.args, nil
}

// placeholdersFor binds one occurrence of a parameter. A server that can name a
// parameter twice reuses what the first occurrence bound; MySQL's ? cannot be
// reused, so each occurrence binds its values again.
func (b *binding) placeholdersFor(name string, values []any) string {
	ordinals, known := b.ordinals[name]
	if !known || !b.style.reuse {
		ordinals = make([]int, len(values))
		for i, value := range values {
			ordinals[i] = len(b.args) + 1
			b.args = append(b.args, b.style.arg(ordinals[i], value))
		}
		b.ordinals[name] = ordinals
	}
	placeholders := make([]string, len(ordinals))
	for i, ordinal := range ordinals {
		placeholders[i] = b.style.format(ordinal)
	}
	return strings.Join(placeholders, ", ")
}

// paramValues is the value a step gave a parameter, as the one or more values
// it binds to.
//
// A parameter the step gave no value is refused rather than bound as NULL: a
// lookup on nothing finds nothing, and "customer not found" is the wrong answer
// to "the step forgot to say which customer". An empty list is refused for the
// same reason, and because IN () is not SQL on any of these servers.
func paramValues(t token, params map[string]any) ([]any, error) {
	value, supplied := params[t.text]
	if !supplied || value == nil {
		return nil, refused(t.start, "the query uses :%s and the step gives it no value", t.text)
	}
	list, isList := value.([]any)
	if !isList {
		one, err := scalarValue(t, value)
		return []any{one}, err
	}
	switch {
	case len(list) == 0:
		return nil, refused(t.start, ":%s is an empty list, which matches nothing; check for that before the lookup", t.text)
	case len(list) > maxListValues:
		return nil, refused(t.start, ":%s has %d values; a lookup takes at most %d", t.text, len(list), maxListValues)
	}
	values := make([]any, len(list))
	for i, item := range list {
		if item == nil {
			return nil, refused(t.start, "value %d of :%s is empty", i+1, t.text)
		}
		one, err := scalarValue(t, item)
		if err != nil {
			return nil, err
		}
		values[i] = one
	}
	return values, nil
}

// scalarValue is one value in a form every driver binds the same way. A set of
// named values is refused because the three servers bind them three ways.
func scalarValue(t token, value any) (any, error) {
	switch v := value.(type) {
	case string, bool, int64, time.Time:
		return v, nil
	case int:
		return int64(v), nil
	case float64:
		return wholeWhereExact(v), nil
	}
	return nil, refused(t.start, ":%s holds a %s; a lookup's value must be text, a number, true or false, a date, or a list of those",
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
