package sqlconnector

// lexMode is one reading of how a server treats a backslash inside quotes.
//
// Whether \' ends a string depends on settings the connector cannot see:
// PostgreSQL's standard_conforming_strings, MySQL's NO_BACKSLASH_ESCAPES. A
// validator that guesses wrong can take text the server runs as code for the
// inside of a string and never look at it. So a statement is read both ways,
// and it has to be clean both ways.
type lexMode struct {
	backslashEscapes bool
	// bracketIdentifiers treats [name] as a quoted identifier, as SQL Server
	// does. Elsewhere [ and ] are array subscripts and are left as symbols, so
	// what is between them is still read.
	bracketIdentifiers bool
}
