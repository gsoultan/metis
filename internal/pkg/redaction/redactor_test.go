package redaction

import (
	"errors"
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
