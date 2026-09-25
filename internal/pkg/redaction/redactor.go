package redaction

import (
	"regexp"
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
	redacted = patterns.kvColon.ReplaceAllString(redacted, `${1}${2}`+redactedValue)

	return redacted
}

func RedactError(err error) string {
	if err == nil {
		return ""
	}

	return RedactText(err.Error())
}
