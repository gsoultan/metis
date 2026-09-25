package sqlconnector

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// lexer splits a statement into tokens under one lexMode.
//
// It is stricter than any server it reads for, on purpose. Anything whose
// meaning differs between servers, or between one server's settings, is
// refused rather than interpreted:
//
//   - comments: a quote inside one moves every string boundary after it, and
//     MySQL runs /*! ... */ as code;
//   - $, which starts a PostgreSQL dollar-quoted string;
//   - #, a MySQL comment and a PostgreSQL operator;
//   - ?, a MySQL placeholder that would take one of the step's own values;
//   - a control character, or a non-ASCII character that is not a letter,
//     digit or mark — a server that took a no-break space for whitespace would
//     see two words where this sees one, and the second could be DELETE.
//
// A number stops at the first letter, so 1DELETE is two tokens. SQL Server
// reads SELECT 1DELETE FROM t as a SELECT and then a DELETE.
type lexer struct {
	src    string
	pos    int
	mode   lexMode
	tokens []token
}

// lex reads a whole statement, or says where it could not.
func lex(statement string, mode lexMode) ([]token, error) {
	l := &lexer{src: statement, mode: mode}
	for l.pos < len(l.src) {
		if err := l.next(); err != nil {
			return nil, err
		}
	}
	return l.tokens, nil
}

func (l *lexer) next() error {
	r, size := utf8.DecodeRuneInString(l.src[l.pos:])
	switch {
	case r == utf8.RuneError && size <= 1:
		return refused(l.pos, "the query is not valid text")
	case isSpace(r):
		l.pos += size
		return nil
	case r == '\'':
		return l.quoted('\'', tokenString, true)
	case r == '"':
		return l.quoted('"', tokenQuotedIdentifier, true)
	case r == '`':
		return l.quoted('`', tokenQuotedIdentifier, false)
	case r == '[' && l.mode.bracketIdentifiers:
		return l.quoted(']', tokenQuotedIdentifier, false)
	case r == ':':
		l.colon()
		return nil
	case isDigit(r):
		l.number()
		return nil
	case isWordStart(r):
		l.word()
		return nil
	}
	return l.symbol(r, size)
}

// symbol takes one punctuation character, or refuses the ones whose meaning is
// not the same everywhere.
func (l *lexer) symbol(r rune, size int) error {
	switch {
	case r == '-' && l.at(1) == '-', r == '/' && l.at(1) == '*':
		return refused(l.pos, "comments are not allowed in a lookup; describe the query in the step's name instead")
	case r == '#', r == '$':
		return refused(l.pos, "%q is not allowed outside quotes", r)
	case r == '?':
		return refused(l.pos, "? is not allowed outside quotes; name each value, as in :customer_id")
	case r < ' ' || r == 0x7f || r > unicode.MaxASCII:
		return refused(l.pos, "the character %U is not allowed outside quotes", r)
	case r == ';':
		l.emit(tokenSeparator, "", l.pos, l.pos+size)
	default:
		l.emit(tokenSymbol, string(r), l.pos, l.pos+size)
	}
	l.pos += size
	return nil
}

// quoted reads a string literal or a quoted identifier up to its closing
// character. A doubled closer is an escaped one. A backslash escapes the next
// character only when the mode says the server may read it that way.
func (l *lexer) quoted(closer byte, kind tokenKind, backslashMayEscape bool) error {
	start := l.pos
	l.pos++
	var content strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\\' && backslashMayEscape && l.mode.backslashEscapes:
			if l.pos+1 >= len(l.src) {
				return refused(start, "a quote is never closed")
			}
			content.WriteByte(l.src[l.pos+1])
			l.pos += 2
		case c == closer && l.at(1) == closer:
			content.WriteByte(closer)
			l.pos += 2
		case c == closer:
			l.pos++
			l.emitQuoted(kind, content.String(), start)
			return nil
		default:
			content.WriteByte(c)
			l.pos++
		}
	}
	return refused(start, "a quote is never closed")
}

// emitQuoted keeps a quoted identifier's name, which is checked, and drops a
// string's contents, which are not.
func (l *lexer) emitQuoted(kind tokenKind, content string, start int) {
	if kind == tokenString {
		content = ""
	}
	l.emit(kind, content, start, l.pos)
}

// colon reads a :name parameter. :: is a PostgreSQL cast and := a MySQL
// assignment; neither is a parameter.
func (l *lexer) colon() {
	if l.at(1) == ':' {
		l.emit(tokenSymbol, "::", l.pos, l.pos+2)
		l.pos += 2
		return
	}
	if !isParamStart(l.at(1)) {
		l.emit(tokenSymbol, ":", l.pos, l.pos+1)
		l.pos++
		return
	}
	start := l.pos
	l.pos++
	for l.pos < len(l.src) && isParamPart(l.src[l.pos]) {
		l.pos++
	}
	l.emit(tokenParam, l.src[start+1:l.pos], start, l.pos)
}

// number reads digits, a fraction and an exponent, or a 0x hex literal — and
// stops at the first letter, so a keyword pressed against a number is still a
// keyword.
func (l *lexer) number() {
	start := l.pos
	if l.src[l.pos] == '0' && (l.at(1) == 'x' || l.at(1) == 'X') {
		l.pos += 2
		for l.pos < len(l.src) && isHexDigit(l.src[l.pos]) {
			l.pos++
		}
		l.emit(tokenNumber, l.src[start:l.pos], start, l.pos)
		return
	}
	l.digits()
	if l.at(0) == '.' {
		l.pos++
		l.digits()
	}
	if (l.at(0) == 'e' || l.at(0) == 'E') && l.exponentFollows() {
		l.pos += 2
		l.digits()
	}
	l.emit(tokenNumber, l.src[start:l.pos], start, l.pos)
}

func (l *lexer) exponentFollows() bool {
	next := l.at(1)
	return isDigit(rune(next)) || ((next == '+' || next == '-') && isDigit(rune(l.at(2))))
}

func (l *lexer) digits() {
	for l.pos < len(l.src) && isDigit(rune(l.src[l.pos])) {
		l.pos++
	}
}

func (l *lexer) word() {
	start := l.pos
	for l.pos < len(l.src) {
		r, size := utf8.DecodeRuneInString(l.src[l.pos:])
		if !isWordPart(r) {
			break
		}
		l.pos += size
	}
	l.emit(tokenWord, strings.ToLower(l.src[start:l.pos]), start, l.pos)
}

func (l *lexer) emit(kind tokenKind, text string, start, end int) {
	l.tokens = append(l.tokens, token{kind: kind, text: text, start: start, end: end})
}

// at is the byte offset positions ahead, or 0 past the end.
func (l *lexer) at(offset int) byte {
	if l.pos+offset >= len(l.src) {
		return 0
	}
	return l.src[l.pos+offset]
}
