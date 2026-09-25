package pg

import "strings"

// likeEscaper escapes the characters LIKE treats specially, with the backslash
// PostgreSQL uses as LIKE's escape character unless told otherwise.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// containsPattern turns what somebody typed into a search box into an ILIKE
// pattern matching any text that contains it, and reports false when there is
// nothing to search for.
//
// Escaped, because the text is the user's: "50%" should find the discount
// called "50% off", not every name with a 50 in it, and an underscore — common
// in keys — is otherwise a wildcard for any one character.
func containsPattern(search string) (string, bool) {
	search = strings.TrimSpace(search)
	if search == "" {
		return "", false
	}
	return "%" + likeEscaper.Replace(search) + "%", true
}
