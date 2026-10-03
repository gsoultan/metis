package db

import (
	"context"
	"testing"

	"github.com/gsoultan/storm/runtime"
)

// A write that has to be inside a transaction asks whether one is open, and
// TransactMain's nil transaction means none is.
func TestInTransactionIsTrueOnlyForAnOpenTransaction(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want bool
	}{
		{"a bare context", context.Background(), false},
		{"the nil transaction TransactMain places", withTx(context.Background(), nil), false},
		{"an open transaction", withTx(context.Background(), runtime.Executor(stubExecutor{})), true},
	} {
		if got := InTransaction(tc.ctx); got != tc.want {
			t.Errorf("%s: InTransaction = %v, want %v", tc.name, got, tc.want)
		}
	}
}

type stubExecutor struct{ runtime.Executor }
