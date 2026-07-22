//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type tAccount struct {
	ID      int
	Owner   string
	Balance int
}

type tAccountModel struct {
	orm.Table[tAccount]
	ID      *orm.IntColumn
	Owner   *orm.StringColumn
	Balance *orm.IntColumn
}

var tAccounts = orm.DefineTable[tAccount]("t_accounts", func(t *orm.TableBuilder[tAccount]) *tAccountModel {
	return &tAccountModel{
		Table:   t.Table(),
		ID:      t.Int("id").PrimaryKey(),
		Owner:   t.String("owner").NotNull().MaxLen(20),
		Balance: t.Int("balance").NotNull(),
	}
})

// Savepoints, isolation levels and retries are the parts of this package a
// compile test cannot reach at all: what they mean is what two concurrent
// transactions do, which only a database decides.
func TestTransactions_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS t_accounts CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(tAccounts)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	ops, _ := migrate.Diff(schema.Schema{}, desired)
	ddl, err := migrate.Generate(dialect, ops)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if _, err := conn.Exec(ctx, ddl); err != nil {
		t.Fatalf("applying schema failed: %v\n%s", err, ddl)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO t_accounts (id, owner, balance) OVERRIDING SYSTEM VALUE VALUES
			(1, 'ada', 100), (2, 'ben', 100)`); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	db := orm.NewDB(conn, dialect)

	// The point of a savepoint: the outer transaction swallows the inner
	// failure and everything before it still commits.
	t.Run("an inner failure undoes only its own part", func(t *testing.T) {
		inner := errors.New("not worth failing over")
		err := db.Transaction(ctx, func(tx *orm.DB) error {
			if _, err := tAccounts.With(tx).Where(tAccounts.ID.Equals(1)).
				UpdateAll(ctx, tAccounts.Balance.Set(150)); err != nil {
				return err
			}
			// This part is undone; the update above is not.
			_ = tx.Transaction(ctx, func(sp *orm.DB) error {
				if _, err := tAccounts.With(sp).Where(tAccounts.ID.Equals(2)).
					UpdateAll(ctx, tAccounts.Balance.Set(999)); err != nil {
					return err
				}
				return inner
			})
			return nil
		})
		if err != nil {
			t.Fatalf("Transaction() error = %v", err)
		}

		ada, err := tAccounts.With(db).Find(ctx, 1)
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}
		ben, err := tAccounts.With(db).Find(ctx, 2)
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}
		if ada.Balance != 150 {
			t.Errorf("ada = %d, want the outer work committed", ada.Balance)
		}
		if ben.Balance != 100 {
			t.Errorf("ben = %d, want the savepoint's work undone", ben.Balance)
		}
	})

	// A read-only transaction refuses a write, which is the point of
	// declaring one.
	t.Run("read only refuses a write", func(t *testing.T) {
		err := db.TransactionWith(ctx, orm.TxOptions{ReadOnly: true}, func(tx *orm.DB) error {
			_, err := tAccounts.With(tx).Where(tAccounts.ID.Equals(1)).
				UpdateAll(ctx, tAccounts.Balance.Set(0))
			return err
		})
		if err == nil {
			t.Fatal("TransactionWith() error = nil, want the write refused")
		}
	})

	// An isolation level the caller asked for is the one the transaction
	// runs at, which the database itself will report back.
	t.Run("the level asked for is the level set", func(t *testing.T) {
		err := db.TransactionWith(ctx, orm.TxOptions{Isolation: orm.IsolationSerializable},
			func(tx *orm.DB) error {
				rows, err := orm.RawQuery[struct{ Level string }](ctx, tx,
					"SELECT current_setting('transaction_isolation')")
				if err != nil {
					return err
				}
				if len(rows) != 1 || rows[0].Level != "serializable" {
					t.Errorf("transaction_isolation = %+v, want serializable", rows)
				}
				return nil
			})
		if err != nil {
			t.Fatalf("TransactionWith() error = %v", err)
		}
	})

	// Two serializable transactions reading and writing the same rows are
	// exactly what the level refuses to commit — and what a retry settles.
	t.Run("a serialization conflict is retried", func(t *testing.T) {
		if _, err := conn.Exec(ctx,
			`UPDATE t_accounts SET balance = 100`); err != nil {
			t.Fatalf("resetting failed: %v", err)
		}

		// Each worker moves a pound the other way, reading both balances
		// first so the two genuinely conflict under SERIALIZABLE.
		transfer := func(from, to int) error {
			return db.TransactionWith(ctx,
				orm.TxOptions{Isolation: orm.IsolationSerializable, Retries: 10},
				func(tx *orm.DB) error {
					total, err := orm.Sum(ctx, tAccounts.With(tx), tAccounts.Balance)
					if err != nil {
						return err
					}
					if total != 200 {
						t.Errorf("total = %d, want the pair to stay whole", total)
					}
					if _, err := tAccounts.With(tx).Where(tAccounts.ID.Equals(from)).
						UpdateAll(ctx, tAccounts.Balance.Decrement(1)); err != nil {
						return err
					}
					_, err = tAccounts.With(tx).Where(tAccounts.ID.Equals(to)).
						UpdateAll(ctx, tAccounts.Balance.Increment(1))
					return err
				})
		}

		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); errs[0] = transfer(1, 2) }()
		go func() { defer wg.Done(); errs[1] = transfer(2, 1) }()
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Errorf("transfer %d failed: %v", i, err)
			}
		}
		total, err := orm.Sum(ctx, tAccounts.With(db), tAccounts.Balance)
		if err != nil {
			t.Fatalf("Sum() error = %v", err)
		}
		if total != 200 {
			t.Errorf("total = %d, want 200: both transfers preserved it", total)
		}
	})

	// The locking clauses have to be ones Postgres accepts, which is the
	// half a compile test cannot check.
	t.Run("every lock mode is one Postgres accepts", func(t *testing.T) {
		for _, lock := range []func(*orm.Query[tAccount]) *orm.Filtered[tAccount]{
			(*orm.Query[tAccount]).ForUpdate,
			(*orm.Query[tAccount]).ForShare,
			(*orm.Query[tAccount]).ForNoKeyUpdate,
			(*orm.Query[tAccount]).ForKeyShare,
		} {
			err := db.Transaction(ctx, func(tx *orm.DB) error {
				_, err := lock(tAccounts.With(tx)).Limit(1).All(ctx)
				return err
			})
			if err != nil {
				t.Errorf("locking read failed: %v", err)
			}
		}
	})

	// A lock narrowed to one table of a join is what stops a reader holding
	// rows it never meant to.
	t.Run("a lock narrowed to one table", func(t *testing.T) {
		err := db.Transaction(ctx, func(tx *orm.DB) error {
			_, err := orm.SelectAs[struct{ Owner string }](
				tAccounts.With(tx).JoinTo(tAccounts2, tAccounts2.ID.Value().Equals(tAccounts.ID)),
				tAccounts.Owner,
			).All(ctx)
			return err
		})
		if err != nil {
			t.Fatalf("projection over a self join failed: %v", err)
		}

		err = db.Transaction(ctx, func(tx *orm.DB) error {
			_, err := tAccounts.With(tx).
				JoinTo(tAccounts2, tAccounts2.ID.Value().Equals(tAccounts.ID)).
				ForUpdate().LockOf(tAccounts).
				All(ctx)
			return err
		})
		if err != nil {
			t.Errorf("narrowed locking read failed: %v", err)
		}
	})
}

// tAccounts2 is the same table under a second name, so a join has something
// to narrow a lock away from.
var tAccounts2 = orm.Alias(tAccounts, "t_accounts_2")
