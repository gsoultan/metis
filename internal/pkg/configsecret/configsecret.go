// Package configsecret keeps a stored credential out of the browser.
//
// A connector instance's configuration holds whatever the connector needs to
// authenticate: a bearer token, an SMTP password, a webhook signing secret. The
// API used to return that map verbatim, so opening the Connectors page put
// every one of an organization's third-party credentials into the response, the
// browser's memory and its developer tools. Anyone who could read the page
// could read the secrets, and any script that ran on the page could take them.
//
// The fix is not to stop returning the configuration — the form has to render
// the fields, and the non-secret ones (a URL, a channel name, a sender address)
// are exactly what the person is there to check. It is to return a sentinel in
// place of each secret, and to accept that sentinel back on an update as "keep
// what you already have". The value never leaves the server, and editing an
// unrelated field does not require re-typing a password.
//
// The executor is unaffected: it reads instances through the service directly,
// not through the API, so it still gets the real values.
package configsecret

import "strings"

// Sentinel is what the API returns in place of a stored secret, and what it
// accepts back to mean "unchanged". A fixed, obviously-not-a-password string
// rather than a run of asterisks: asterisks are a plausible password, and a
// caller sending them back would otherwise set one.
const Sentinel = "__unchanged__"

// sensitiveFragments name a configuration key that holds a credential.
//
// Matched as substrings of the normalised key, because connector authors name
// these things every way there is: apiKey, api_key, API-KEY, clientSecret,
// smtp_password, authToken, connectionString. Over-matching is the safe
// direction — masking a field that turns out to be harmless costs a re-type;
// missing one publishes a credential.
//
// Written already normalised: lowercase, no separators. See normaliseKey.
//
// dsn and the connection-string spellings were missing, and a database
// connection string is every credential in that system at once. A directory
// source's DSN — password included — was returned to the browser by the
// participant sources page, which masked its configuration with this list and
// had no key this list recognised.
//
// webhook was missing too. A Slack, Discord or Teams webhook URL is the whole
// credential: anybody holding it can post as the integration. The catalogue
// declared those fields as passwords, which changed the input the form drew and
// nothing about what the server sent back.
var sensitiveFragments = []string{
	"secret",
	"password",
	"passwd",
	"token",
	"apikey",
	"credential",
	"private",
	"signature",
	"key",
	"dsn",
	"connectionstring",
	"connstring",
	"connectionuri",
	"webhook",
}

// SensitiveFragments returns the fragments a sensitive key is recognised by.
//
// A copy, so a caller cannot widen or narrow the list. It exists for the drift
// test that holds the browser's copy of this list to this one: the browser
// decides from its own copy whether to draw a password box, and that copy had
// already fallen four fragments behind.
func SensitiveFragments() []string {
	return append([]string(nil), sensitiveFragments...)
}

// IsSensitive reports whether a configuration key holds a credential.
func IsSensitive(key string) bool {
	normalised := normaliseKey(key)
	for _, fragment := range sensitiveFragments {
		if strings.Contains(normalised, fragment) {
			return true
		}
	}
	return false
}

// normaliseKey lowercases a key and drops everything that is not a letter or a
// digit, so connection_string, connectionString, connection-string and
// "Connection String" are all the same key.
//
// It can only add matches, never remove one: every fragment is itself letters
// and digits, so a key that contained a fragment before still contains it once
// the separators around it are gone. The one fragment that had a separator,
// api_key, is apikey here and matches everything api_key did.
func normaliseKey(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range strings.ToLower(key) {
		if ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Mask returns a copy of config with every secret replaced by the sentinel.
//
// A copy, not an edit in place: the map handed in may be the one the executor
// is about to use, and masking it there would break the very call it is for.
// An empty value is left empty rather than masked, so a field nobody has filled
// in still reads as blank instead of looking like a stored secret.
func Mask(config map[string]any) map[string]any {
	if config == nil {
		return nil
	}
	masked := make(map[string]any, len(config))
	for key, value := range config {
		if IsSensitive(key) && !isEmpty(value) {
			masked[key] = Sentinel
			continue
		}
		masked[key] = value
	}
	return masked
}

// Merge folds an incoming configuration onto the stored one, keeping any value
// the caller sent back as the sentinel.
//
// Anything else the caller sends wins, including a deliberate empty string:
// clearing a credential is a thing people need to do, and treating "" as
// "unchanged" would make a secret impossible to remove.
//
// Keys the caller omitted entirely are dropped, because the form sends the
// whole configuration and an omitted key means the field was removed.
func Merge(incoming, stored map[string]any) map[string]any {
	if incoming == nil {
		return nil
	}
	merged := make(map[string]any, len(incoming))
	for key, value := range incoming {
		if text, ok := value.(string); ok && text == Sentinel {
			if previous, held := stored[key]; held {
				merged[key] = previous
			}
			// A sentinel for a key that was never stored is meaningless; it is
			// dropped rather than saved, so the literal string can never become
			// somebody's password.
			continue
		}
		merged[key] = value
	}
	return merged
}

// HasSentinel reports whether a configuration still carries a placeholder.
//
// Used where there is nothing stored to merge against — a trial run of a
// connection that has not been saved — so the caller can say "type the password
// to test this" rather than authenticating with the literal sentinel and
// reporting a confusing failure from the partner.
func HasSentinel(config map[string]any) bool {
	for _, value := range config {
		if text, ok := value.(string); ok && text == Sentinel {
			return true
		}
	}
	return false
}

func isEmpty(value any) bool {
	if value == nil {
		return true
	}
	text, ok := value.(string)
	return ok && text == ""
}
