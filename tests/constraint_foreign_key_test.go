package orm_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/schema"
)

// A key spanning several columns has no single column to hang References
// on, so it is declared at the table level like a compound index.
type orderEntity struct {
	OrgID int
	ID    int
}
type orderModel struct {
	orm.Table[orderEntity]
	OrgID *orm.IntColumn
	ID    *orm.IntColumn
}

type lineEntity struct {
	ID      int
	OrgID   int
	OrderID int
}
type lineModel struct {
	orm.Table[lineEntity]
	ID      *orm.IntColumn
	OrgID   *orm.IntColumn
	OrderID *orm.IntColumn
}

func (m *lineModel) ForeignKeys() []orm.ForeignKeyDef {
	return []orm.ForeignKeyDef{
		orm.NewForeignKeyDef(m.OrgID, m.OrderID).
			References(orderTable.OrgID, orderTable.ID).
			OnDelete(orm.ActionCascade),
	}
}

var (
	orderTable = orm.DefineTable[orderEntity]("orders", func(t *orm.TableBuilder[orderEntity]) *orderModel {
		return &orderModel{Table: t.Table(), OrgID: t.Int("org_id").PrimaryKey(), ID: t.Int("id").PrimaryKey()}
	})
	lineTable = orm.DefineTable[lineEntity]("order_lines", func(t *orm.TableBuilder[lineEntity]) *lineModel {
		return &lineModel{
			Table:   t.Table(),
			ID:      t.Int("id").PrimaryKey(),
			OrgID:   t.Int("org_id").NotNull(),
			OrderID: t.Int("order_id").NotNull(),
		}
	})
)

func TestForeignKeyDef_Composite(t *testing.T) {
	s, err := schema.ExtractSchema(orderTable, lineTable)
	if err != nil {
		t.Fatalf("ExtractSchema() error = %v", err)
	}
	var lines schema.Table
	for _, tbl := range s.Tables {
		if tbl.Name == "order_lines" {
			lines = tbl
		}
	}
	if len(lines.ForeignKeys) != 1 {
		t.Fatalf("order_lines has %d foreign keys, want 1", len(lines.ForeignKeys))
	}
	fk := lines.ForeignKeys[0]

	// Order carries the pairing, so both lists have to keep it.
	if len(fk.Columns) != 2 || fk.Columns[0] != "org_id" || fk.Columns[1] != "order_id" {
		t.Errorf("Columns = %v, want [org_id order_id]", fk.Columns)
	}
	if len(fk.ReferencedColumns) != 2 || fk.ReferencedColumns[0] != "org_id" || fk.ReferencedColumns[1] != "id" {
		t.Errorf("ReferencedColumns = %v, want [org_id id]", fk.ReferencedColumns)
	}
	if fk.ReferencedTable != "orders" {
		t.Errorf("ReferencedTable = %q, want orders", fk.ReferencedTable)
	}
	if fk.OnDelete != schema.ActionCascade {
		t.Errorf("OnDelete = %v, want ActionCascade", fk.OnDelete)
	}
	// The name is derived from the columns, like every other constraint.
	if fk.Name != "fk_order_lines_org_id_order_id" {
		t.Errorf("Name = %q, want fk_order_lines_org_id_order_id", fk.Name)
	}
}

func TestForeignKeyDef_Named(t *testing.T) {
	d := orm.NewForeignKeyDef(orderTable.OrgID).ReferencesTable("orders", "org_id").Named("fk_custom")
	if d.Name() != "fk_custom" {
		t.Errorf("Name() = %q, want fk_custom", d.Name())
	}
	if d.ReferencedTable() != "orders" {
		t.Errorf("ReferencedTable() = %q, want orders", d.ReferencedTable())
	}
}

func TestForeignKeyDef_ChainOrderIndependence(t *testing.T) {
	a := orm.NewForeignKeyDef(orderTable.OrgID).
		References(orderTable.OrgID).OnDelete(orm.ActionSetNull).OnUpdate(orm.ActionCascade)
	b := orm.NewForeignKeyDef(orderTable.OrgID).
		OnUpdate(orm.ActionCascade).OnDelete(orm.ActionSetNull).References(orderTable.OrgID)
	if a.OnDeleteAction() != b.OnDeleteAction() || a.OnUpdateAction() != b.OnUpdateAction() ||
		a.ReferencedTable() != b.ReferencedTable() {
		t.Error("the builder is order dependent")
	}
}

func extractErr(t *testing.T, m orm.Model) string {
	t.Helper()
	_, err := schema.ExtractSchema(m)
	if err == nil {
		t.Fatal("ExtractSchema() error = nil, want a failure")
	}
	return err.Error()
}

type badFKModel struct {
	orm.Table[orm.NoEntity]
	A    *orm.IntColumn
	B    *orm.IntColumn
	defs []orm.ForeignKeyDef
}

func (m *badFKModel) ForeignKeys() []orm.ForeignKeyDef { return m.defs }

func newBadFK(defs ...orm.ForeignKeyDef) *badFKModel {
	return &badFKModel{
		Table: orm.NewTable[orm.NoEntity]("bad"),
		A:     orm.NewIntColumn("a"),
		B:     orm.NewIntColumn("b"),
		defs:  defs,
	}
}

func TestForeignKeyDef_NoColumns(t *testing.T) {
	got := extractErr(t, newBadFK(orm.NewForeignKeyDef().ReferencesTable("orders", "id")))
	if !strings.Contains(got, "no columns") {
		t.Errorf("error %q does not report the empty column list", got)
	}
}

// A key whose target was never bound to a table references nothing, which
// would otherwise produce a constraint pointing at "".
func TestForeignKeyDef_UnboundTarget(t *testing.T) {
	m := newBadFK(orm.NewForeignKeyDef(orm.NewIntColumn("a")).References(orm.NewIntColumn("id")))
	got := extractErr(t, m)
	if !strings.Contains(got, "references no table") {
		t.Errorf("error %q does not report the unbound target", got)
	}
}

// Order carries the pairing, so a mismatched count would silently pair the
// wrong columns rather than fail.
func TestForeignKeyDef_ArityMismatch(t *testing.T) {
	m := newBadFK(orm.NewForeignKeyDef(orm.NewIntColumn("a"), orm.NewIntColumn("b")).
		ReferencesTable("orders", "id"))
	got := extractErr(t, m)
	for _, want := range []string{"references 1 columns", "want 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("error %q does not mention %q", got, want)
		}
	}
}
