package sqlconnector

// tokenKind is what a stretch of a statement is, as far as validation cares.
type tokenKind int

const (
	// tokenWord is an unquoted identifier or keyword.
	tokenWord tokenKind = iota
	// tokenNumber is a numeric literal.
	tokenNumber
	// tokenParam is a :name parameter; text holds the name without the colon.
	tokenParam
	// tokenString is a string literal. Its contents are never inspected: a
	// lookup filtering on status = 'deleted' is ordinary.
	tokenString
	// tokenQuotedIdentifier is "name", `name` or [name]. Its contents are
	// checked as a whole, because a function can be called by quoted name.
	tokenQuotedIdentifier
	// tokenSeparator is a semicolon.
	tokenSeparator
	// tokenSymbol is any other single character: an operator, a comma, a
	// parenthesis.
	tokenSymbol
)

// token is one lexed stretch of a statement.
type token struct {
	kind tokenKind
	// text is the word, the parameter name, or the quoted identifier's
	// contents. Empty for a string literal, whose contents do not matter here.
	text string
	// start is the byte offset of the token in the statement, for placing a
	// parameter's replacement and for saying where a problem is.
	start int
	// end is the byte offset just past the token.
	end int
}
