package redaction

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestRedactText(t *testing.T) {
	t.Parallel()

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
