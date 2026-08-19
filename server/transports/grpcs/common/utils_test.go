package common

import (
	"errors"
	"testing"
)

func TestErrString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "nil error",
			err:  nil,
			want: "",
		},
		{
			name: "redacts secret",
			err:  errors.New("connection failed password=secret123"),
			want: "connection failed password=***REDACTED***",
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ErrString(tt.err); got != tt.want {
				t.Fatalf("ErrString() = %q, want %q", got, tt.want)
			}
		})
	}
}
