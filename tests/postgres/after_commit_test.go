package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// What is scheduled for after a commit is part of the work it follows: it runs
// when that work commits, and not when it is undone — whether the transaction
// rolls back or only a savepoint inside it does.
func TestWorkAfterACommitFollowsTheTransactionItWasScheduledIn(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 4)
	uow := repositories.NewRepository(testutils.StormConn(db)).UnitOfWork()
	ctx := entities.WithSystemContext(t.Context())

	var ran []string
	record := func(name string) func() { return func() { ran = append(ran, name) } }

	err := uow.Do(ctx, func(txCtx context.Context) error {
		uow.AfterCommit(txCtx, record("kept"))

		failed := uow.Attempt(txCtx, func(inner context.Context) error {
			uow.AfterCommit(inner, record("rolled back to a savepoint"))
			return errors.New("this attempt is undone")
		})
		if failed == nil {
			t.Error("the attempt should have failed")
		}

		kept := uow.Attempt(txCtx, func(inner context.Context) error {
			uow.AfterCommit(inner, record("kept by a savepoint"))
			return nil
		})
		if kept != nil {
			return kept
		}
		if len(ran) != 0 {
			t.Errorf("work ran before the transaction committed: %v", ran)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the transaction: %v", err)
	}
	if len(ran) != 2 || ran[0] != "kept" || ran[1] != "kept by a savepoint" {
		t.Fatalf("after the commit, ran %v; want [kept, kept by a savepoint]", ran)
	}

	ran = nil
	_ = uow.Do(ctx, func(txCtx context.Context) error {
		uow.AfterCommit(txCtx, record("rolled back"))
		return errors.New("undone")
	})
	if len(ran) != 0 {
		t.Fatalf("work scheduled in a transaction that rolled back still ran: %v", ran)
	}

	uow.AfterCommit(ctx, record("outside any transaction"))
	if len(ran) != 1 {
		t.Fatalf("outside a transaction the work it follows has committed, so it runs now: %v", ran)
	}
}
