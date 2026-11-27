package userimport

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// FromJSON reads a participant directory from a JSON document.
//
// The shape is an array of objects whose keys are the same names the CSV
// columns use — `username`, `display_name`, `email`, `groups`, `active`. One
// vocabulary across every source is the point: somebody who has imported a CSV
// already knows what an endpoint has to return, and the validation underneath
// is literally the same code.
//
// An object nested under a key is also accepted, because most APIs wrap their
// results: {"data": [...]} and {"items": [...]} are the two spellings that
// cover nearly everything, and refusing them would mean asking somebody to put
// a transforming proxy in front of their own directory.
func FromJSON(document []byte) (Result, error) { return FromJSONWith(document, nil) }

// FromJSONWith reads a JSON directory whose fields are named as mapping says.
// A nested field is named by its path: "profile.full_name".
func FromJSONWith(document []byte, mapping Mapping) (Result, error) {
	if err := mapping.Validate(); err != nil {
		return Result{}, err
	}
	records, err := recordsFromJSON(document)
	if err != nil {
		return Result{}, err
	}
	return FromRawRecords(records, mapping), nil
}

func recordsFromJSON(document []byte) ([]Record, error) {
	trimmed := strings.TrimSpace(string(document))
	if trimmed == "" {
		return nil, fmt.Errorf("the response was empty")
	}

	// An array is the plain case.
	if strings.HasPrefix(trimmed, "[") {
		var items []map[string]any
		if err := json.Unmarshal(document, &items); err != nil {
			return nil, fmt.Errorf("the response is not a list of participants: %w", err)
		}
		return recordsFrom(items), nil
	}

	if !strings.HasPrefix(trimmed, "{") {
		return nil, fmt.Errorf("the response is neither a list nor an object")
	}

	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(document, &wrapper); err != nil {
		return nil, fmt.Errorf("the response is not valid JSON: %w", err)
	}
	// Named rather than "whichever key holds an array": guessing would pick a
	// different key when an endpoint adds one, and the import would silently
	// start reading something else.
	for _, key := range []string{"data", "items", "results", "users", "participants"} {
		raw, ok := wrapper[key]
		if !ok {
			continue
		}
		var items []map[string]any
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("%q is not a list of participants: %w", key, err)
		}
		return recordsFrom(items), nil
	}
	return nil, fmt.Errorf("the response has no participant list; expected an array, or an object with one under data, items, results, users or participants")
}

func recordsFrom(items []map[string]any) []Record {
	records := make([]Record, 0, len(items))
	for i, item := range items {
		fields := make(map[string]string, len(item))
		flatten(fields, "", item, 0)
		// Numbered from one, and called a line, because that is the word the
		// problem report uses for every other source. "Item 4" and "line 4"
		// being the same number matters more than the word being exact.
		records = append(records, Record{Line: i + 1, Fields: fields})
	}
	return records
}

// Stringify flattens a value into the text the shared validation reads.
//
// JSON and SQL both have types a CSV cell does not, and a directory uses them:
// a boolean for active, a list for groups, sometimes a number for a name that
// looks like one. Converting here rather than teaching the validator about each
// source keeps one set of rules for what a valid participant is.
func Stringify(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		// Whole numbers without a decimal point: an employee id of 1024 read
		// back as "1024" rather than "1024.000000".
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []any:
		// A list of groups arrives as a list, and the shared reader expects the
		// semicolon-separated form the CSV uses.
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := strings.TrimSpace(Stringify(item)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ";")
	default:
		return fmt.Sprint(v)
	}
}

// Bounds on how far one object is read into. A directory's person is a few
// dozen fields a level or two deep; past these it is not a person.
const (
	maxNesting      = 4
	maxFieldsPerRow = 256
)

// flatten names every value in an object by its path, lower-cased:
// {"profile": {"full_name": "Ada"}} is "profile.full_name". A list is a value,
// not a level, so a list of groups stays one field.
func flatten(into map[string]string, prefix string, object map[string]any, depth int) {
	for key, value := range object {
		if len(into) >= maxFieldsPerRow {
			return
		}
		name := strings.ToLower(strings.TrimSpace(key))
		if prefix != "" {
			name = prefix + "." + name
		}
		if nested, ok := value.(map[string]any); ok {
			if depth+1 < maxNesting {
				flatten(into, name, nested, depth+1)
			}
			continue
		}
		into[name] = Stringify(value)
	}
}
