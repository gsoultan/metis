// Package userimport reads a CSV of workflow participants.
//
// Importing a directory is the normal way a team arrives: somebody exports a
// list from an HR system and expects to paste it in, not to type five hundred
// names into a form. What makes it worth its own package is that almost none of
// the work is reading commas — it is deciding what to do with the rows that are
// wrong, and that decision has to be made once, in a place with tests, rather
// than in the middle of a request handler.
//
// The rule everywhere below: a bad row is reported with its line number and the
// rest of the file still imports. An all-or-nothing import of a five-hundred-row
// file fails on row four hundred and leaves whoever ran it to work out which
// rows had landed — the answer being none, which they then have to trust.
package userimport

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"sort"
	"strings"
)

// MaxRows bounds a single import.
//
// The file is caller-supplied and read into memory, so it needs a limit that is
// not "whatever fits". Ten thousand is far past any real directory and small
// enough that a malicious upload cannot exhaust the server by being large.
const MaxRows = 10_000

// Record is one participant as a source produced it, before anything has been
// checked.
//
// It is the common currency of every source. A CSV row, an object from a JSON
// endpoint and a row from a SQL query arrive as different things and mean the
// same thing, so they are all turned into this and validated once. Otherwise
// each source grows its own idea of what a valid email is, and they drift.
type Record struct {
	// Line is where this came from — the line in a file, the index in a JSON
	// array, the row number of a result set. Carried so a problem can be
	// reported where somebody can find it.
	Line   int
	Fields map[string]string
}

// Field reads a column, absent or not.
func (r Record) Field(name string) string { return r.Fields[name] }

// Row is one participant read from the file.
type Row struct {
	// Line is the line in the file this came from, counting the header. It is
	// carried so a problem can be reported where somebody can find it.
	Line int

	Username string
	// ExternalID is the person's id in the application the source belongs
	// to, when the source carries one.
	ExternalID  string
	DisplayName string
	Email       string
	// Groups are the candidate groups to put this participant in. Groups are
	// assignment targets, not permissions.
	Groups []string
	Active bool
}

// Problem is one row that could not be imported, and why.
type Problem struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
	// Username is echoed when it was readable, so a report names people rather
	// than only line numbers.
	Username string `json:"username,omitzero"`
}

func (p Problem) String() string {
	if p.Username != "" {
		return fmt.Sprintf("line %d (%s): %s", p.Line, p.Username, p.Reason)
	}
	return fmt.Sprintf("line %d: %s", p.Line, p.Reason)
}

// Result is what a file turned out to contain.
type Result struct {
	Rows     []Row
	Problems []Problem
	// Fields are the names the source used, before any mapping, so somebody
	// matching columns can pick from what is actually there.
	Fields []string
	// Sample is the first record as the source sent it, every value cut short,
	// so a mapping can be checked against a real person.
	Sample map[string]string
}

// requiredColumn is the only column a file must have. Everything else about a
// participant can be filled in later; a row with no name is not a participant.
const requiredColumn = "username"

// knownColumns are the headers this understands. An unknown column is ignored
// rather than refused: an export from somebody's HR system will carry columns
// that mean nothing here, and rejecting the file over them would mean asking a
// person to hand-edit a CSV before every import.
var knownColumns = map[string]bool{
	"username": true, "external_id": true, "display_name": true, "email": true,
	"groups": true, "active": true,
}

// Understands reports whether a column or key is part of the shared
// vocabulary. Every source uses the same names, so this is the one place that
// says what they are.
func Understands(name string) bool { return knownColumns[strings.ToLower(strings.TrimSpace(name))] }

// Parse reads a CSV of participants.
//
// The first line is the header and names the columns, in any order — an export
// nobody controls the column order of is the normal case. Only `username` is
// required.
func Parse(r io.Reader) (Result, error) { return ParseWith(r, nil) }

// ParseWith reads a CSV whose columns are named as mapping says.
func ParseWith(r io.Reader, mapping Mapping) (Result, error) {
	if err := mapping.Validate(); err != nil {
		return Result{}, err
	}
	reader := csv.NewReader(r)
	// Rows are allowed to differ in length: a trailing empty column is what a
	// spreadsheet writes for a blank cell, and refusing the file over it would
	// be refusing the most common export there is.
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return Result{}, errors.New("the file is empty")
	}
	if err != nil {
		return Result{}, fmt.Errorf("could not read the header: %w", err)
	}

	index, err := indexHeader(header, mapping.SourceFor(requiredColumn))
	if err != nil {
		return Result{}, err
	}

	var records []Record
	var readProblems []Problem

	for line := 2; ; line++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A malformed line is one row's problem, not the file's: the reader
			// recovers, so the rest still imports.
			readProblems = append(readProblems, Problem{Line: line, Reason: readErrorReason(err)})
			continue
		}
		if isBlank(record) {
			continue
		}
		if len(records) > MaxRows {
			return Result{}, fmt.Errorf("the file has more than %d rows; split it and import the parts", MaxRows)
		}
		records = append(records, Record{Line: line, Fields: fieldsOf(record, index)})
	}

	result := FromRawRecords(records, mapping)
	// Read failures come first: they happened before anything was validated,
	// and reporting them in line order matters more than in stage order.
	result.Problems = append(readProblems, result.Problems...)
	sortProblems(result.Problems)
	return result, nil
}

// fieldsOf reads one CSV record by its header's names. The mapping decides
// which of them are kept.
func fieldsOf(record []string, index map[string]int) map[string]string {
	fields := make(map[string]string, len(index))
	for name, i := range index {
		if i < len(record) {
			fields[name] = record[i]
		}
	}
	return fields
}

// sortProblems puts a report in the order somebody reads their file.
func sortProblems(problems []Problem) {
	sort.SliceStable(problems, func(i, j int) bool { return problems[i].Line < problems[j].Line })
}

// bom is the byte order mark Excel writes at the start of a UTF-8 CSV. It
// arrives glued to the first header name, so "username" reads as something that
// is not "username" and the file is refused for having no such column — which
// is the first thing anybody exporting from Excel would hit.
const bom = "\ufeff"

// indexHeader maps every column to its position, and refuses a header with
// no column for the username.
func indexHeader(header []string, usernameColumn string) (map[string]int, error) {
	index := map[string]int{}
	for i, raw := range header {
		name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(raw, bom)))
		if _, seen := index[name]; !seen && name != "" {
			index[name] = i
		}
	}
	if _, ok := index[usernameColumn]; !ok {
		return nil, fmt.Errorf("the file needs a %q column; found %s",
			usernameColumn, strings.Join(header, ", "))
	}
	return index, nil
}

// readRecord turns one record into a participant, or says why it cannot.
//
// Every source goes through here. What counts as a usable participant is one
// answer, in one place, rather than one per source.
func readRecord(record Record) (Row, *Problem) {
	line := record.Line
	username := strings.TrimSpace(record.Field("username"))
	if username == "" {
		return Row{}, &Problem{Line: line, Reason: "no username"}
	}
	if len(username) > 255 {
		return Row{}, &Problem{Line: line, Username: username, Reason: "the username is longer than 255 characters"}
	}

	externalID := strings.TrimSpace(record.Field("external_id"))
	if len(externalID) > 255 {
		return Row{}, &Problem{Line: line, Username: username, Reason: "the external ID is longer than 255 characters"}
	}

	email := strings.TrimSpace(record.Field("email"))
	if email != "" {
		if _, err := mail.ParseAddress(email); err != nil {
			// Refused rather than imported blank: a participant with a silently
			// dropped address is one who never gets told they have work.
			return Row{}, &Problem{Line: line, Username: username, Reason: fmt.Sprintf("%q is not an email address", email)}
		}
	}

	active, err := readActive(record.Field("active"))
	if err != nil {
		return Row{}, &Problem{Line: line, Username: username, Reason: err.Error()}
	}

	return Row{
		Line:        line,
		Username:    username,
		ExternalID:  externalID,
		DisplayName: strings.TrimSpace(record.Field("display_name")),
		Email:       email,
		Groups:      readGroups(record.Field("groups")),
		Active:      active,
	}, nil
}

// FromRecords validates a batch a source has already fetched.
//
// The half of Parse that is not about commas. A source that speaks JSON or SQL
// produces records and calls this; nothing about duplicate names, addresses or
// the active column is decided twice.
func FromRecords(records []Record) Result {
	var result Result
	seen := map[string]int{}
	seenExternal := map[string]int{}

	for _, record := range records {
		if len(result.Rows) >= MaxRows {
			result.Problems = append(result.Problems, Problem{
				Line:   record.Line,
				Reason: fmt.Sprintf("more than %d participants; the rest were not read", MaxRows),
			})
			break
		}
		row, problem := readRecord(record)
		if problem != nil {
			result.Problems = append(result.Problems, *problem)
			continue
		}
		if first, duplicate := seen[strings.ToLower(row.Username)]; duplicate {
			result.Problems = append(result.Problems, Problem{
				Line:     record.Line,
				Username: row.Username,
				Reason:   fmt.Sprintf("already named on line %d", first),
			})
			continue
		}
		// An external ID twice is one person named twice, which says nothing
		// about which of the two rows is right. Compared exactly: an id is the
		// application's, and "A1" and "a1" may well be two people there.
		if row.ExternalID != "" {
			if first, duplicate := seenExternal[row.ExternalID]; duplicate {
				result.Problems = append(result.Problems, Problem{
					Line:     record.Line,
					Username: row.Username,
					Reason:   fmt.Sprintf("external ID %q is already named on line %d", row.ExternalID, first),
				})
				continue
			}
			seenExternal[row.ExternalID] = record.Line
		}
		seen[strings.ToLower(row.Username)] = record.Line
		result.Rows = append(result.Rows, row)
	}
	return result
}

// readActive reads the active column, which is absent far more often than not.
//
// Absent means active: somebody importing a directory is adding people who
// should receive work, and defaulting to inactive would import five hundred
// participants that nothing can assign to.
func readActive(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "true", "yes", "y", "1", "active":
		return true, nil
	case "false", "no", "n", "0", "inactive":
		return false, nil
	default:
		return false, fmt.Errorf("%q is not yes or no", raw)
	}
}

// readGroups splits the groups column.
//
// Semicolon-separated, because a comma inside a CSV cell means quoting the cell
// and spreadsheets are inconsistent about doing so. Empty names are dropped, so
// "approvers;;finance" is two groups rather than three with one unnamed.
func readGroups(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var groups []string
	for _, part := range strings.Split(raw, ";") {
		if name := strings.TrimSpace(part); name != "" {
			groups = append(groups, name)
		}
	}
	return groups
}

func isBlank(record []string) bool {
	for _, cell := range record {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

// readErrorReason turns a csv reader failure into something a person can act on.
func readErrorReason(err error) string {
	var parseErr *csv.ParseError
	if errors.As(err, &parseErr) && parseErr.Err != nil {
		return parseErr.Err.Error()
	}
	return err.Error()
}
