//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type sRow struct {
	ID int
	N  int
}

type sRowModel struct {
	orm.Table[sRow]
	ID *orm.IntColumn
	N  *orm.IntColumn
}

var sRows = orm.DefineTable[sRow]("s_rows", func(t *orm.TableBuilder[sRow]) *sRowModel {
	return &sRowModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		N:     t.Int("n").NotNull(),
	}
})

// What Each renders is checked against the fakes. What it is for — reading a
// result set larger than a slice you would want in memory, one row at a time,
// while holding whatever lock the read took — is only observable against a
// real server, which is what this is for.
func TestStreaming_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}

	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS s_rows CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(sRows)
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

	db := orm.NewDB(conn, dialect)

	// A second handle on its own connection, so a transaction here really is a
	// different session from one there.
	other, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = other.Close(context.Background()) })
	otherDB := orm.NewDB(other, dialect)

	seed := func(t *testing.T, n int) {
		t.Helper()
		if _, err := conn.Exec(ctx, `TRUNCATE s_rows RESTART IDENTITY`); err != nil {
			t.Fatalf("truncate failed: %v", err)
		}
		rows := make([]*sRow, n)
		for i := range rows {
			rows[i] = &sRow{N: i + 1}
		}
		if err := sRows.With(db).InsertMany(ctx, rows...); err != nil {
			t.Fatalf("InsertMany failed: %v", err)
		}
	}

	// Every row comes back exactly once, in order, for a set larger than a test
	// would want to hold as a slice. Summing N is a checksum: 1+2+...+n.
	t.Run("streams a large result set once each", func(t *testing.T) {
		const n = 10000
		seed(t, n)

		count, sum, prev := 0, 0, 0
		for r, err := range sRows.With(db).OrderBy(sRows.ID.Asc()).Each(ctx) {
			if err != nil {
				t.Fatalf("Each yielded error = %v", err)
			}
			if r.ID <= prev {
				t.Fatalf("ids came back out of order: %d after %d", r.ID, prev)
			}
			prev = r.ID
			count++
			sum += r.N
		}
		if count != n {
			t.Errorf("streamed %d rows, want %d", count, n)
		}
		if want := n * (n + 1) / 2; sum != want {
			t.Errorf("N summed to %d, want %d", sum, want)
		}
	})

	// Breaking out of the loop closes the cursor. A pgx connection refuses a
	// second query while one is still open, so the follow-up read succeeding is
	// proof the first was released rather than left dangling.
	t.Run("an early break releases the cursor", func(t *testing.T) {
		seed(t, 100)

		seen := 0
		for _, err := range sRows.With(db).OrderBy(sRows.ID.Asc()).Each(ctx) {
			if err != nil {
				t.Fatalf("Each yielded error = %v", err)
			}
			seen++
			if seen == 3 {
				break
			}
		}
		if seen != 3 {
			t.Fatalf("saw %d rows before break, want 3", seen)
		}
		// This runs on the same connection; a leaked cursor would fail it.
		if n, err := sRows.With(db).Count(ctx); err != nil || n != 100 {
			t.Fatalf("follow-up Count() = %d, %v; want 100 with the cursor released", n, err)
		}
	})

	// A stream holds the lock its read took for the length of the transaction,
	// exactly as All does: the rows one transaction drained under
	// ForUpdate().SkipLocked() are passed over by a second one, never claimed
	// twice.
	t.Run("streaming holds a lock for the transaction", func(t *testing.T) {
		seed(t, 4)

		drain := func(tx *orm.DB) ([]int, error) {
			var ids []int
			for r, err := range sRows.With(tx).Where(sRows.N.GreaterThan(0)).
				OrderBy(sRows.ID.Asc()).ForUpdate().SkipLocked().Limit(2).Each(ctx) {
				if err != nil {
					return nil, err
				}
				ids = append(ids, r.ID)
			}
			return ids, nil
		}

		var first, second []int
		err := db.Transaction(ctx, func(tx *orm.DB) error {
			first, err = drain(tx)
			if err != nil {
				return err
			}
			// Still inside the first transaction, so its rows are locked.
			return otherDB.Transaction(ctx, func(tx2 *orm.DB) error {
				second, err = drain(tx2)
				return err
			})
		})
		if err != nil {
			t.Fatalf("Transaction() error = %v", err)
		}
		if len(first) != 2 || len(second) != 2 {
			t.Fatalf("drained %d and %d rows, want 2 each", len(first), len(second))
		}
		taken := map[int]bool{}
		for _, id := range append(append([]int{}, first...), second...) {
			if taken[id] {
				t.Errorf("row %d was claimed by both streams", id)
			}
			taken[id] = true
		}
		if len(taken) != 4 {
			t.Errorf("claimed %d distinct rows, want all 4", len(taken))
		}
	})

	// A context cancelled mid-iteration ends the stream with an error rather
	// than finishing quietly, since the failure has nowhere to go but the
	// range's second value.
	t.Run("a cancelled context ends the stream with an error", func(t *testing.T) {
		seed(t, 5000)

		cctx, cancel := context.WithCancel(ctx)
		defer cancel()

		seen := 0
		var gotErr error
		for _, err := range sRows.With(db).OrderBy(sRows.ID.Asc()).Each(cctx) {
			if err != nil {
				gotErr = err
				break
			}
			seen++
			if seen == 1 {
				cancel()
			}
		}
		if gotErr == nil || !errors.Is(gotErr, context.Canceled) {
			t.Fatalf("Each ended with %v, want a cancelled-context error", gotErr)
		}
		if seen >= 5000 {
			t.Errorf("streamed every row despite cancellation, want it cut short")
		}
		// The connection is usable again once the stream has ended.
		if _, err := sRows.With(db).Count(ctx); err != nil {
			t.Fatalf("Count() after a cancelled stream error = %v", err)
		}
	})
}
