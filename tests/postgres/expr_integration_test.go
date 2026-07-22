//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type eItem struct {
	ID    int
	Name  string
	Price int
	Cost  int
	Qty   int
}

type eItemModel struct {
	orm.Table[eItem]
	ID    *orm.IntColumn
	Name  *orm.StringColumn
	Price *orm.IntColumn
	Cost  *orm.IntColumn
	Qty   *orm.IntColumn
}

var eItems = orm.DefineTable[eItem]("e_items", func(t *orm.TableBuilder[eItem]) *eItemModel {
	return &eItemModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull().MaxLen(30),
		Price: t.Int("price").NotNull(),
		Cost:  t.Int("cost").NotNull(),
		Qty:   t.Int("qty").NotNull(),
	}
})

// Expressions are tested against a fake dialect elsewhere, which only ever
// proves the string Tork writes. This runs them against the database they
// were written for, so what is checked is the rows that come back rather
// than an expectation written by the same hand that wrote the renderer.
func TestExpressions_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS e_items CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(eItems)
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

	// widget sells above cost, gadget below it, gizmo exactly at it.
	if _, err := conn.Exec(ctx, `
		INSERT INTO e_items (name, price, cost, qty) VALUES
			('widget', 100, 60, 3),
			('gadget',  40, 90, 5),
			('gizmo',   50, 50, 7)`); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	db := orm.NewDB(conn, dialect)

	// The motivating case: one column compared against another, which no
	// column method could express before expressions existed.
	t.Run("column against column", func(t *testing.T) {
		got, err := eItems.With(db).
			Where(eItems.Price.Value().GreaterThan(eItems.Cost)).
			OrderBy(eItems.Name.Asc()).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 1 || got[0].Name != "widget" {
			t.Fatalf("got %d rows %v, want just widget", len(got), names(got))
		}
	})

	t.Run("column against column, the other way", func(t *testing.T) {
		got, err := eItems.With(db).
			Where(eItems.Price.Value().LessThan(eItems.Cost)).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 1 || got[0].Name != "gadget" {
			t.Fatalf("got %v, want just gadget", names(got))
		}
	})

	// Equality catches the row where the two columns tie, which is the case
	// a strict comparison would miss and a wrong operator would return.
	t.Run("column equal to column", func(t *testing.T) {
		got, err := eItems.With(db).
			Where(eItems.Price.Value().Equals(eItems.Cost)).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 1 || got[0].Name != "gizmo" {
			t.Fatalf("got %v, want just gizmo", names(got))
		}
	})

	// Arithmetic in a filter, which is what the parenthesising exists for:
	// price * qty is 300, 200 and 350, so only two clear 250.
	t.Run("arithmetic in a filter", func(t *testing.T) {
		got, err := eItems.With(db).
			Where(eItems.Price.Times(eItems.Qty).GreaterThan(250)).
			OrderBy(eItems.Name.Asc()).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 2 || got[0].Name != "gizmo" || got[1].Name != "widget" {
			t.Fatalf("got %v, want gizmo and widget", names(got))
		}
	})

	// A computed column read back into a caller's own struct, with the
	// arithmetic done by the database rather than in Go.
	t.Run("computed column in a projection", func(t *testing.T) {
		type row struct {
			Name  string
			Total int
		}
		got, err := orm.SelectAs[row](
			eItems.With(db).Where(eItems.Name.Equals("widget")),
			eItems.Name,
			eItems.Price.Times(eItems.Qty),
		).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 1 || got[0].Total != 300 {
			t.Fatalf("got %+v, want a total of 300", got)
		}
	})

	// Nesting, where the parentheses decide the answer: (price - cost) * qty
	// is 120 for widget, where price - (cost * qty) would be -80.
	t.Run("nested arithmetic groups as written", func(t *testing.T) {
		type row struct{ Margin int }
		got, err := orm.SelectAs[row](
			eItems.With(db).Where(eItems.Name.Equals("widget")),
			eItems.Price.Minus(eItems.Cost).Times(eItems.Qty),
		).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 1 || got[0].Margin != 120 {
			t.Fatalf("got %+v, want a margin of 120", got)
		}
	})

	// An expression-valued assignment, computed by the database against
	// whatever the column holds when the statement runs.
	t.Run("increment writes against the stored value", func(t *testing.T) {
		if _, err := eItems.With(db).
			Where(eItems.Name.Equals("gizmo")).
			UpdateAll(ctx, eItems.Qty.Increment(5)); err != nil {
			t.Fatalf("UpdateAll() error = %v", err)
		}
		got, err := eItems.With(db).Where(eItems.Name.Equals("gizmo")).First(ctx)
		if err != nil {
			t.Fatalf("First() error = %v", err)
		}
		if got.Qty != 12 {
			t.Errorf("Qty = %d, want 12", got.Qty)
		}
	})
}

func names(items []*eItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Name
	}
	return out
}
