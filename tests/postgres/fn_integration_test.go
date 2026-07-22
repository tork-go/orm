//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

// An order carries what the reporting queries below need: a timestamp to
// bucket by month, a region to group by, a total to aggregate, and a
// nullable label so COALESCE has something to fall back from.
type fOrder struct {
	ID        int
	Region    string
	Label     *string
	Total     int
	CreatedAt time.Time
}

type fOrderModel struct {
	orm.Table[fOrder]
	ID        *orm.IntColumn
	Region    *orm.StringColumn
	Label     *orm.NullableStringColumn
	Total     *orm.IntColumn
	CreatedAt *orm.TimeColumn
}

var fOrders = orm.DefineTable[fOrder]("f_orders", func(t *orm.TableBuilder[fOrder]) *fOrderModel {
	return &fOrderModel{
		Table:     t.Table(),
		ID:        t.Int("id").PrimaryKey(),
		Region:    t.String("region").NotNull().MaxLen(20),
		Label:     t.NullableString("label"),
		Total:     t.Int("total").NotNull(),
		CreatedAt: t.Time("created_at").NotNull(),
	}
})

// The unit tests assert the statement; this asserts the answer. A function
// call is where the two differ most: a name this package never heard of has
// to resolve against a real database to mean anything at all.
func TestFunctions_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS f_orders CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(fOrders)
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

	// Two months, two regions. January: north 100 + 200, south 50.
	// February: north 300, south 400 + 400 — the last two equal, so a
	// distinct count differs from a plain one.
	if _, err := conn.Exec(ctx, `
		INSERT INTO f_orders (id, region, label, total, created_at) VALUES
			(1, 'NORTH', 'retail', 100, '2026-01-05'),
			(2, 'north', NULL,     200, '2026-01-19'),
			(3, 'south', 'trade',   50, '2026-01-25'),
			(4, 'North', 'retail', 300, '2026-02-02'),
			(5, 'south', NULL,     400, '2026-02-14'),
			(6, 'SOUTH', NULL,     400, '2026-02-28')`); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	db := orm.NewDB(conn, dialect)

	// A named helper over a column, folding a value the rows disagree about.
	t.Run("lower folds the region", func(t *testing.T) {
		type row struct {
			Region string
			N      int64
		}
		got, err := orm.SelectAs[row](fOrders.With(db), orm.Lower(fOrders.Region), orm.CountAll()).
			GroupBy(orm.Lower(fOrders.Region)).
			OrderBy(orm.Lower(fOrders.Region).Asc()).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		want := []row{{"north", 3}, {"south", 3}}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("All() = %v, want %v", got, want)
		}
	})

	// COALESCE reads a nullable column as an ordinary value, which is what
	// its explicit result type is for.
	t.Run("coalesce supplies a fallback", func(t *testing.T) {
		type row struct {
			ID    int
			Label string
		}
		got, err := orm.SelectAs[row](
			fOrders.With(db).Where(fOrders.ID.In(2, 3)),
			fOrders.ID, orm.Coalesce[string](fOrders.Label, "unlabelled"),
		).OrderBy(fOrders.ID.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		want := []row{{2, "unlabelled"}, {3, "trade"}}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("All() = %v, want %v", got, want)
		}
	})

	// A value argument reaches the database as a parameter, cast so the
	// function can be resolved at all.
	t.Run("a bound argument resolves the call", func(t *testing.T) {
		type row struct{ N int64 }
		got, err := orm.SelectAs[row](
			fOrders.With(db).Where(fOrders.ID.Equals(1)),
			orm.Fn[int64]("char_length", fOrders.Region),
		).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 1 || got[0].N != 5 {
			t.Errorf("All() = %v, want [{5}]", got)
		}
	})

	// COUNT(DISTINCT x) against COUNT(x): the two differ here, which is the
	// only way to prove the DISTINCT reached the statement.
	t.Run("count distinct", func(t *testing.T) {
		type row struct {
			All      int64
			Distinct int64
		}
		got, err := orm.SelectAs[row](fOrders.With(db),
			orm.CountOf(fOrders.Total), orm.CountOf(fOrders.Total).Distinct()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 1 || got[0].All != 6 || got[0].Distinct != 5 {
			t.Errorf("All() = %v, want [{6 5}]", got)
		}
	})

	// The report the phase is for: bucket by a computed month, group by it,
	// and filter the groups on an aggregate.
	t.Run("monthly report", func(t *testing.T) {
		month := orm.Fn[time.Time]("date_trunc", "month", fOrders.CreatedAt)
		type row struct {
			Month time.Time
			Total int
		}
		got, err := orm.SelectAs[row](fOrders.With(db), month, orm.SumOf(fOrders.Total)).
			GroupBy(month).
			Having(orm.SumOf(fOrders.Total).GreaterThan(500)).
			OrderBy(month.Asc()).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		// January totals 350 and is filtered out; February totals 1100.
		if len(got) != 1 {
			t.Fatalf("All() = %v, want one month", got)
		}
		if got[0].Total != 1100 {
			t.Errorf("total = %d, want 1100", got[0].Total)
		}
		if got[0].Month.Month() != time.February || got[0].Month.Day() != 1 {
			t.Errorf("month = %v, want the first of February", got[0].Month)
		}
	})

	// One aggregate compared against another, which the old HAVING could
	// not express at all.
	t.Run("having compares two aggregates", func(t *testing.T) {
		type row struct{ Region string }
		got, err := orm.SelectAs[row](fOrders.With(db), orm.Lower(fOrders.Region)).
			GroupBy(orm.Lower(fOrders.Region)).
			Having(orm.SumOf(fOrders.Total).GreaterThan(orm.MaxOf(fOrders.Total).Times(2))).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		// north: sum 600, max 300 — 600 > 600 is false.
		// south: sum 850, max 400 — 850 > 800 is true.
		if len(got) != 1 || got[0].Region != "south" {
			t.Errorf("All() = %v, want [south]", got)
		}
	})

	// An aggregate inside arithmetic, and an OR across two of them.
	t.Run("aggregates compose", func(t *testing.T) {
		type row struct {
			Region string
			Mean   int
		}
		got, err := orm.SelectAs[row](fOrders.With(db),
			orm.Lower(fOrders.Region),
			orm.SumOf(fOrders.Total).DividedBy(orm.CountAll()),
		).
			GroupBy(orm.Lower(fOrders.Region)).
			Having(orm.Or(
				orm.CountAll().GreaterThan(int64(5)),
				orm.SumOf(fOrders.Total).GreaterThan(800),
			)).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		// Only south totals more than 800; neither region has over 5 rows.
		if len(got) != 1 || got[0].Region != "south" || got[0].Mean != 283 {
			t.Errorf("All() = %v, want [{south 283}]", got)
		}
	})

	// A hostile function name never reaches the database, so this proves the
	// check rather than the error message.
	t.Run("a hostile name is refused before the statement runs", func(t *testing.T) {
		type row struct{ N int64 }
		_, err := orm.SelectAs[row](fOrders.With(db),
			orm.Fn[int64]("count(*) FROM f_orders; DROP TABLE f_orders; --")).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the name refused")
		}
		// The table is still there, which is the part that matters.
		n, err := fOrders.With(db).Count(ctx)
		if err != nil {
			t.Fatalf("the table did not survive: %v", err)
		}
		if n != 6 {
			t.Errorf("rows = %d, want 6", n)
		}
	})
}
