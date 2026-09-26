package redaction

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// Both directions of the same rule: whatever can be a credential is redacted,
// and the words of a sentence are not — "missing or invalid token: the ID
// token names no issuer" is an error to read, not a token to hide.
func TestRedactText(t *testing.T) {
	t.Parallel()

	// Assembled rather than written down, so a secret scanner reading this file
	// does not take a fixture for a credential. Only the shapes matter.
	jwtLike := strings.Join([]string{"eyJhbGciOiJIUzI1NiJ9", "eyJzdWIiOiIxIn0", "c2lnbmF0dXJl"}, ".")
	apiKey := strings.Repeat("a1b2c3d4", 3)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text unchanged",
			input: "connection timeout",
			want:  "connection timeout",
		},
		{
			name:  "key value pair is redacted",
			input: "password=super-secret",
			want:  "password=***REDACTED***",
		},
		{
			name:  "bearer token is redacted",
			input: "Authorization: Bearer abc.def.ghi",
			want:  "Authorization: Bearer ***REDACTED***",
		},
		{
			name:  "json secret value is redacted",
			input: `{"token":"abc123"}`,
			want:  `{"token":"***REDACTED***"}`,
		},
		{
			name:  "dsn credentials are redacted",
			input: "postgres://alice:my-password@localhost:5432/app",
			want:  "postgres://alice:***REDACTED***@localhost:5432/app",
		},

		// A credential after "token:" and its kin, whatever follows it.
		{name: "a token after token:", input: "token: " + apiKey, want: "token: ***REDACTED***"},
		{name: "a JWT after token:", input: "token: " + jwtLike, want: "token: ***REDACTED***"},
		{name: "a token an error quotes", input: "invalid token: " + jwtLike, want: "invalid token: ***REDACTED***"},
		{name: "a token with a sentence after it", input: "token: " + apiKey + " was refused", want: "token: ***REDACTED*** was refused"},
		{name: "a token after a colon with no space", input: "token:" + apiKey, want: "token:***REDACTED***"},
		{name: "a password with a digit", input: "password: hunter2", want: "password: ***REDACTED***"},
		{name: "a password that is a word, ending the text", input: "password: letmein", want: "password: ***REDACTED***"},
		{name: "a password that is a word, ending the line", input: "password: letmein\nhost: db.internal", want: "password: ***REDACTED***\nhost: db.internal"},
		{name: "a secret before a comma", input: "secret: s3cr3t, user: bob", want: "secret: ***REDACTED***, user: bob"},
		{name: "a word before a comma", input: "secret: letmein, user: bob", want: "secret: ***REDACTED***, user: bob"},
		{name: "a quoted password", input: `password: "letmein" was rejected`, want: `password: ***REDACTED*** was rejected`},
		{name: "a word in mixed case", input: "api_key: AbCdEfGhIjKl was rejected", want: "api_key: ***REDACTED*** was rejected"},
		{name: "a map printed with %v", input: "map[password:letmein user:svc]", want: "map[password:***REDACTED*** user:svc]"},
		// A password or secret can be a plain word, and a log line can go on
		// after it. Only the token's name starts sentences in Go errors; the
		// others keep a word only when it opens the next link of an error chain.
		{name: "a plain-word password in a sentence", input: "password: letmein was rejected", want: "password: ***REDACTED*** was rejected"},
		{name: "a plain-word secret in a sentence", input: "secret: hunter was wrong for bob", want: "secret: ***REDACTED*** was wrong for bob"},
		{name: "a plain-word API key in a sentence", input: "api_key: sesame was refused", want: "api_key: ***REDACTED*** was refused"},
		{name: "a capitalised word after a secret's name", input: "secret: Rotation is overdue for this connection", want: "secret: ***REDACTED*** is overdue for this connection"},
		{name: "a struct printed with %+v", input: "{User:svc Password:letmein}", want: "{User:svc Password:***REDACTED***"},
		{name: "a lone word ending the text, which a password can be", input: "invalid token: expired", want: "invalid token: ***REDACTED***"},
		{name: "a secret's name inside a sentence, then a token", input: "failed to verify token: jwt: " + jwtLike, want: "failed to verify token: jwt: ***REDACTED***"},
		{name: "a token after a sentence that names one", input: "the token: the token: " + apiKey, want: "the token: the token: ***REDACTED***"},

		// token=, in JSON, in a URL query, in an Authorization header.
		{name: "a token after token=", input: "token=" + apiKey, want: "token=***REDACTED***"},
		{name: "a word after token=", input: "token=letmein and more", want: "token=***REDACTED*** and more"},
		{name: "a password in a connection string", input: "host=db user=svc password=letmein dbname=crm", want: "host=db user=svc password=***REDACTED*** dbname=crm"},
		{name: "JSON with spaces", input: `{"password": "letmein", "user": "svc"}`, want: `{"password": "***REDACTED***", "user": "svc"}`},
		{name: "an access token in JSON", input: `{"access_token":"` + jwtLike + `","token_type":"Bearer"}`, want: `{"access_token":"***REDACTED***","token_type":"Bearer"}`},
		{name: "a key in a URL query", input: "GET https://api.example.com/v1/leads?api_key=" + apiKey + " failed", want: "GET https://api.example.com/v1/leads?api_key=***REDACTED*** failed"},
		{name: "an access token in a URL query", input: "GET https://api.example.com/v1?access_token=" + apiKey + " failed", want: "GET https://api.example.com/v1?access_token=***REDACTED*** failed"},
		{name: "a JWT in an Authorization header", input: "Authorization: Bearer " + jwtLike, want: "Authorization: Bearer ***REDACTED***"},
		{name: "a lower-case Authorization header", input: "authorization: bearer " + apiKey, want: "authorization: bearer ***REDACTED***"},

		// The words of an error after a secret's name, kept.
		{
			name:  "an unauthorized error explaining itself",
			input: "unauthorized: missing or invalid token: the ID token names no issuer or no subject",
			want:  "unauthorized: missing or invalid token: the ID token names no issuer or no subject",
		},
		{
			name:  "the tenant resolver's refusal",
			input: "tenant: unauthorized: missing or invalid token: the authenticated principal could not be resolved to an organization",
			want:  "tenant: unauthorized: missing or invalid token: the authenticated principal could not be resolved to an organization",
		},
		{
			name:  "an expired local token",
			input: "invalid token: token has invalid claims: token is expired",
			want:  "invalid token: token has invalid claims: token is expired",
		},
		{
			name:  "a token for an account that is gone",
			input: "invalid token: no such user",
			want:  "invalid token: no such user",
		},
		{
			name:  "a token from before a password change",
			input: "invalid token: issued before the password was changed",
			want:  "invalid token: issued before the password was changed",
		},
		{
			name:  "an expired ID token",
			input: "failed to verify token: oidc: token is expired (Token Expiry: 2026-09-26 10:00:00 +0000 UTC)",
			want:  "failed to verify token: oidc: token is expired (Token Expiry: 2026-09-26 10:00:00 +0000 UTC)",
		},
		{
			name:  "an ID token signed with the wrong algorithm",
			input: `failed to verify token: oidc: malformed jwt: go-jose/go-jose: unexpected signature algorithm "HS256"; expected ["RS256"]`,
			want:  `failed to verify token: oidc: malformed jwt: go-jose/go-jose: unexpected signature algorithm "HS256"; expected ["RS256"]`,
		},
		{
			name:  "a password bcrypt will not hash",
			input: "could not hash the new password: bcrypt: password length exceeds 72 bytes",
			want:  "could not hash the new password: bcrypt: password length exceeds 72 bytes",
		},
		{
			name:  "an encryption key refused at boot",
			input: "invalid ENCRYPTION_KEY: crypto: encryption passphrase must not be empty",
			want:  "invalid ENCRYPTION_KEY: crypto: encryption passphrase must not be empty",
		},
		{
			name:  "a word in capitals",
			input: "unauthorized: missing or invalid token: ID token has no subject",
			want:  "unauthorized: missing or invalid token: ID token has no subject",
		},
		{
			name:  "a word with an apostrophe",
			input: "token: doesn't match the one issued",
			want:  "token: doesn't match the one issued",
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := RedactText(tt.input)
			if got != tt.want {
				t.Fatalf("RedactText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRedactError(t *testing.T) {
	t.Parallel()

	err := errors.New("failed with password=top-secret")
	if got := RedactError(err); got != "failed with password=***REDACTED***" {
		t.Fatalf("RedactError() = %q, want %q", got, "failed with password=***REDACTED***")
	}

	if got := RedactError(nil); got != "" {
		t.Fatalf("RedactError(nil) = %q, want empty string", got)
	}
}

func TestGetPatternsCachesCompiledRegexps(t *testing.T) {
	t.Parallel()

	first := getPatterns()
	if first == nil {
		t.Fatal("getPatterns() returned nil on first call")
	}

	second := getPatterns()
	if second == nil {
		t.Fatal("getPatterns() returned nil on second call")
	}

	if first != second {
		t.Fatal("getPatterns() did not reuse compiled regex patterns")
	}
}

func TestGetPatternsConcurrentCallsReuseSingleCache(t *testing.T) {
	t.Parallel()

	const workers = 32
	results := make(chan *patterns, workers)

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			results <- getPatterns()
		})
	}
	wg.Wait()
	close(results)

	var first *patterns
	for result := range results {
		if result == nil {
			t.Fatal("getPatterns() returned nil in concurrent call")
		}

		if first == nil {
			first = result
			continue
		}

		if result != first {
			t.Fatal("getPatterns() returned multiple cache instances under concurrency")
		}
	}
}

// A database lookup's connection string is every credential for that database
// at once, and a driver error or an incident may carry one. Each form the three
// drivers accept has to come out without its password.
func TestEveryConnectionStringFormLosesItsPassword(t *testing.T) {
	for name, dsn := range map[string]string{
		"libpq keywords":      "host=db.internal user=svc password=hunter2 dbname=crm",
		"PostgreSQL URL":      "postgres://svc:hunter2@db.internal:5432/crm?sslmode=require",
		"SQL Server URL":      "sqlserver://svc:hunter2@db.internal:1433?database=crm",
		"SQL Server ADO":      "server=db.internal;user id=svc;Password=hunter2;database=crm",
		"SQL Server ODBC pwd": "server=db.internal;uid=svc;pwd=hunter2;database=crm",
		"MySQL":               "svc:hunter2@tcp(db.internal:3306)/crm?parseTime=true",
		"MySQL, default host": "svc:hunter2@/crm",
		"MySQL, an @ in it":   "svc:hun@ter2@tcp(db.internal:3306)/crm",
		"MySQL, a socket":     "svc:hunter2@unix(/var/run/mysqld.sock)/crm",
	} {
		t.Run(name, func(t *testing.T) {
			got := RedactText("could not connect with " + dsn + ": refused")
			if strings.Contains(got, "hunter2") || strings.Contains(got, "ter2") {
				t.Fatalf("the password survived: %s", got)
			}
			if !strings.Contains(got, "refused") {
				t.Fatalf("redaction took the rest of the message too: %s", got)
			}
		})
	}
}

// The MySQL form must not eat ordinary text that has a colon and an @ in it.
func TestTheMySQLFormLeavesOrdinaryTextAlone(t *testing.T) {
	for _, text := range []string{
		"step 3: notify ops@example.com",
		"ratio 3:2 at noon",
	} {
		if got := RedactText(text); got != text {
			t.Errorf("%q became %q", text, got)
		}
	}
}
