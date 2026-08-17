package feel

import (
	"fmt"
	"strings"
	"unicode"
)

type tokenKind uint8

const (
	tokEOF tokenKind = iota
	tokNumber
	tokString
	tokName     // identifier, possibly multi-word: "list contains"
	tokKeyword  // and, or, not, true, false, null, in, between, instance, of
	tokOperator // + - * / ** = != < <= > >= .
	tokLParen
	tokRParen
	tokLBracket
	tokRBracket
	tokLBrace
	tokRBrace
	tokComma
	tokRange // ..
	tokColon
)

type token struct {
	kind tokenKind
	text string
	num  float64
	pos  int
}

func (t token) String() string {
	if t.kind == tokEOF {
		return "end of expression"
	}
	return fmt.Sprintf("%q", t.text)
}

// keywords are the reserved words. FEEL's built-in function names are NOT here
// — they are ordinary names resolved at call time, which is what lets
// `starts with` work as a multi-word identifier.
var keywords = map[string]bool{
	"and": true, "or": true, "not": true,
	"true": true, "false": true, "null": true,
	"in": true, "between": true, "if": true, "then": true, "else": true,
}

// multiWordNames are the built-in identifiers containing spaces. FEEL allows
// spaces inside names, which is ambiguous in general; rather than implement the
// specification's full backtracking name resolution, the lexer joins exactly
// the names this subset defines. Anything else keeps the ordinary one-word
// rule, so an unknown multi-word name fails as an unknown name rather than
// silently parsing as something else.
var multiWordNames = []string{
	"starts with",
	"ends with",
	"list contains",
	"string length",
	"upper case",
	"lower case",
	"day of week",
	"day of year",
	"month of year",
	"week of year",
}

type lexer struct {
	input  string
	pos    int
	tokens []token
}

// tokenize splits an expression into tokens.
func tokenize(input string) ([]token, error) {
	l := &lexer{input: input}
	if err := l.run(); err != nil {
		return nil, err
	}
	l.tokens = append(l.tokens, token{kind: tokEOF, pos: len(input)})
	return l.tokens, nil
}

func (l *lexer) run() error {
	for l.pos < len(l.input) {
		c := l.input[l.pos]

		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			l.pos++

		case c >= '0' && c <= '9':
			if err := l.lexNumber(); err != nil {
				return err
			}

		case c == '"' || c == '\'':
			// FEEL only defines double-quoted strings. Single quotes are
			// accepted because deployed tables in this codebase use them —
			// they came from the JavaScript-flavoured evaluator this replaces,
			// where 'VIP' was ordinary. Rejecting them would break decisions
			// that are live today, and the character is unambiguous: nothing
			// else in the grammar uses it.
			if err := l.lexString(); err != nil {
				return err
			}

		case isNameStart(rune(c)):
			l.lexName()

		default:
			if err := l.lexSymbol(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *lexer) lexNumber() error {
	start := l.pos
	for l.pos < len(l.input) && (isDigit(l.input[l.pos]) || l.input[l.pos] == '.') {
		// A ".." is a range operator, not a decimal point — stop before it, or
		// `[1..10]` would lex as the number "1." followed by ".10".
		if l.input[l.pos] == '.' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '.' {
			break
		}
		l.pos++
	}

	text := l.input[start:l.pos]
	var f float64
	if _, err := fmt.Sscanf(text, "%g", &f); err != nil {
		return fmt.Errorf("feel: %q is not a number at position %d", text, start)
	}
	l.tokens = append(l.tokens, token{kind: tokNumber, text: text, num: f, pos: start})
	return nil
}

func (l *lexer) lexString() error {
	start := l.pos
	quote := l.input[l.pos]
	l.pos++ // opening quote

	var sb strings.Builder
	for l.pos < len(l.input) {
		c := l.input[l.pos]
		if c == '\\' && l.pos+1 < len(l.input) {
			l.pos++
			switch l.input[l.pos] {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case '"', '\'':
				sb.WriteByte(l.input[l.pos])
			case '\\':
				sb.WriteByte('\\')
			default:
				sb.WriteByte(l.input[l.pos])
			}
			l.pos++
			continue
		}
		if c == quote {
			l.pos++
			l.tokens = append(l.tokens, token{kind: tokString, text: sb.String(), pos: start})
			return nil
		}
		sb.WriteByte(c)
		l.pos++
	}
	return fmt.Errorf("feel: unterminated string starting at position %d", start)
}

func (l *lexer) lexName() {
	start := l.pos
	for l.pos < len(l.input) && isNamePart(rune(l.input[l.pos])) {
		l.pos++
	}
	word := l.input[start:l.pos]

	// Try to extend into one of the known multi-word names.
	if joined, width := l.matchMultiWord(start); joined != "" {
		l.pos = start + width
		l.tokens = append(l.tokens, token{kind: tokName, text: joined, pos: start})
		return
	}

	kind := tokName
	if keywords[word] {
		kind = tokKeyword
	}
	l.tokens = append(l.tokens, token{kind: kind, text: word, pos: start})
}

// matchMultiWord reports the longest known multi-word name starting at pos.
func (l *lexer) matchMultiWord(pos int) (string, int) {
	rest := l.input[pos:]
	best, bestWidth := "", 0
	for _, name := range multiWordNames {
		if len(name) > len(rest) || !strings.EqualFold(rest[:len(name)], name) {
			continue
		}
		// The match must end at a name boundary, so "starts within" does not
		// match "starts with".
		if len(rest) > len(name) && isNamePart(rune(rest[len(name)])) {
			continue
		}
		if len(name) > bestWidth {
			best, bestWidth = name, len(name)
		}
	}
	return best, bestWidth
}

func (l *lexer) lexSymbol() error {
	start := l.pos
	two := ""
	if l.pos+1 < len(l.input) {
		two = l.input[l.pos : l.pos+2]
	}

	switch two {
	case "..":
		l.pos += 2
		l.tokens = append(l.tokens, token{kind: tokRange, text: "..", pos: start})
		return nil
	case "<=", ">=", "!=", "**":
		l.pos += 2
		l.tokens = append(l.tokens, token{kind: tokOperator, text: two, pos: start})
		return nil
	}

	c := l.input[l.pos]
	l.pos++
	switch c {
	case '(':
		l.tokens = append(l.tokens, token{kind: tokLParen, text: "(", pos: start})
	case ')':
		l.tokens = append(l.tokens, token{kind: tokRParen, text: ")", pos: start})
	case '[':
		l.tokens = append(l.tokens, token{kind: tokLBracket, text: "[", pos: start})
	case ']':
		l.tokens = append(l.tokens, token{kind: tokRBracket, text: "]", pos: start})
	case '{':
		l.tokens = append(l.tokens, token{kind: tokLBrace, text: "{", pos: start})
	case '}':
		l.tokens = append(l.tokens, token{kind: tokRBrace, text: "}", pos: start})
	case ',':
		l.tokens = append(l.tokens, token{kind: tokComma, text: ",", pos: start})
	case ':':
		l.tokens = append(l.tokens, token{kind: tokColon, text: ":", pos: start})
	case '+', '-', '*', '/', '=', '<', '>', '.':
		l.tokens = append(l.tokens, token{kind: tokOperator, text: string(c), pos: start})
	default:
		return fmt.Errorf("feel: unexpected character %q at position %d", string(c), start)
	}
	return nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isNameStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isNamePart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
