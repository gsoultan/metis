package entities

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
)

// RoleAction is one action a role is required for, named for a person.
//
// The one place a gated method is put into words. The gate knows the method
// by the name it was built with; whoever asks what a role allows wants a
// sentence and a heading to find it under.
type RoleAction struct {
	// Method is the name the gate was built with, which is also the action's
	// name in the request log, so an entry can be traced to the line that
	// enforces it.
	Method string `json:"method"`

	// Area is the part of the product the action belongs to, as a key the
	// interface translates. RoleActionAreas lists them.
	Area string `json:"area"`

	// Label is the method in words: "CreateDefinition" reads "Create definition".
	Label string `json:"label"`
}

// NewRoleAction describes the action a gated method performs.
func NewRoleAction(method string) RoleAction {
	return RoleAction{Method: method, Area: actionArea(method), Label: actionLabel(method)}
}

// CompareRoleActions orders actions by area, in RoleActionAreas' order with
// anything unplaced last, and then by their words.
func CompareRoleActions(a, b RoleAction) int {
	return cmp.Or(
		cmp.Compare(areaRank(a.Area), areaRank(b.Area)),
		cmp.Compare(a.Label, b.Label),
		cmp.Compare(a.Method, b.Method),
	)
}

func areaRank(area string) int {
	areas := RoleActionAreas()
	if rank := slices.Index(areas, area); rank >= 0 {
		return rank
	}
	return len(areas)
}

// actionLabel puts a method name into words: "ActivateAdHocTask" reads
// "Activate ad hoc task". A run of capitals is an abbreviation and stays one:
// "ExportOCEL" reads "Export OCEL".
func actionLabel(method string) string {
	words := camelWords(method)
	for i := 1; i < len(words); i++ {
		if !isAbbreviation(words[i]) {
			words[i] = strings.ToLower(words[i])
		}
	}
	return strings.Join(words, " ")
}

// camelWords splits a name where each word starts.
func camelWords(name string) []string {
	runes := []rune(name)
	var words []string
	start := 0
	for i := 1; i < len(runes); i++ {
		if startsWord(runes, i) {
			words = append(words, string(runes[start:i]))
			start = i
		}
	}
	if start < len(runes) {
		words = append(words, string(runes[start:]))
	}
	return words
}

// startsWord reports whether a word starts at i: a capital after a small
// letter or a digit, or the last capital of a run when a small letter follows
// it — the T of "OCELTrace".
func startsWord(runes []rune, i int) bool {
	if !unicode.IsUpper(runes[i]) {
		return false
	}
	previous := runes[i-1]
	if unicode.IsLower(previous) || unicode.IsDigit(previous) {
		return true
	}
	return unicode.IsUpper(previous) && i+1 < len(runes) && unicode.IsLower(runes[i+1])
}

func isAbbreviation(word string) bool {
	return len(word) > 1 && strings.ToUpper(word) == word
}
