package userimport

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Mapping says which of a source's fields is which of Metis's, keyed by
// Metis's name: {"username": "login", "display_name": "profile.full_name"}.
//
// A directory nobody here controls names its fields its own way, and asking
// somebody to alias every column in SQL or put a proxy in front of their API
// is asking them to change their system to suit ours. A field the mapping does
// not name is read under Metis's own name, so an empty mapping is the shared
// vocabulary exactly as before.
type Mapping map[string]string

// maxSourceFieldName bounds a mapped name. A nested JSON path is a few
// segments; anything longer is not a field name.
const maxSourceFieldName = 128

// Validate refuses a mapping that names a Metis field that does not exist, or
// maps one to nothing.
func (m Mapping) Validate() error {
	for metis, source := range m {
		if !knownColumns[metis] {
			return fmt.Errorf("%q is not a field Metis imports; use %s", metis, strings.Join(Fields(), ", "))
		}
		source = strings.TrimSpace(source)
		if source == "" {
			return fmt.Errorf("the mapping for %q names no field", metis)
		}
		if len(source) > maxSourceFieldName {
			return fmt.Errorf("the field mapped to %q is longer than %d characters", metis, maxSourceFieldName)
		}
	}
	return nil
}

// SourceFor is the source field a Metis field is read from, lower-cased the
// way every source's names are.
func (m Mapping) SourceFor(metis string) string {
	if source := strings.TrimSpace(m[metis]); source != "" {
		return strings.ToLower(source)
	}
	return metis
}

// Apply picks Metis's fields out of everything a source returned for one
// person. What is not mapped is dropped here, and never stored.
func (m Mapping) Apply(raw map[string]string) map[string]string {
	fields := make(map[string]string, len(knownColumns))
	for metis := range knownColumns {
		if value, ok := raw[m.SourceFor(metis)]; ok {
			fields[metis] = value
		}
	}
	return fields
}

// Fields are the names Metis imports, in the order a person reads them.
func Fields() []string {
	order := []string{"external_id", "username", "display_name", "email", "groups", "active"}
	extra := []string{}
	for name := range knownColumns {
		if !slices.Contains(order, name) {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	return append(order, extra...)
}

// Bounds on what a check reports about a source's own fields.
const (
	maxReportedFields = 256
	maxSampleValue    = 200
)

// FromRawRecords maps records read under the source's own names onto Metis's,
// and validates them. The source's names and its first record are kept on the
// result for whoever is matching columns.
func FromRawRecords(raw []Record, mapping Mapping) Result {
	names := map[string]struct{}{}
	mapped := make([]Record, 0, len(raw))
	for _, record := range raw {
		for name := range record.Fields {
			if len(names) < maxReportedFields {
				names[name] = struct{}{}
			}
		}
		mapped = append(mapped, Record{Line: record.Line, Fields: mapping.Apply(record.Fields)})
	}
	result := FromRecords(mapped)
	result.Fields = make([]string, 0, len(names))
	for name := range names {
		result.Fields = append(result.Fields, name)
	}
	sort.Strings(result.Fields)
	if len(raw) > 0 {
		result.Sample = make(map[string]string, len(raw[0].Fields))
		for name, value := range raw[0].Fields {
			if runes := []rune(value); len(runes) > maxSampleValue {
				value = string(runes[:maxSampleValue]) + "…"
			}
			result.Sample[name] = value
		}
	}
	return result
}
