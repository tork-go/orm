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

type compositeOrder struct {
	OrgID int
	ID    int
}
type compositeOrderModel struct {
	orm.Table[compositeOrder]
	OrgID *orm.IntColumn
	ID    *orm.IntColumn
}

type compositeLine struct {
	ID      int
	OrgID   int
	OrderID int
}
type compositeLineModel struct {
	orm.Table[compositeLine]
	ID      *orm.IntColumn
	OrgID   *orm.IntColumn
	OrderID *orm.IntColumn
}

func (m *compositeLineModel) ForeignKeys() []orm.ForeignKeyDef {
	return []orm.ForeignKeyDef{
		orm.NewForeignKeyDef(m.OrgID, m.OrderID).
			References(compositeOrders.OrgID, compositeOrders.ID).
			OnDelete(orm.ActionCascade),
	}
}

var (
	compositeOrders = orm.DefineTable[compositeOrder]("ck_orders",
		func(t *orm.TableBuilder[compositeOrder]) *compositeOrderModel {
			return &compositeOrderModel{
				Table: t.Table(),
				OrgID: t.Int("org_id").PrimaryKey(),
				ID:    t.Int("id").PrimaryKey(),
			}
		})
	compositeLines = orm.DefineTable[compositeLine]("ck_lines",
		func(t *orm.TableBuilder[compositeLine]) *compositeLineModel {
			return &compositeLineModel{
				Table:   t.Table(),
				ID:      t.Int("id").PrimaryKey(),
				OrgID:   t.Int("org_id").NotNull(),
				OrderID: t.Int("order_id").NotNull(),
			}
		})
)

// A composite key is the case information_schema could never report
// correctly: its constraint_column_usage view lists the referenced columns
// without tying each to the key column it pairs with, so a two column key
// came back as four unordered rows. Reading pg_constraint's ordered conkey
// and confkey arrays is what makes this round trip.
//
// The pairing here is deliberately not the obvious one. ck_lines(org_id,
// order_id) references ck_orders(org_id, id), so pairing by name alone
// would map order_id to the wrong column and the re-diff would show it.
func TestCompositeForeignKey_RoundTripsThroughPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS ck_lines, ck_orders CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(compositeOrders, compositeLines)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	ops, err := migrate.Diff(schema.Schema{}, desired)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	sql, err := migrate.Generate(dialect, ops)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("applying generated SQL failed: %v\n%s", err, sql)
	}

	got, err := dialect.Introspect(ctx, conn, []string{"ck_orders", "ck_lines"})
	if err != nil {
		t.Fatalf("Introspect failed: %v", err)
	}

	var lines *schema.Table
	for i := range got.Tables {
		if got.Tables[i].Name == "ck_lines" {
			lines = &got.Tables[i]
		}
	}
	if lines == nil {
		t.Fatal("ck_lines missing from the introspected schema")
	}
	if len(lines.ForeignKeys) != 1 {
		t.Fatalf("ck_lines has %d foreign keys, want 1", len(lines.ForeignKeys))
	}
	fk := lines.ForeignKeys[0]

	if len(fk.Columns) != 2 || fk.Columns[0] != "org_id" || fk.Columns[1] != "order_id" {
		t.Errorf("Columns = %v, want [org_id order_id]", fk.Columns)
	}
	// The pairing, which is the whole point: order_id maps to id, not to
	// the identically named org_id.
	if len(fk.ReferencedColumns) != 2 || fk.ReferencedColumns[0] != "org_id" || fk.ReferencedColumns[1] != "id" {
		t.Errorf("ReferencedColumns = %v, want [org_id id]", fk.ReferencedColumns)
	}
	if fk.OnDelete != schema.ActionCascade {
		t.Errorf("OnDelete = %v, want ActionCascade", fk.OnDelete)
	}

	back, err := migrate.Diff(got, desired)
	if err != nil {
		t.Fatalf("re-diff failed: %v", err)
	}
	if len(back) != 0 {
		t.Errorf("re-diffing produced %d operations, want none:", len(back))
		for _, op := range back {
			t.Errorf("  %#v", op)
		}
	}
}
