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

// A nested transaction opens a savepoint and releases it, which is what
// makes the inner part undoable on its own.
func TestSavepoint_OpensAndReleases(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	err := db.Transaction(context.Background(), func(outer *orm.DB) error {
		return outer.Transaction(context.Background(), func(*orm.DB) error { return nil })
	})
	if err != nil {
		t.Fatalf("Transaction() error = %v", err)
	}

	var opened, released string
	for _, sql := range c.ExecCalls() {
		switch {
		case strings.HasPrefix(sql, "SAVEPOINT "):
			opened = sql
		case strings.HasPrefix(sql, "RELEASE SAVEPOINT "):
			released = sql
		}
	}
	if opened == "" || released == "" {
		t.Fatalf("ExecCalls() = %v, want a savepoint opened and released", c.ExecCalls())
	}
	if strings.TrimPrefix(opened, "SAVEPOINT ") != strings.TrimPrefix(released, "RELEASE SAVEPOINT ") {
		t.Errorf("opened %q but released %q", opened, released)
	}
	if !strings.Contains(opened, `"orm_sp_`) {
		t.Errorf("opened %q, want a quoted generated name", opened)
	}
}

// The point of a savepoint: the outer transaction can swallow an inner
// failure and still commit everything else.
func TestSavepoint_InnerFailureLeavesTheOuterCommittable(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	sentinel := errors.New("inner")
	err := db.Transaction(context.Background(), func(outer *orm.DB) error {
		// The inner work is worth having and not worth failing over.
		if err := outer.Transaction(context.Background(), func(*orm.DB) error {
			return sentinel
		}); !errors.Is(err, sentinel) {
			t.Errorf("inner error = %v, want the sentinel", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Transaction() error = %v, want the outer to survive", err)
	}
	if !c.Txs()[0].Committed {
		t.Error("the outer transaction did not commit")
	}
	if c.Txs()[0].RolledBack {
		t.Error("the outer transaction rolled back over an inner failure it swallowed")
	}

	var rolledTo bool
	for _, sql := range c.ExecCalls() {
		if strings.HasPrefix(sql, "ROLLBACK TO SAVEPOINT ") {
			rolledTo = true
		}
	}
	if !rolledTo {
		t.Errorf("ExecCalls() = %v, want the savepoint rolled back to", c.ExecCalls())
	}
}

// Two savepoints open at once never share a name, or rolling back to one
// would undo the other.
func TestSavepoint_NamesAreDistinct(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	err := db.Transaction(context.Background(), func(outer *orm.DB) error {
		return outer.Transaction(context.Background(), func(mid *orm.DB) error {
			return mid.Transaction(context.Background(), func(*orm.DB) error { return nil })
		})
	})
	if err != nil {
		t.Fatalf("Transaction() error = %v", err)
	}

	seen := map[string]bool{}
	for _, sql := range c.ExecCalls() {
		if name, ok := strings.CutPrefix(sql, "SAVEPOINT "); ok {
			if seen[name] {
				t.Errorf("savepoint %s opened twice at once", name)
			}
			seen[name] = true
		}
	}
	if len(seen) != 2 {
		t.Errorf("opened %d savepoints, want 2", len(seen))
	}
}

// A savepoint that cannot be opened is reported rather than run past.
func TestSavepoint_OpenFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.FailOnPrefix("SAVEPOINT ")
	db := orm.NewDB(c, postgres.Dialect{})

	ran := false
	err := db.Transaction(context.Background(), func(outer *orm.DB) error {
		return outer.Transaction(context.Background(), func(*orm.DB) error {
			ran = true
			return nil
		})
	})
	if err == nil {
		t.Fatal("Transaction() error = nil, want the failure reported")
	}
	if !strings.Contains(err.Error(), "opening a savepoint") {
		t.Errorf("error = %v, want it to name what failed", err)
	}
	if ran {
		t.Error("the nested work ran without a savepoint to undo it")
	}
}

// Releasing is the last thing a savepoint does, and a failure there is the
// caller's to hear: the work is still in the transaction, but the database
// said something went wrong.
func TestSavepoint_ReleaseFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.FailOnPrefix("RELEASE SAVEPOINT ")
	db := orm.NewDB(c, postgres.Dialect{})

	err := db.Transaction(context.Background(), func(outer *orm.DB) error {
		return outer.Transaction(context.Background(), func(*orm.DB) error { return nil })
	})
	if err == nil {
		t.Fatal("Transaction() error = nil, want the failure reported")
	}
	if !strings.Contains(err.Error(), "releasing a savepoint") {
		t.Errorf("error = %v, want it to name what failed", err)
	}
}

// Options belong to a transaction. A nested call is a savepoint inside one
// already open, so asking it for an isolation level is refused rather than
// quietly ignored.
func TestSavepoint_OptionsRejected(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	err := db.Transaction(context.Background(), func(outer *orm.DB) error {
		return outer.TransactionWith(context.Background(),
			orm.TxOptions{Isolation: orm.IsolationSerializable},
			func(*orm.DB) error { return nil })
	})
	if err == nil {
		t.Fatal("TransactionWith() error = nil, want the nested options refused")
	}
	if !strings.Contains(err.Error(), "savepoint") {
		t.Errorf("error = %v, want it to explain what a nested transaction is", err)
	}
}

// InTransaction answers the one question a *DB cannot otherwise be asked.
func TestInTransaction(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	if db.InTransaction() {
		t.Error("a fresh handle reports it is in a transaction")
	}
	err := db.Transaction(context.Background(), func(tx *orm.DB) error {
		if !tx.InTransaction() {
			t.Error("the transaction's handle reports it is not in one")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Transaction() error = %v", err)
	}
	var nilDB *orm.DB
	if nilDB.InTransaction() {
		t.Error("a nil handle reports it is in a transaction")
	}
}
