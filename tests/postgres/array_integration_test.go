//go:build integration

package postgres_test

import (
	"context"
	"sort"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type aRow struct {
	ID   int
	Name string
	Tags []string
	Nums []int
}

type aRowModel struct {
	orm.Table[aRow]
	ID   *orm.IntColumn
	Name *orm.StringColumn
	Tags *orm.StringArrayColumn
	Nums *orm.IntArrayColumn
}

var aRows = orm.DefineTable[aRow]("a_rows", func(t *orm.TableBuilder[aRow]) *aRowModel {
	return &aRowModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
		Tags:  t.StringArray("tags").NotNull(),
		Nums:  t.IntArray("nums").NotNull(),
	}
})

// What the array operators render is checked against the fakes. That the SQL
// runs — @> and && over real text[] and int[], cardinality counting an empty
// array as zero, and the empty-list rules matching what Postgres itself does —
// is only knowable here.
func TestArray_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}

	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS a_rows CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(aRows)
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
	if err := aRows.With(db).InsertMany(ctx,
		&aRow{Name: "a", Tags: []string{"go", "sql", "orm"}, Nums: []int{1, 2, 3}},
		&aRow{Name: "b", Tags: []string{"go", "rust"}, Nums: []int{3, 4}},
		&aRow{Name: "c", Tags: []string{}, Nums: []int{}}, // empty arrays
	); err != nil {
		t.Fatalf("InsertMany failed: %v", err)
	}

	names := func(rs []*aRow) []string {
		out := make([]string, len(rs))
		for i, r := range rs {
			out[i] = r.Name
		}
		sort.Strings(out)
		return out
	}
	eq := func(t *testing.T, got, want []string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("matched %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("matched %v, want %v", got, want)
			}
		}
	}
	all := func(t *testing.T, pred orm.Predicate) []string {
		t.Helper()
		got, err := aRows.With(db).Where(pred).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		return names(got)
	}

	t.Run("has an element", func(t *testing.T) {
		eq(t, all(t, aRows.Tags.Has("go")), []string{"a", "b"})
	})
	t.Run("has all elements", func(t *testing.T) {
		eq(t, all(t, aRows.Tags.HasAll("go", "sql")), []string{"a"})
	})
	t.Run("has any element", func(t *testing.T) {
		eq(t, all(t, aRows.Tags.HasAny("rust", "java")), []string{"b"})
	})
	t.Run("length greater than", func(t *testing.T) {
		eq(t, all(t, aRows.Tags.Len().Gt(2)), []string{"a"})
	})
	t.Run("length zero is empty", func(t *testing.T) {
		eq(t, all(t, aRows.Tags.Len().Eq(0)), []string{"c"})
	})
	t.Run("int elements", func(t *testing.T) {
		eq(t, all(t, aRows.Nums.Has(3)), []string{"a", "b"})
	})

	// The empty-list rules, confirmed against the database rather than only
	// asserted: HasAll of nothing is true of every row, HasAny of nothing of
	// none.
	t.Run("empty HasAll matches every row", func(t *testing.T) {
		eq(t, all(t, aRows.Tags.HasAll()), []string{"a", "b", "c"})
	})
	t.Run("empty HasAny matches no row", func(t *testing.T) {
		got, err := aRows.With(db).Where(aRows.Tags.HasAny()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("matched %v, want nothing", names(got))
		}
	})
}
