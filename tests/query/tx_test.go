package query_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

func TestTransaction_CommitsOnSuccess(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	err := db.Transaction(context.Background(), func(tx *orm.DB) error {
		return Users.With(tx).Update(context.Background(), &User{ID: 1, Username: "alice"})
	})
	if err != nil {
		t.Fatalf("Transaction() error = %v", err)
	}

	txs := c.Txs()
	if len(txs) != 1 {
		t.Fatalf("started %d transactions, want 1", len(txs))
	}
	if !txs[0].Committed {
		t.Error("the transaction was not committed")
	}
	if txs[0].RolledBack {
		t.Error("the transaction was rolled back as well as committed")
	}
	if len(c.ExecCalls()) != 1 {
		t.Errorf("ran %v, want the update to have gone through the transaction", c.ExecCalls())
	}
}

func TestTransaction_RollsBackOnError(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	sentinel := errors.New("the caller changed its mind")
	err := db.Transaction(context.Background(), func(tx *orm.DB) error {
		if err := Users.With(tx).Update(context.Background(), &User{ID: 1}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Transaction() error = %v, want the callback's own error", err)
	}

	txs := c.Txs()
	if len(txs) != 1 {
		t.Fatalf("started %d transactions, want 1", len(txs))
	}
	if !txs[0].RolledBack {
		t.Error("the transaction was not rolled back")
	}
	if txs[0].Committed {
		t.Error("the transaction was committed despite the error")
	}
}

// A panic must not leave the transaction open. It would hold whatever locks
// its statements had taken until the connection closed.
func TestTransaction_RollsBackOnPanicAndKeepsPanicking(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("the panic did not reach the caller")
			}
			if got, ok := r.(string); !ok || got != "boom" {
				t.Errorf("recovered %v, want the original panic", r)
			}
		}()
		_ = db.Transaction(context.Background(), func(tx *orm.DB) error {
			panic("boom")
		})
	}()

	txs := c.Txs()
	if len(txs) != 1 {
		t.Fatalf("started %d transactions, want 1", len(txs))
	}
	if !txs[0].RolledBack {
		t.Error("the transaction was left open after a panic")
	}
}

// A handle that is already a transaction has no connection to start a second
// one from, so an inner call joins the outer one.
func TestTransaction_JoinsRatherThanNests(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	var innerHandle, outerHandle *orm.DB
	err := db.Transaction(context.Background(), func(outer *orm.DB) error {
		outerHandle = outer
		return outer.Transaction(context.Background(), func(inner *orm.DB) error {
			innerHandle = inner
			return nil
		})
	})
	if err != nil {
		t.Fatalf("Transaction() error = %v", err)
	}
	if len(c.Txs()) != 1 {
		t.Errorf("started %d transactions, want 1: the inner call should join", len(c.Txs()))
	}
	if innerHandle != outerHandle {
		t.Error("the inner call was given a different handle, so it did not join")
	}
	if !c.Txs()[0].Committed {
		t.Error("the joined transaction was not committed")
	}
}

// An inner failure rolls the whole thing back, which is the consequence of
// joining rather than nesting and is worth pinning down.
func TestTransaction_InnerErrorRollsBackTheOuter(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	sentinel := errors.New("inner")
	err := db.Transaction(context.Background(), func(outer *orm.DB) error {
		return outer.Transaction(context.Background(), func(inner *orm.DB) error {
			return sentinel
		})
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Transaction() error = %v, want the inner error", err)
	}
	if !c.Txs()[0].RolledBack {
		t.Error("the outer transaction survived an inner failure")
	}
}

func TestTransaction_BeginFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.FailBegin = true
	db := orm.NewDB(c, postgres.Dialect{})

	ran := false
	err := db.Transaction(context.Background(), func(tx *orm.DB) error {
		ran = true
		return nil
	})
	if err == nil {
		t.Fatal("Transaction() error = nil, want the Begin failure")
	}
	if !strings.Contains(err.Error(), "beginning a transaction") {
		t.Errorf("error %q does not say what failed", err)
	}
	if ran {
		t.Error("the callback ran even though no transaction was started")
	}
}

func TestTransaction_CommitFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.FailCommit = true
	db := orm.NewDB(c, postgres.Dialect{})

	err := db.Transaction(context.Background(), func(tx *orm.DB) error { return nil })
	if err == nil {
		t.Fatal("Transaction() error = nil, want the Commit failure")
	}
	if !strings.Contains(err.Error(), "committing") {
		t.Errorf("error %q does not say what failed", err)
	}
	// A commit that failed leaves the transaction open, so it is still rolled
	// back rather than abandoned.
	if !c.Txs()[0].RolledBack {
		t.Error("a failed commit left the transaction open")
	}
}

// A handle with nothing behind it reports that rather than panicking.
func TestTransaction_NoHandle(t *testing.T) {
	var db orm.DB
	err := db.Transaction(context.Background(), func(tx *orm.DB) error { return nil })
	if err == nil {
		t.Fatal("Transaction() error = nil, want a missing handle error")
	}
	if !strings.Contains(err.Error(), "no database handle") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// The handle the callback receives is an ordinary one, so everything else in
// the API works inside unchanged. That is the property the whole design
// rests on, so it is asserted directly rather than implied by other tests.
func TestTransaction_HandleIsAnOrdinaryDB(t *testing.T) {
	ctx := context.Background()
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	c.QueueRows([]any{int64(7)}) // the count
	c.QueueRows([]any{11})       // the id the insert reads back
	db := orm.NewDB(c, postgres.Dialect{})

	err := db.Transaction(ctx, func(tx *orm.DB) error {
		if _, err := Users.With(tx).Count(ctx); err != nil {
			return err
		}
		u := &User{Username: "alice"}
		if err := Users.With(tx).Insert(ctx, u); err != nil {
			return err
		}
		if u.ID != 11 {
			t.Errorf("ID = %d, want the key read back inside the transaction", u.ID)
		}
		u.Age = 30
		return Users.With(tx).Update(ctx, u)
	})
	if err != nil {
		t.Fatalf("Transaction() error = %v", err)
	}
	if len(c.Txs()) != 1 || !c.Txs()[0].Committed {
		t.Errorf("started %d transactions, want 1 committed", len(c.Txs()))
	}
	// Reading, inserting and updating all went through the one transaction.
	if len(c.QueryCalls()) != 2 || len(c.ExecCalls()) != 1 {
		t.Errorf("ran %v and %v, want the count, the insert and the update",
			c.QueryCalls(), c.ExecCalls())
	}
}
