package redaction

import (
	"regexp"
	"strings"
	"sync"
)

const redactedValue = "***REDACTED***"

type patterns struct {
	urlCredential *regexp.Regexp
	// mysqlDSN is the MySQL driver's connection string, user:password@tcp(host)/db,
	// which has no scheme for urlCredential to anchor on. Lazy up to the protocol,
	// so a password with an @ in it is still covered: the driver itself splits on
	// the last one.
	mysqlDSN    *regexp.Regexp
	bearerToken *regexp.Regexp
	jsonSecret  *regexp.Regexp
	kvEquals    *regexp.Regexp
	kvColon     *regexp.Regexp
	// proseWord is a word as a sentence has it, which a credential is not: see
	// readsAsProse.
	proseWord *regexp.Regexp
}

var (
	patternsOnce sync.Once
	compiled     *patterns
)

func getPatterns() *patterns {
	patternsOnce.Do(func() {
		compiled = &patterns{
			urlCredential: regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:@/\s]+:)([^@/\s]+)(@)`),
			mysqlDSN:      regexp.MustCompile(`([A-Za-z0-9_.\-]+:)(\S+?)(@(?:(?:tcp|tcp4|tcp6|unix)\(|/))`),
			bearerToken:   regexp.MustCompile(`(?i)(bearer\s+)([^\s,;]+)`),
			jsonSecret:    regexp.MustCompile(`(?i)("(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?token|refresh[_-]?token|jwt|encryption[_-]?key)"\s*:\s*")([^"]*)(")`),
			kvEquals:      regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?token|refresh[_-]?token|jwt|encryption[_-]?key)\b(\s*=\s*)([^\s,;]+)`),
			kvColon:       regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?token|refresh[_-]?token|jwt|encryption[_-]?key)\b(\s*:\s*)([^\s,;]+)`),
			// Lower-case words, joined by a hyphen, slash or apostrophe (go-jose/go-jose,
			// doesn't); a capitalised word; a word in capitals (ID, ERROR). Each may end
			// in the colon that opens the next link of an error chain (oidc:).
			proseWord: regexp.MustCompile(`^(?:[a-z]+(?:['/-][a-z]+)*|[A-Z][a-z]+|[A-Z]+):?$`),
		}
	})

	return compiled
}

func RedactText(value string) string {
	if value == "" {
		return value
	}

	patterns := getPatterns()

	redacted := value
	redacted = patterns.urlCredential.ReplaceAllString(redacted, `${1}`+redactedValue+`${3}`)
	redacted = patterns.mysqlDSN.ReplaceAllString(redacted, `${1}`+redactedValue+`${3}`)
	redacted = patterns.bearerToken.ReplaceAllString(redacted, `${1}`+redactedValue)
	redacted = patterns.jsonSecret.ReplaceAllString(redacted, `${1}`+redactedValue+`${3}`)
	redacted = patterns.kvEquals.ReplaceAllString(redacted, `${1}${2}`+redactedValue)
	redacted = patterns.redactColonValues(redacted)

	return redacted
}

// redactColonValues redacts what follows a secret's name and a colon, unless
// it is the next word of a sentence.
//
// The same colon ends a key in "token: 9f2c…" and joins the links of a Go error
// in "missing or invalid token: the ID token names no issuer". Redacting
// whatever came next ate the first word of every such error — and spent the
// match on it, so in "failed to verify token: jwt: eyJ…" the word jwt: was
// redacted and the token after it was left in clear. When the word is kept,
// matching resumes at it, because it may itself be a secret's name.
func (p *patterns) redactColonValues(text string) string {
	var out strings.Builder
	rest := text
	for {
		m := p.kvColon.FindStringSubmatchIndex(rest)
		if m == nil {
			break
		}
		separator, value, after := rest[m[4]:m[5]], rest[m[6]:m[7]], rest[m[7]:]
		out.WriteString(rest[:m[6]])
		if p.readsAsProse(separator, value, after) {
			rest = rest[m[6]:]
			continue
		}
		out.WriteString(redactedValue)
		rest = after
	}
	out.WriteString(rest)
	return out.String()
}

// readsAsProse reports whether value, after a secret's name and separator, is
// the next word of a sentence rather than a credential.
//
// All three must hold, so anything that could be a credential is redacted:
//
//   - the colon is followed by a space, as it is between the links of an error.
//     Go prints a map or struct as password:letmein, with none, and a value on
//     its own line is a key's value, not a sentence going on;
//   - the value is shaped like a word: letters in one case or capitalised, with
//     no digit, symbol or change of case inside it, which a generated token or
//     key almost always has;
//   - the sentence goes on after it, on the same line, with another word. A
//     value that ends the text, the line or the field is what a key's value
//     does, and a password can be a plain word.
//
// So a plain-word password followed by more words on the same line is not
// redacted. That shape is a sentence, and redacting it is what hid the errors.
func (p *patterns) readsAsProse(separator, value, after string) bool {
	if !strings.HasSuffix(separator, " ") || strings.ContainsAny(separator, "\r\n") {
		return false
	}
	if !p.proseWord.MatchString(value) {
		return false
	}
	next := strings.TrimLeft(after, " \t")
	return len(next) < len(after) && next != "" && isASCIILetter(next[0])
}

func isASCIILetter(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func RedactError(err error) string {
	if err == nil {
		return ""
	}

	return RedactText(err.Error())
}
