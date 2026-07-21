package orm_test

import (
	"testing"

	"github.com/tork-go/orm"
)

// Foreign keys are not a distinct column type. Any column may reference
// another, and what makes a field a foreign key is having a referenced
// table rather than satisfying an interface. These tests cover that
// distinction, the type check References imposes, and the two ways a
// reference can be spelled.

type refEntity struct {
	ID       int
	AuthorID int
}

type refModel struct {
	orm.Table[refEntity]
	ID       *orm.IntColumn
	AuthorID *orm.IntColumn
}

type refTargetEntity struct{ ID int }

type refTargetModel struct {
	orm.Table[refTargetEntity]
	ID *orm.IntColumn
}

var refTarget = orm.DefineTable[refTargetEntity]("authors",
	func(t *orm.TableBuilder[refTargetEntity]) *refTargetModel {
		return &refTargetModel{Table: t.Table(), ID: t.Int("id").PrimaryKey()}
	})

var refSource = orm.DefineTable[refEntity]("articles",
	func(t *orm.TableBuilder[refEntity]) *refModel {
		return &refModel{
			Table:    t.Table(),
			ID:       t.Int("id").PrimaryKey(),
			AuthorID: t.Int("author_id").NotNull().References(refTarget.ID),
		}
	})

// The referenced table name comes from the target column, so a caller
// never repeats it and it can never drift from the table it names.
func TestReferences_ResolvesTargetFromColumn(t *testing.T) {
	if got := refSource.AuthorID.ReferencedTable(); got != "authors" {
		t.Errorf("ReferencedTable() = %q, want %q", got, "authors")
	}
	if got := refSource.AuthorID.ReferencedColumn(); got != "id" {
		t.Errorf("ReferencedColumn() = %q, want %q", got, "id")
	}
}

// Only columns carrying a reference are foreign keys, even though every
// column satisfies ForeignKeyMeta.
func TestForeignKeys_OnlyReferencingColumns(t *testing.T) {
	fks := orm.ForeignKeys(refSource)
	if len(fks) != 1 {
		t.Fatalf("ForeignKeys() returned %d, want 1 (only author_id references anything)", len(fks))
	}
	if got := fks[0].Name(); got != "author_id" {
		t.Errorf("ForeignKeys()[0].Name() = %q, want %q", got, "author_id")
	}

	// The key is still an ordinary column of the table.
	var found bool
	for _, c := range orm.Columns(refSource) {
		if c.Name() == "author_id" {
			found = true
		}
	}
	if !found {
		t.Error("author_id missing from Columns(): a foreign key is still a column")
	}
}

func TestForeignKeys_NoneWhenNothingReferences(t *testing.T) {
	if got := orm.ForeignKeys(refTarget); len(got) != 0 {
		t.Errorf("ForeignKeys() returned %d, want 0", len(got))
	}
}

func TestColumn_ReferencesNothingByDefault(t *testing.T) {
	c := orm.NewIntColumn("plain")
	if got := c.ReferencedTable(); got != "" {
		t.Errorf("ReferencedTable() = %q, want %q on a plain column", got, "")
	}
	if got := c.ReferencedColumn(); got != "" {
		t.Errorf("ReferencedColumn() = %q, want %q on a plain column", got, "")
	}
	if c.OnDeleteAction() != orm.ActionNoAction || c.OnUpdateAction() != orm.ActionNoAction {
		t.Errorf("OnDeleteAction()=%v OnUpdateAction()=%v, want both ActionNoAction",
			c.OnDeleteAction(), c.OnUpdateAction())
	}
}

// ReferencesTable is the escape hatch for a target that has no bound
// table: a table outside this program, or a model built without
// DefineTable.
func TestReferencesTable_ExplicitTarget(t *testing.T) {
	c := orm.NewIntColumn("author_id").ReferencesTable("legacy_authors", "author_pk")
	if got := c.ReferencedTable(); got != "legacy_authors" {
		t.Errorf("ReferencedTable() = %q, want %q", got, "legacy_authors")
	}
	if got := c.ReferencedColumn(); got != "author_pk" {
		t.Errorf("ReferencedColumn() = %q, want %q", got, "author_pk")
	}
}

// An unbound target reports no table, so the reference is indistinguishable
// from none and the column is not treated as a foreign key. That is the
// documented reason References wants a column DefineTable has seen.
func TestReferences_UnboundTargetIsNotAForeignKey(t *testing.T) {
	type model struct {
		orm.Table[orm.NoEntity]
		AuthorID *orm.IntColumn
	}
	loose := orm.NewIntColumn("id") // never bound to a table
	m := &model{
		Table:    orm.NewTable[orm.NoEntity]("articles"),
		AuthorID: orm.NewIntColumn("author_id").References(loose),
	}
	if got := orm.ForeignKeys(m); len(got) != 0 {
		t.Errorf("ForeignKeys() returned %d, want 0 for an unbound target", len(got))
	}
}

func TestReferentialActions(t *testing.T) {
	tests := []struct {
		name   string
		action orm.ForeignKeyAction
	}{
		{"NoAction", orm.ActionNoAction},
		{"Cascade", orm.ActionCascade},
		{"SetNull", orm.ActionSetNull},
		{"SetDefault", orm.ActionSetDefault},
		{"Restrict", orm.ActionRestrict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := orm.NewIntColumn("a").ReferencesTable("t", "id").
				OnDelete(tt.action).OnUpdate(tt.action)
			if got := c.OnDeleteAction(); got != tt.action {
				t.Errorf("OnDeleteAction() = %v, want %v", got, tt.action)
			}
			if got := c.OnUpdateAction(); got != tt.action {
				t.Errorf("OnUpdateAction() = %v, want %v", got, tt.action)
			}
		})
	}
}

// The reference builders must return the concrete column type like every
// other builder, or a chain would downcast partway through. These only
// compile if they do.
var (
	_ *orm.IntColumn = orm.NewIntColumn("a").References(refTarget.ID).NotNull()
	_ *orm.IntColumn = orm.NewIntColumn("a").ReferencesTable("t", "id").
		OnDelete(orm.ActionCascade).OnUpdate(orm.ActionRestrict).Index()
	_ *orm.NullableIntColumn = orm.NewNullableIntColumn("a").References(refTarget.ID)
	_ *orm.StringColumn      = orm.NewStringColumn("a").ReferencesTable("t", "code").MaxLen(10)
	_ *orm.Column[int]       = orm.NewColumn[int]("a").References(refTarget.ID).NotNull()
)

// A nullable key pointing at a non-nullable primary key is the ordinary
// shape of an optional relationship, so References must accept it: the
// column is *int, the target is int.
func TestReferences_NullableKeyOntoNonNullableTarget(t *testing.T) {
	c := orm.NewNullableIntColumn("author_id").References(refTarget.ID)
	if got := c.ReferencedTable(); got != "authors" {
		t.Errorf("ReferencedTable() = %q, want %q", got, "authors")
	}
	if !c.IsNullable() {
		t.Error("IsNullable() = false, want true: the key itself is still optional")
	}
}

// A column may reference one declared beside it in the same table. This
// works only because the target is resolved when read rather than when
// References is called: nothing has bound either column to the table yet
// at the point the model literal is evaluated.
func TestReferences_SelfReferencingTable(t *testing.T) {
	type entity struct {
		ID       int
		ParentID *int
	}
	type model struct {
		orm.Table[entity]
		ID       *orm.IntColumn
		ParentID *orm.NullableIntColumn
	}

	m := orm.DefineTable[entity]("categories", func(t *orm.TableBuilder[entity]) *model {
		id := t.Int("id").PrimaryKey()
		return &model{
			Table:    t.Table(),
			ID:       id,
			ParentID: t.NullableInt("parent_id").References(id),
		}
	})

	if got := m.ParentID.ReferencedTable(); got != "categories" {
		t.Errorf("ReferencedTable() = %q, want %q", got, "categories")
	}
	if got := m.ParentID.ReferencedColumn(); got != "id" {
		t.Errorf("ReferencedColumn() = %q, want %q", got, "id")
	}
}

func BenchmarkReferenceBuilderChain(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = orm.NewIntColumn("author_id").NotNull().Index().
			ReferencesTable("users", "id").OnDelete(orm.ActionCascade)
	}
}
