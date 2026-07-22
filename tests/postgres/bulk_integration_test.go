//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type bulkRow struct {
	ID    int
	Name  string
	Slot  int
	Tag   *string
	Note  string
	Extra string
}

type bulkRowModel struct {
	orm.Table[bulkRow]
	ID    *orm.IntColumn
	Name  *orm.StringColumn
	Slot  *orm.IntColumn
	Tag   *orm.NullableStringColumn
	Note  *orm.StringColumn
	Extra *orm.StringColumn
}

// Six columns, of which five are written on an ordinary row: the key is
// generated, and the other five are bound. Postgres allows 65535 parameters
// a statement, so a statement holds 13107 rows and a batch larger than that
// has to be split.
var bulkRows = orm.DefineTable[bulkRow]("bulk_rows", func(t *orm.TableBuilder[bulkRow]) *bulkRowModel {
	return &bulkRowModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
		Slot:  t.Int("slot").NotNull(),
		Tag:   t.NullableString("tag"),
		Note:  t.String("note").NotNull().ServerDefault("'default note'"),
		Extra: t.String("extra").NotNull(),
	}
})

// setupBulk creates the table and returns a handle and the raw connection.
func setupBulk(t *testing.T) (*orm.DB, orm.Conn) {
	t.Helper()
	ctx := context.Background()
	dialect := postgres.Dialect{}

	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS bulk_rows CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(bulkRows)
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
	return orm.NewDB(conn, dialect), conn
}

func countBulk(t *testing.T, conn orm.Conn, where string, args ...any) int64 {
	t.Helper()
	sql := `SELECT count(*) FROM bulk_rows`
	if where != "" {
		sql += " WHERE " + where
	}
	rows, err := conn.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatalf("counting failed: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("counting returned no row")
	}
	var n int64
	if err := rows.Scan(&n); err != nil {
		t.Fatalf("scanning count failed: %v", err)
	}
	return n
}

// The statements the compiler builds are tested against a fake dialect
// elsewhere. This runs them against the database they were written for,
// which is the only way to learn that Postgres accepts them and that the
// rows come back in the order the correlation assumes.
func TestBulk_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	db, conn := setupBulk(t)

	t.Run("InsertMany reads generated keys back onto the right rows", func(t *testing.T) {
		rows := make([]*bulkRow, 50)
		for i := range rows {
			rows[i] = &bulkRow{Name: fmt.Sprintf("row-%02d", i), Slot: i, Extra: "e"}
		}
		if err := bulkRows.With(db).InsertMany(ctx, rows...); err != nil {
			t.Fatalf("InsertMany() error = %v", err)
		}

		// Every row got a distinct key, and the key it got is the one the
		// database stored against that row's own name. This is the property
		// the whole read back rests on, so it is checked against the
		// database rather than against the order of the result.
		seen := map[int]bool{}
		for i, r := range rows {
			if r.ID == 0 {
				t.Fatalf("row %d has no key", i)
			}
			if seen[r.ID] {
				t.Fatalf("row %d reused key %d", i, r.ID)
			}
			seen[r.ID] = true
			if n := countBulk(t, conn, "id = $1 AND name = $2", r.ID, r.Name); n != 1 {
				t.Errorf("row %d claims key %d, but the database has %d such row(s) named %s",
					i, r.ID, n, r.Name)
			}
			// The server default was read back too.
			if r.Note != "default note" {
				t.Errorf("row %d note = %q, want the default read back", i, r.Note)
			}
		}
	})

	t.Run("rows writing different columns are split and still correlate", func(t *testing.T) {
		if _, err := bulkRows.With(db).DeleteAll(ctx); err != nil {
			t.Fatalf("DeleteAll() error = %v", err)
		}
		rows := []*bulkRow{
			{Name: "default-note", Slot: 1, Extra: "e"},
			{Name: "own-note", Slot: 2, Extra: "e", Note: "mine"},
			{Name: "default-note-2", Slot: 3, Extra: "e"},
		}
		if err := bulkRows.With(db).InsertMany(ctx, rows...); err != nil {
			t.Fatalf("InsertMany() error = %v", err)
		}
		want := []string{"default note", "mine", "default note"}
		for i, r := range rows {
			if r.Note != want[i] {
				t.Errorf("row %d note = %q, want %q", i, r.Note, want[i])
			}
			if n := countBulk(t, conn, "id = $1 AND note = $2", r.ID, want[i]); n != 1 {
				t.Errorf("row %d did not land in the database with note %q", i, want[i])
			}
		}
	})

	t.Run("a batch past the parameter ceiling is split", func(t *testing.T) {
		if _, err := bulkRows.With(db).DeleteAll(ctx); err != nil {
			t.Fatalf("DeleteAll() error = %v", err)
		}

		// Five bound values a row against a ceiling of 65535 is 13107 rows a
		// statement, so fifteen thousand cannot have been sent as one. That
		// the call succeeds at all is the assertion; without splitting,
		// Postgres rejects it outright.
		const size = 15000
		const boundary = 65535 / 5 // where the first statement ends
		rows := make([]*bulkRow, size)
		for i := range rows {
			rows[i] = &bulkRow{Name: fmt.Sprintf("r%05d", i), Slot: i, Extra: "e", Note: "n"}
		}
		if err := bulkRows.With(db).InsertMany(ctx, rows...); err != nil {
			t.Fatalf("InsertMany() error = %v", err)
		}
		if n := countBulk(t, conn, ""); n != size {
			t.Fatalf("the table holds %d rows, want %d", n, size)
		}
		// Spot check the correlation at both ends and either side of the
		// chunk boundary, which is where a mismatch would first appear.
		for _, i := range []int{0, boundary - 1, boundary, size - 1} {
			r := rows[i]
			if n := countBulk(t, conn, "id = $1 AND slot = $2", r.ID, i); n != 1 {
				t.Errorf("row %d got key %d, which is not the row with slot %d", i, r.ID, i)
			}
		}
	})

	t.Run("UpdateAll writes every matching row", func(t *testing.T) {
		matching := countBulk(t, conn, "slot < 100")
		n, err := bulkRows.With(db).Where(bulkRows.Slot.LessThan(100)).
			UpdateAll(ctx, bulkRows.Extra.Set("touched"), bulkRows.Tag.SetNull())
		if err != nil {
			t.Fatalf("UpdateAll() error = %v", err)
		}
		if n != matching {
			t.Errorf("UpdateAll() = %d, want the %d rows matching the filter", n, matching)
		}
		if got := countBulk(t, conn, "extra = 'touched'"); got != matching {
			t.Errorf("%d rows carry the new value, want %d", got, matching)
		}
	})

	t.Run("UpdateMany writes a different value to each row", func(t *testing.T) {
		rows, err := bulkRows.With(db).Where(bulkRows.Slot.LessThan(5)).OrderBy(bulkRows.Slot.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		for i, r := range rows {
			r.Extra = fmt.Sprintf("each-%d", i)
		}
		n, err := bulkRows.With(db).UpdateMany(ctx, rows...)
		if err != nil {
			t.Fatalf("UpdateMany() error = %v", err)
		}
		if n != int64(len(rows)) {
			t.Errorf("UpdateMany() = %d, want %d", n, len(rows))
		}
		for i, r := range rows {
			want := fmt.Sprintf("each-%d", i)
			if got := countBulk(t, conn, "id = $1 AND extra = $2", r.ID, want); got != 1 {
				t.Errorf("row %d did not get its own value %q", i, want)
			}
		}
	})

	t.Run("DeleteMany removes exactly the rows it was given", func(t *testing.T) {
		rows, err := bulkRows.With(db).Where(bulkRows.Slot.LessThan(10)).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		before := countBulk(t, conn, "")
		n, err := bulkRows.With(db).DeleteMany(ctx, rows...)
		if err != nil {
			t.Fatalf("DeleteMany() error = %v", err)
		}
		if n != int64(len(rows)) {
			t.Errorf("DeleteMany() = %d, want %d", n, len(rows))
		}
		if got := countBulk(t, conn, ""); got != before-int64(len(rows)) {
			t.Errorf("the table holds %d rows, want %d", got, before-int64(len(rows)))
		}
	})

	t.Run("DeleteAll with a filter removes only the matching rows", func(t *testing.T) {
		matching := countBulk(t, conn, "slot >= 11000")
		if matching == 0 {
			t.Fatal("the fixture no longer has rows past slot 11000")
		}
		n, err := bulkRows.With(db).Where(bulkRows.Slot.GreaterOrEqual(11000)).DeleteAll(ctx)
		if err != nil {
			t.Fatalf("DeleteAll() error = %v", err)
		}
		if n != matching {
			t.Errorf("DeleteAll() = %d, want %d", n, matching)
		}
		if got := countBulk(t, conn, "slot >= 11000"); got != 0 {
			t.Errorf("%d matching rows survived", got)
		}
	})
}

// A failure partway through a chunked insert must leave nothing behind.
func TestBulk_RollsBackAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	db, conn := setupBulk(t)

	// Two rows sharing a slot, with a unique index added after the fact, so
	// the second chunk violates it and the first must not survive.
	if _, err := conn.Exec(ctx, `CREATE UNIQUE INDEX bulk_rows_slot_uq ON bulk_rows (slot)`); err != nil {
		t.Fatalf("creating the unique index failed: %v", err)
	}

	const size = 15000
	rows := make([]*bulkRow, size)
	for i := range rows {
		rows[i] = &bulkRow{Name: fmt.Sprintf("r%05d", i), Slot: i, Extra: "e", Note: "n"}
	}
	// A duplicate slot in the last row, which falls in the second statement,
	// so the first has already run when the violation is raised.
	rows[size-1].Slot = 0

	err := bulkRows.With(db).InsertMany(ctx, rows...)
	if err == nil {
		t.Fatal("InsertMany() error = nil, want the unique violation")
	}
	if n := countBulk(t, conn, ""); n != 0 {
		t.Errorf("the table holds %d rows, want none: the earlier chunks were not rolled back", n)
	}
}

// A transaction the caller opened wraps everything inside it, including a
// bulk write that would otherwise have opened one of its own.
func TestTransaction_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	db, conn := setupBulk(t)

	sentinel := errors.New("changed my mind")
	err := db.Transaction(ctx, func(tx *orm.DB) error {
		rows := []*bulkRow{
			{Name: "a", Slot: 1, Extra: "e"},
			{Name: "b", Slot: 2, Extra: "e"},
		}
		if err := bulkRows.With(tx).InsertMany(ctx, rows...); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Transaction() error = %v, want the callback's own", err)
	}
	if n := countBulk(t, conn, ""); n != 0 {
		t.Errorf("the table holds %d rows after a rollback, want none", n)
	}

	// And the committing case actually commits.
	err = db.Transaction(ctx, func(tx *orm.DB) error {
		return bulkRows.With(tx).InsertMany(ctx,
			&bulkRow{Name: "a", Slot: 1, Extra: "e"},
			&bulkRow{Name: "b", Slot: 2, Extra: "e"})
	})
	if err != nil {
		t.Fatalf("Transaction() error = %v", err)
	}
	if n := countBulk(t, conn, ""); n != 2 {
		t.Errorf("the table holds %d rows after a commit, want 2", n)
	}
}
