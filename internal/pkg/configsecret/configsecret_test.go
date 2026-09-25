package configsecret

import (
	"strings"
	"testing"
)

func TestSensitiveKeysAreRecognisedHoweverTheyAreSpelled(t *testing.T) {
	// Connector authors name these every way there is, so the match is on
	// substrings and case-insensitive.
	for _, key := range []string{
		"password", "Password", "smtp_password",
		"token", "authToken", "AUTH_TOKEN",
		"apiKey", "api_key", "API-KEY",
		"secret", "clientSecret", "webhook_secret",
		"privateKey", "signature", "credentials",
	} {
		if !IsSensitive(key) {
			t.Errorf("%q was not treated as a secret", key)
		}
	}
}

func TestAConnectionStringIsASecret(t *testing.T) {
	// A connection string carries the database password, and a directory
	// source's dsn was returned to the browser in clear because nothing in the
	// list matched it. Every spelling a connector author reaches for.
	for _, key := range []string{
		"dsn", "DSN", "source_dsn", "databaseDsn",
		"connection_string", "connectionString", "Connection-String", "connection string",
		"conn_string", "connString",
		"connection_uri", "connectionUri",
	} {
		if !IsSensitive(key) {
			t.Errorf("%q was not treated as a secret", key)
		}
	}
}

func TestAWebhookURLIsASecret(t *testing.T) {
	// Slack, Discord and Teams authenticate a post by the URL alone, so the
	// URL is the credential. The catalogue declared these as password fields,
	// which only changed the input the form drew.
	for _, key := range []string{"webhook_url", "webhookUrl", "WEBHOOK-URL"} {
		if !IsSensitive(key) {
			t.Errorf("%q was not treated as a secret", key)
		}
	}
}

// TestNormalisingAKeyNeverUnmasksOne holds the new matcher to the one it
// replaced: every key the old list masked, the new one must mask too. The old
// matcher is written out here, frozen, because the point is to compare against
// what shipped rather than against whatever the list says today.
func TestNormalisingAKeyNeverUnmasksOne(t *testing.T) {
	previous := []string{
		"secret", "password", "passwd", "token", "apikey", "api_key",
		"credential", "private", "signature", "key",
	}
	maskedBefore := func(key string) bool {
		lower := strings.ToLower(key)
		for _, fragment := range previous {
			if strings.Contains(lower, fragment) {
				return true
			}
		}
		return false
	}

	corpus := []string{
		"password", "Password", "smtp_password", "token", "authToken", "AUTH_TOKEN",
		"apiKey", "api_key", "API-KEY", "API_KEY", "x-api_key-header", "secret",
		"clientSecret", "webhook_secret", "privateKey", "private_key", "signature",
		"credentials", "routing_key", "signing_key", "bot_token", "passwd",
		"url", "channel", "from", "port", "host", "method", "timeout", "username",
	}
	for _, key := range corpus {
		if maskedBefore(key) && !IsSensitive(key) {
			t.Errorf("%q was masked before normalisation and is not now", key)
		}
	}
}

func TestSensitiveFragmentsIsACopy(t *testing.T) {
	// The drift test reads this; a caller editing the slice it got back must
	// not be able to change what the server masks.
	fragments := SensitiveFragments()
	fragments[0] = "nothing-matches-this"
	if SensitiveFragments()[0] == "nothing-matches-this" {
		t.Fatal("SensitiveFragments returned the package's own slice")
	}
}

func TestOrdinarySettingsAreNotMasked(t *testing.T) {
	// The person is on this page to check these. Masking them would make the
	// form useless.
	for _, key := range []string{
		"url", "channel", "from", "port", "host", "method", "timeout",
		// The database lookup connector's own settings: the person needs to
		// read these to know what the connection will do.
		"driver", "statement_timeout_ms", "max_rows", "max_result_bytes",
	} {
		if IsSensitive(key) {
			t.Errorf("%q was treated as a secret", key)
		}
	}
}

func TestMaskReplacesSecretsAndKeepsTheRest(t *testing.T) {
	config := map[string]any{
		"url":      "https://hooks.example.com/abc",
		"password": "hunter2",
		"port":     587,
	}
	masked := Mask(config)

	if masked["password"] != Sentinel {
		t.Fatalf("password was returned as %v", masked["password"])
	}
	if masked["url"] != "https://hooks.example.com/abc" {
		t.Fatalf("url was altered: %v", masked["url"])
	}
	if masked["port"] != 587 {
		t.Fatalf("port was altered: %v", masked["port"])
	}
	// The original must be untouched: it may be the map the executor is about
	// to authenticate with.
	if config["password"] != "hunter2" {
		t.Fatal("Mask edited the caller's map in place")
	}
}

func TestAnUnsetSecretStaysEmpty(t *testing.T) {
	// A field nobody has filled in should read as blank, not as a stored
	// credential the person is afraid to touch.
	masked := Mask(map[string]any{"password": ""})
	if masked["password"] != "" {
		t.Fatalf("an empty secret was masked as %v", masked["password"])
	}
}

func TestMergeKeepsWhatWasNotRetyped(t *testing.T) {
	stored := map[string]any{"url": "https://old.example.com", "password": "hunter2"}
	incoming := map[string]any{"url": "https://new.example.com", "password": Sentinel}

	merged := Merge(incoming, stored)
	if merged["password"] != "hunter2" {
		t.Fatalf("the stored password was not kept: %v", merged["password"])
	}
	if merged["url"] != "https://new.example.com" {
		t.Fatalf("the edited url was not applied: %v", merged["url"])
	}
}

func TestMergeAppliesARealReplacement(t *testing.T) {
	merged := Merge(
		map[string]any{"password": "a-new-one"},
		map[string]any{"password": "hunter2"},
	)
	if merged["password"] != "a-new-one" {
		t.Fatalf("a typed replacement was not applied: %v", merged["password"])
	}
}

/*
 * Clearing a credential has to be possible.
 *
 * Treating an empty string as "unchanged" — the obvious shortcut — would make a
 * stored secret impossible to remove through the interface, which is exactly
 * what somebody does when a key leaks.
 */
func TestMergeCanClearASecret(t *testing.T) {
	merged := Merge(
		map[string]any{"password": ""},
		map[string]any{"password": "hunter2"},
	)
	if merged["password"] != "" {
		t.Fatalf("an emptied secret was not cleared: %v", merged["password"])
	}
}

func TestTheSentinelCanNeverBecomeAStoredValue(t *testing.T) {
	// A sentinel for a key that was never stored means nothing; saving it
	// literally would make "__unchanged__" somebody's actual password.
	merged := Merge(map[string]any{"password": Sentinel}, map[string]any{})
	if _, present := merged["password"]; present {
		t.Fatalf("the sentinel was stored: %v", merged["password"])
	}
}

func TestMergeDropsOmittedKeys(t *testing.T) {
	// The form sends the whole configuration, so a key it left out was removed.
	merged := Merge(map[string]any{"url": "x"}, map[string]any{"url": "y", "gone": "z"})
	if _, present := merged["gone"]; present {
		t.Fatal("a removed key survived the merge")
	}
}

func TestHasSentinelSpotsAnUntestableConfig(t *testing.T) {
	if !HasSentinel(map[string]any{"password": Sentinel}) {
		t.Fatal("a placeholder was not spotted")
	}
	if HasSentinel(map[string]any{"password": "real"}) {
		t.Fatal("a real value was reported as a placeholder")
	}
}

func TestNilIsCarriedThrough(t *testing.T) {
	if Mask(nil) != nil {
		t.Fatal("Mask invented a map")
	}
	if Merge(nil, map[string]any{"a": 1}) != nil {
		t.Fatal("Merge invented a map")
	}
}
