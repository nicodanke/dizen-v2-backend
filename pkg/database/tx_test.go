package database_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/nicodanke/dizen-v2-backend/pkg/database"
)

// fakeTx records what WithTx did to it. Only the methods the helper touches carry
// behavior; the rest satisfy pgx.Tx and are never called.
type fakeTx struct {
	pgx.Tx

	committed  bool
	rolledBack bool

	commitErr   error
	rollbackErr error
}

func (f *fakeTx) Commit(context.Context) error {
	f.committed = true

	return f.commitErr
}

func (f *fakeTx) Rollback(context.Context) error {
	f.rolledBack = true

	return f.rollbackErr
}

// fakeBeginner hands out a prepared transaction.
type fakeBeginner struct {
	tx       *fakeTx
	beginErr error
}

func (f *fakeBeginner) Begin(context.Context) (pgx.Tx, error) {
	if f.beginErr != nil {
		return nil, f.beginErr
	}

	return f.tx, nil
}

var errBusiness = errors.New("the business rule rejected it")

func TestWithTxCommitsOnSuccess(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	beginner := &fakeBeginner{tx: tx}

	var ran bool

	err := database.WithTx(t.Context(), beginner, func(pgx.Tx) error {
		ran = true

		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	if !ran {
		t.Error("the function never ran")
	}

	if !tx.committed {
		t.Error("it did not commit")
	}

	if tx.rolledBack {
		t.Error("it rolled back a successful transaction")
	}
}

// The point of the helper: a caller cannot commit a transaction whose function failed.
func TestWithTxRollsBackOnError(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	beginner := &fakeBeginner{tx: tx}

	err := database.WithTx(t.Context(), beginner, func(pgx.Tx) error {
		return errBusiness
	})

	if !errors.Is(err, errBusiness) {
		t.Errorf("the original error was lost: %v", err)
	}

	if tx.committed {
		t.Error("it committed a failed transaction")
	}

	if !tx.rolledBack {
		t.Error("it did not roll back")
	}
}

// A panic inside the transaction has to roll back and keep unwinding. This matters for the
// outbox (RF-12): an event written in a transaction that was never closed would block the
// rows behind it.
func TestWithTxRollsBackOnPanicAndRepanics(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	beginner := &fakeBeginner{tx: tx}

	defer func() {
		r := recover()
		if r == nil {
			t.Error("the panic did not propagate")
		}

		if r != "boom" {
			t.Errorf("the panic value changed: %v", r)
		}

		if !tx.rolledBack {
			t.Error("it did not roll back on panic")
		}

		if tx.committed {
			t.Error("it committed despite the panic")
		}
	}()

	_ = database.WithTx(t.Context(), beginner, func(pgx.Tx) error {
		panic("boom")
	})
}

func TestWithTxReportsAFailureToBegin(t *testing.T) {
	t.Parallel()

	beginner := &fakeBeginner{beginErr: errors.New("too many connections")}

	var ran bool

	err := database.WithTx(t.Context(), beginner, func(pgx.Tx) error {
		ran = true

		return nil
	})

	if err == nil {
		t.Fatal("WithTx succeeded without a transaction")
	}

	if ran {
		t.Error("the function ran without a transaction")
	}
}

func TestWithTxReportsAFailureToCommit(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{commitErr: errors.New("deadlock detected")}
	beginner := &fakeBeginner{tx: tx}

	err := database.WithTx(t.Context(), beginner, func(pgx.Tx) error { return nil })
	if err == nil {
		t.Fatal("a failed commit was reported as success")
	}
}

// When both the function and the rollback fail, the original error must survive: it is the
// one that explains what actually went wrong.
func TestWithTxKeepsTheOriginalErrorWhenTheRollbackAlsoFails(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{rollbackErr: errors.New("connection lost")}
	beginner := &fakeBeginner{tx: tx}

	err := database.WithTx(t.Context(), beginner, func(pgx.Tx) error {
		return errBusiness
	})

	if !errors.Is(err, errBusiness) {
		t.Errorf("the original error was lost: %v", err)
	}

	if !strings.Contains(err.Error(), "rollback also failed") {
		t.Errorf("the rollback failure is not reported: %v", err)
	}
}

// A transaction pgx already closed on cancellation is not a failure worth reporting.
func TestWithTxIgnoresAnAlreadyClosedTransaction(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{rollbackErr: pgx.ErrTxClosed}
	beginner := &fakeBeginner{tx: tx}

	err := database.WithTx(t.Context(), beginner, func(pgx.Tx) error {
		return errBusiness
	})

	if !errors.Is(err, errBusiness) {
		t.Errorf("the original error was lost: %v", err)
	}

	if strings.Contains(err.Error(), "rollback also failed") {
		t.Errorf("ErrTxClosed was reported as a rollback failure: %v", err)
	}
}
