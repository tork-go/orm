package orm_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
)

type aliasedEntity struct {
	ID   int
	Name string
}

type aliasedModel struct {
	orm.Table[aliasedEntity]
	ID   *orm.IntColumn
	Name *orm.StringColumn
}

var aliasedTable = orm.DefineTable[aliasedEntity]("aliased",
	func(b *orm.TableBuilder[aliasedEntity]) *aliasedModel {
		return &aliasedModel{
			Table: b.Table(),
			ID:    b.Int("id").PrimaryKey(),
			Name:  b.String("name").NotNull(),
		}
	})

// An alias is the same model type with the same typed columns, each bound
// to the second name rather than the stored one.
func TestAlias_RebuildsTheModelUnderTheNewName(t *testing.T) {
	other := orm.Alias(aliasedTable, "other")

	if got := other.TableName(); got != "other" {
		t.Errorf("TableName() = %q, want %q", got, "other")
	}
	for _, c := range orm.Columns(other) {
		if c.OwnerTable() != "other" {
			t.Errorf("column %q owned by %q, want %q", c.Name(), c.OwnerTable(), "other")
		}
	}
	// A second model, not the first renamed.
	if other == aliasedTable {
		t.Error("Alias returned the model it was given, want a second one")
	}
	if got := aliasedTable.ID.OwnerTable(); got != "aliased" {
		t.Errorf("the aliased table's own column is owned by %q, want %q", got, "aliased")
	}
}

// Every column keeps what it was declared with; only its owner differs.
func TestAlias_ColumnsKeepTheirDeclaration(t *testing.T) {
	other := orm.Alias(aliasedTable, "other")
	if !other.ID.IsPrimaryKey() {
		t.Error("the alias's id is not a primary key, want the declaration carried over")
	}
	if !other.Name.HasNotNull() {
		t.Error("the alias's name is nullable, want the declaration carried over")
	}
}

// Model asks only for a TableName, so a type can satisfy it while carrying
// no table at all. There is nothing there to alias.
func TestAlias_ModelWithNoIdentity_Panics(t *testing.T) {
	got := mustPanic(t, func() { orm.Alias(nameOnlyModel{}, "n") })
	if !strings.Contains(got, "no table identity") {
		t.Errorf("panic = %q, want it to say the model carries no table", got)
	}
}

type nameOnlyModel struct{}

func (nameOnlyModel) TableName() string { return "name_only" }

// A model built by NewTable has no build function to run a second time,
// which is the one thing aliasing needs.
func TestAlias_NewTableModel_Panics(t *testing.T) {
	type model struct {
		orm.Table[orm.NoEntity]
		ID *orm.IntColumn
	}
	m := &model{Table: orm.NewTable[orm.NoEntity]("hand_built"), ID: orm.NewIntColumn("id")}

	got := mustPanic(t, func() { orm.Alias(m, "hb") })
	if !strings.Contains(got, "NewTable") {
		t.Errorf("panic = %q, want it to name NewTable", got)
	}
}

// A derived table's name is already the alias its subquery is wrapped
// under, so a second one would name nothing further.
func TestAlias_DerivedModel_Panics(t *testing.T) {
	type row struct{ Name string }
	type model struct {
		orm.DerivedTable[row]
		Name *orm.StringColumn
	}
	d := orm.DefineDerived[row]("derived", func(b *orm.TableBuilder[row]) *model {
		return &model{DerivedTable: b.Derived(), Name: b.String("name")}
	})

	got := mustPanic(t, func() { orm.Alias(d, "d2") })
	if !strings.Contains(got, "derived table") {
		t.Errorf("panic = %q, want it to name the derived table", got)
	}
}

func TestAlias_EmptyName_Panics(t *testing.T) {
	got := mustPanic(t, func() { orm.Alias(aliasedTable, "") })
	if !strings.Contains(got, "empty name") {
		t.Errorf("panic = %q, want it to name the empty alias", got)
	}
}

// Two of one name in a statement is exactly what an alias exists to avoid.
func TestAlias_OwnName_Panics(t *testing.T) {
	got := mustPanic(t, func() { orm.Alias(aliasedTable, "aliased") })
	if !strings.Contains(got, "own name") {
		t.Errorf("panic = %q, want it to say the name was the table's own", got)
	}
}

// Aliasing an alias names the stored table, so the chain never grows.
func TestAlias_OfAnAliasNamesStorage(t *testing.T) {
	first := orm.Alias(aliasedTable, "first")
	second := orm.Alias(first, "second")
	if got := second.TableName(); got != "second" {
		t.Errorf("TableName() = %q, want %q", got, "second")
	}
	// Aliasing the stored table's own name through an alias is refused for
	// the same reason aliasing it directly is: the two would be one name.
	got := mustPanic(t, func() { orm.Alias(first, "aliased") })
	if !strings.Contains(got, "own name") {
		t.Errorf("panic = %q, want it to say the name was the table's own", got)
	}
}
