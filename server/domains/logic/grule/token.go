package grule

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokNumber
	tokString
	tokPunct
)

// token is one lexed piece of GRL. line is 1-based, for messages.
type token struct {
	kind tokenKind
	text string
	line int
}
