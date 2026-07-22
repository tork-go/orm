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

// The options are applied as the transaction's first statement, before
// anything the caller does can have read at the wrong level.
func TestTxOptions_Renders(t *testing.T) {
	tests := []struct {
		name string
		opts orm.TxOptions
		want string
	}{
		{"read committed", orm.TxOptions{Isolation: orm.IsolationReadCommitted},
			"SET TRANSACTION ISOLATION LEVEL READ COMMITTED"},
		{"repeatable read", orm.TxOptions{Isolation: orm.IsolationRepeatableRead},
			"SET TRANSACTION ISOLATION LEVEL REPEATABLE READ"},
		{"serializable", orm.TxOptions{Isolation: orm.IsolationSerializable},
			"SET TRANSACTION ISOLATION LEVEL SERIALIZABLE"},
		{"read only", orm.TxOptions{ReadOnly: true},
			"SET TRANSACTION READ ONLY"},
		{"both", orm.TxOptions{Isolation: orm.IsolationSerializable, ReadOnly: true},
			"SET TRANSACTION ISOLATION LEVEL SERIALIZABLE READ ONLY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fakedriver.NewConn()
			db := orm.NewDB(c, postgres.Dialect{})
			if err := db.TransactionWith(context.Background(), tt.opts,
				func(*orm.DB) error { return nil }); err != nil {
				t.Fatalf("TransactionWith() error = %v", err)
			}
			calls := c.ExecCalls()
			if len(calls) == 0 || calls[0] != tt.want {
				t.Errorf("ExecCalls() = %v\nwant it to start with %q", calls, tt.want)
			}
		})
	}
}

// The zero options ask for nothing, so the transaction carries no clause at
// all — which is every Transaction call.
func TestTxOptions_ZeroAsksForNothing(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})
	if err := db.Transaction(context.Background(), func(*orm.DB) error { return nil }); err != nil {
		t.Fatalf("Transaction() error = %v", err)
	}
	for _, sql := range c.ExecCalls() {
		if strings.HasPrefix(sql, "SET TRANSACTION") {
			t.Errorf("ExecCalls() = %v, want no transaction options set", c.ExecCalls())
		}
	}
}

// Another dialect spells it differently, which is why it is the dialect's to
// write.
func TestTxOptions_DialectSpellsIt(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, fakedriver.NewDialect())
	if err := db.TransactionWith(context.Background(),
		orm.TxOptions{Isolation: orm.IsolationSerializable, ReadOnly: true},
		func(*orm.DB) error { return nil }); err != nil {
		t.Fatalf("TransactionWith() error = %v", err)
	}
	if got := c.ExecCalls(); len(got) == 0 || got[0] != "SET TX LEVEL SERIALIZABLE NO WRITES" {
		t.Errorf("ExecCalls() = %v", got)
	}
}

// A database with one isolation level says so rather than running at another.
func TestTxOptions_DialectWithout(t *testing.T) {
	d := fakedriver.NewDialect()
	d.NoIsolation = true
	db := orm.NewDB(fakedriver.NewConn(), d)

	err := db.TransactionWith(context.Background(),
		orm.TxOptions{Isolation: orm.IsolationSerializable}, func(*orm.DB) error { return nil })
	if err == nil {
		t.Fatal("TransactionWith() error = nil, want the level reported as unavailable")
	}
	if !strings.Contains(err.Error(), "isolation") {
		t.Errorf("error = %v, want it to name the operation", err)
	}
}

// A transaction refused for a conflict is run again, and the retry commits.
func TestTxOptions_RetriesAConflict(t *testing.T) {
	conflict := errors.New("could not serialize access")
	d := fakedriver.NewDialect()
	d.RetryableErr = conflict
	c := fakedriver.NewConn()
	db := orm.NewDB(c, d)

	attempts := 0
	err := db.TransactionWith(context.Background(), orm.TxOptions{Retries: 3},
		func(*orm.DB) error {
			attempts++
			if attempts < 3 {
				return conflict
			}
			return nil
		})
	if err != nil {
		t.Fatalf("TransactionWith() error = %v", err)
	}
	if attempts != 3 {
		t.Errorf("ran fn %d times, want 3", attempts)
	}
	// Each attempt is its own transaction: the failed ones rolled back.
	if len(c.Txs()) != 3 {
		t.Errorf("started %d transactions, want one per attempt", len(c.Txs()))
	}
	if !c.Txs()[0].RolledBack || !c.Txs()[2].Committed {
		t.Error("want the first attempt rolled back and the last committed")
	}
}

// Retries run out, and the last failure is what the caller sees.
func TestTxOptions_RetriesExhausted(t *testing.T) {
	conflict := errors.New("could not serialize access")
	d := fakedriver.NewDialect()
	d.RetryableErr = conflict
	db := orm.NewDB(fakedriver.NewConn(), d)

	attempts := 0
	err := db.TransactionWith(context.Background(), orm.TxOptions{Retries: 2},
		func(*orm.DB) error {
			attempts++
			return conflict
		})
	if !errors.Is(err, conflict) {
		t.Fatalf("TransactionWith() error = %v, want the conflict", err)
	}
	if attempts != 3 {
		t.Errorf("ran fn %d times, want the first attempt and two retries", attempts)
	}
}

// An error the database did not raise as a conflict fails the same way
// however many times it runs, so it is not retried.
func TestTxOptions_DoesNotRetryOtherErrors(t *testing.T) {
	d := fakedriver.NewDialect()
	d.RetryableErr = errors.New("could not serialize access")
	db := orm.NewDB(fakedriver.NewConn(), d)

	other := errors.New("constraint violation")
	attempts := 0
	err := db.TransactionWith(context.Background(), orm.TxOptions{Retries: 5},
		func(*orm.DB) error {
			attempts++
			return other
		})
	if !errors.Is(err, other) {
		t.Fatalf("TransactionWith() error = %v", err)
	}
	if attempts != 1 {
		t.Errorf("ran fn %d times, want 1: nothing here is worth retrying", attempts)
	}
}

// A cancelled context stops the retrying rather than running fn again
// against a context that is already done.
func TestTxOptions_StopsRetryingWhenTheContextEnds(t *testing.T) {
	conflict := errors.New("could not serialize access")
	d := fakedriver.NewDialect()
	d.RetryableErr = conflict
	db := orm.NewDB(fakedriver.NewConn(), d)

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := db.TransactionWith(ctx, orm.TxOptions{Retries: 5}, func(*orm.DB) error {
		attempts++
		cancel()
		return conflict
	})
	if !errors.Is(err, conflict) {
		t.Fatalf("TransactionWith() error = %v", err)
	}
	if attempts != 1 {
		t.Errorf("ran fn %d times, want it to stop once the context ended", attempts)
	}
}

func TestTxOptions_NegativeRetries(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	err := db.TransactionWith(context.Background(), orm.TxOptions{Retries: -1},
		func(*orm.DB) error { return nil })
	if err == nil {
		t.Fatal("TransactionWith() error = nil, want the negative count refused")
	}
	if !strings.Contains(err.Error(), "negative") {
		t.Errorf("error = %v", err)
	}
}

// The options statement failing is reported before the caller's own work
// runs at a level nobody asked for.
func TestTxOptions_SetupFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.FailOn("SET TRANSACTION ISOLATION LEVEL SERIALIZABLE")
	db := orm.NewDB(c, postgres.Dialect{})

	ran := false
	err := db.TransactionWith(context.Background(),
		orm.TxOptions{Isolation: orm.IsolationSerializable},
		func(*orm.DB) error { ran = true; return nil })
	if err == nil {
		t.Fatal("TransactionWith() error = nil, want the failure reported")
	}
	if ran {
		t.Error("fn ran at a level the database refused to set")
	}
}

func TestIsolation_String(t *testing.T) {
	for _, tt := range []struct {
		iso  orm.Isolation
		want string
	}{
		{orm.IsolationDefault, "DEFAULT"},
		{orm.IsolationReadCommitted, "READ COMMITTED"},
		{orm.IsolationRepeatableRead, "REPEATABLE READ"},
		{orm.IsolationSerializable, "SERIALIZABLE"},
	} {
		if got := tt.iso.String(); got != tt.want {
			t.Errorf("Isolation(%d).String() = %q, want %q", tt.iso, got, tt.want)
		}
	}
}

// The Postgres driver recognises the two conditions worth retrying, and
// nothing else.
func TestIsRetryable_PostgresRecognisesConflicts(t *testing.T) {
	d := postgres.Dialect{}
	if d.IsRetryable(nil) {
		t.Error("IsRetryable(nil) = true")
	}
	if d.IsRetryable(errors.New("some other failure")) {
		t.Error("IsRetryable(plain error) = true, want only a conflict retried")
	}
}
