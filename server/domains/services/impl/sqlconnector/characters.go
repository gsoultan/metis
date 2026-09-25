package sqlconnector

import "unicode"

// isSpace is ASCII whitespace only. Every server here agrees on these; they do
// not all agree on the rest of Unicode's, which is why the lexer refuses those.
func isSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

func isDigit(r rune) bool { return '0' <= r && r <= '9' }

func isHexDigit(c byte) bool {
	return isDigit(rune(c)) || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

func isASCIILetter(r rune) bool { return ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') }

// isWordStart is a letter or underscore. Letters beyond ASCII are identifier
// characters on all three servers.
func isWordStart(r rune) bool {
	return isASCIILetter(r) || r == '_' || (r > unicode.MaxASCII && unicode.IsLetter(r))
}

// isWordPart also takes digits and $, which all three servers allow after the
// first character of an unquoted identifier — so a$b is one name here as it is
// there, and a $ that starts a token is still refused.
func isWordPart(r rune) bool {
	if isASCIILetter(r) || isDigit(r) || r == '_' || r == '$' {
		return true
	}
	return r > unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r))
}

// isParamStart and isParamPart keep parameter names to plain ASCII, since they
// become placeholder names and map keys.
func isParamStart(c byte) bool { return isASCIILetter(rune(c)) || c == '_' }

func isParamPart(c byte) bool { return isParamStart(c) || isDigit(rune(c)) }
