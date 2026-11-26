package grule

import (
	"fmt"
	"strings"
)

// punctuation is every operator and separator GRL has, longest first so that
// `<=` is read as one token and not as `<` followed by `=`.
var punctuation = []string{
	"&&", "||", "==", "!=", "<=", ">=", "+=", "-=", "*=", "/=",
	"+", "-", "*", "/", "%", "<", ">", "=", "!", "&", "|",
	"(", ")", "{", "}", "[", "]", ",", ";", ".",
}

// stringEscapes are the escapes a GRL string may use, and what each stands for.
var stringEscapes = map[byte]byte{'"': '"', '\'': '\'', '\\': '\\', 'n': '\n', 't': '\t', 'r': '\r'}

// lex splits GRL source into tokens, dropping whitespace and comments.
func lex(source string) ([]token, error) {
	l := &lexer{src: source, line: 1}
	for {
		if err := l.skipSpaceAndComments(); err != nil {
			return nil, err
		}
		if l.pos >= len(l.src) {
			l.tokens = append(l.tokens, token{kind: tokEOF, line: l.line})
			return l.tokens, nil
		}
		if err := l.next(); err != nil {
			return nil, err
		}
	}
}

type lexer struct {
	src    string
	pos    int
	line   int
	tokens []token
}

func (l *lexer) next() error {
	c := l.src[l.pos]
	switch {
	case isIdentStart(c):
		l.lexIdent()
		return nil
	case isDigit(c) || (c == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1])):
		return l.lexNumber()
	case c == '"' || c == '\'':
		return l.lexString(c)
	}
	for _, p := range punctuation {
		if strings.HasPrefix(l.src[l.pos:], p) {
			l.emit(tokPunct, p)
			l.pos += len(p)
			return nil
		}
	}
	return fmt.Errorf("line %d: unexpected character %q", l.line, c)
}

func (l *lexer) emit(kind tokenKind, text string) {
	l.tokens = append(l.tokens, token{kind: kind, text: text, line: l.line})
}

func (l *lexer) skipSpaceAndComments() error {
	for l.pos < len(l.src) {
		rest := l.src[l.pos:]
		switch {
		case rest[0] == '\n':
			l.line++
			l.pos++
		case rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\r':
			l.pos++
		case strings.HasPrefix(rest, "//"):
			end := strings.IndexByte(rest, '\n')
			if end < 0 {
				end = len(rest)
			}
			l.pos += end
		case strings.HasPrefix(rest, "/*"):
			end := strings.Index(rest[2:], "*/")
			if end < 0 {
				return fmt.Errorf("line %d: a comment is never closed", l.line)
			}
			l.line += strings.Count(rest[:end+2], "\n")
			l.pos += end + 4
		default:
			return nil
		}
	}
	return nil
}

func (l *lexer) lexIdent() {
	start := l.pos
	for l.pos < len(l.src) && (isIdentStart(l.src[l.pos]) || isDigit(l.src[l.pos])) {
		l.pos++
	}
	l.emit(tokIdent, l.src[start:l.pos])
}

func (l *lexer) lexNumber() error {
	start := l.pos
	for l.pos < len(l.src) && (isDigit(l.src[l.pos]) || l.src[l.pos] == '.') {
		l.pos++
	}
	if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
		l.pos++
		if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
			l.pos++
		}
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
		}
	}
	l.emit(tokNumber, l.src[start:l.pos])
	return nil
}

// lexString reads a quoted string with Go's escapes for quotes, backslash,
// newline and tab. Any other escape is refused rather than guessed at.
func (l *lexer) lexString(quote byte) error {
	line := l.line
	l.pos++
	var sb strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch c {
		case quote:
			l.pos++
			l.tokens = append(l.tokens, token{kind: tokString, text: sb.String(), line: line})
			return nil
		case '\n':
			return fmt.Errorf("line %d: a string runs past the end of its line", line)
		case '\\':
			if err := l.lexEscape(&sb, line); err != nil {
				return err
			}
		default:
			sb.WriteByte(c)
			l.pos++
		}
	}
	return fmt.Errorf("line %d: a string is never closed", line)
}

func (l *lexer) lexEscape(sb *strings.Builder, line int) error {
	if l.pos+1 >= len(l.src) {
		return fmt.Errorf("line %d: a string ends in a backslash", line)
	}
	escaped, ok := stringEscapes[l.src[l.pos+1]]
	if !ok {
		return fmt.Errorf("line %d: the escape \\%c is not supported", line, l.src[l.pos+1])
	}
	sb.WriteByte(escaped)
	l.pos += 2
	return nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
