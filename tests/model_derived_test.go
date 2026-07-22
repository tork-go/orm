package orm_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// A stored table and a derived table declared for the same row type, which
// is what TestDefineDerived_DoesNotRegisterTheRowType is about: the
// relationship must keep resolving to the stored one.
type dsParent struct{ ID int }

type dsChild struct {
	ID       int
	ParentID int
}

type dsParentModel struct {
	orm.Table[dsParent]
	ID       *orm.IntColumn
	Children orm.HasMany[dsChild]
}

type dsChildModel struct {
	orm.Table[dsChild]
	ID       *orm.IntColumn
	ParentID *orm.IntColumn
}

var dsParents = orm.DefineTable[dsParent]("ds_parents",
	func(t *orm.TableBuilder[dsParent]) *dsParentModel {
		return &dsParentModel{Table: t.Table(), ID: t.Int("id").PrimaryKey()}
	})

var dsChildren = orm.DefineTable[dsChild]("ds_children",
	func(t *orm.TableBuilder[dsChild]) *dsChildModel {
		return &dsChildModel{
			Table:    t.Table(),
			ID:       t.Int("id").PrimaryKey(),
			ParentID: t.Int("parent_id").NotNull().References(dsParents.ID),
		}
	})

type dsChildDerivedModel struct {
	orm.DerivedTable[dsChild]
	ID       *orm.IntColumn
	ParentID *orm.IntColumn
}

// Declared after the stored table, over the same row type. If DefineDerived
// registered, this would be what the relationship above resolves to.
var _ = orm.DefineDerived[dsChild]("ds_children_derived",
	func(t *orm.TableBuilder[dsChild]) *dsChildDerivedModel {
		return &dsChildDerivedModel{
			DerivedTable: t.Derived(),
			ID:           t.Int("id"),
			ParentID:     t.Int("parent_id"),
		}
	})

var _ = dsChildren // the stored child model is reached through the relationship

// oddModel is a model that is not a struct, which Model permits: it asks
// only for TableName.
type oddModel string

func (o oddModel) TableName() string { return string(o) }

type ranked struct {
	Username string
	Rank     int64
}

type rankedModel struct {
	orm.DerivedTable[ranked]
	Username *orm.StringColumn
	Rank     *orm.BigIntColumn
}

var rankedT = orm.DefineDerived[ranked]("ranked",
	func(t *orm.TableBuilder[ranked]) *rankedModel {
		return &rankedModel{
			DerivedTable: t.Derived(),
			Username:     t.String("username"),
			Rank:         t.BigInt("rank"),
		}
	})

// A derived model is declared exactly as a stored one is, so its columns are
// ordinary typed handles bound to the table.
func TestDefineDerived_DeclaresColumns(t *testing.T) {
	if got := rankedT.TableName(); got != "ranked" {
		t.Errorf("TableName() = %q, want ranked", got)
	}
	cols := orm.Columns(rankedT)
	if len(cols) != 2 {
		t.Fatalf("Columns() returned %d, want 2", len(cols))
	}
	if cols[0].Name() != "username" || cols[1].Name() != "rank" {
		t.Errorf("columns = %q, %q, want username, rank", cols[0].Name(), cols[1].Name())
	}
	// Bound to the table, which is what lets a statement qualify them.
	for _, c := range cols {
		if c.OwnerTable() != "ranked" {
			t.Errorf("column %q reports owner %q, want ranked", c.Name(), c.OwnerTable())
		}
	}
}

// The row type is not registered: the registry is how a relationship finds
// the table for a row type, and registering a derived one would shadow
// whatever real table was declared for the same type.
//
// That is only observable through something that consults the registry, so
// this resolves a relationship pointing at the shared row type and checks it
// still names the stored table.
func TestDefineDerived_DoesNotRegisterTheRowType(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	sql, _, err := dsParents.With(db).Where(orm.Has(dsParents.Children)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `FROM "ds_children" WHERE`) {
		t.Errorf("SQL() = %s\nwant the relationship to resolve to the stored table", sql)
	}
	if strings.Contains(sql, "ds_children_derived") {
		t.Errorf("SQL() = %s\nthe derived table shadowed the stored one in the registry", sql)
	}
}

func TestDefineDerived_RelationshipRejected(t *testing.T) {
	type leaf struct{ ID int }
	type withRel struct {
		orm.DerivedTable[ranked]
		Username *orm.StringColumn
		Leaves   orm.HasMany[leaf]
	}
	got := mustPanic(t, func() {
		orm.DefineDerived[ranked]("bad_derived",
			func(t *orm.TableBuilder[ranked]) *withRel {
				return &withRel{DerivedTable: t.Derived(), Username: t.String("username")}
			})
	})
	if !strings.Contains(got, "cannot have") || !strings.Contains(got, "Leaves") {
		t.Errorf("panic message %q does not name the relationship field", got)
	}
}

// The identity has to come from the builder, and the message says which
// field to set rather than repeating the stored table's advice.
func TestDefineDerived_IdentityMustComeFromTheBuilder(t *testing.T) {
	type model struct {
		orm.DerivedTable[ranked]
		Username *orm.StringColumn
	}
	got := mustPanic(t, func() {
		orm.DefineDerived[ranked]("unset",
			func(t *orm.TableBuilder[ranked]) *model {
				// DerivedTable deliberately left unset.
				return &model{Username: t.String("username")}
			})
	})
	if !strings.Contains(got, "t.Derived()") {
		t.Errorf("panic message %q does not say to set DerivedTable from the builder", got)
	}
}

// An unexported field is skipped rather than inspected, the same way the
// relationship binding skips one on a stored model.
func TestDefineDerived_UnexportedFieldIgnored(t *testing.T) {
	type model struct {
		orm.DerivedTable[ranked]
		Username *orm.StringColumn
		Rank     *orm.BigIntColumn
		note     string // present precisely to be skipped
	}
	m := orm.DefineDerived[ranked]("with_unexported",
		func(t *orm.TableBuilder[ranked]) *model {
			return &model{
				DerivedTable: t.Derived(),
				Username:     t.String("username"),
				Rank:         t.BigInt("rank"),
			}
		})
	if got := len(orm.Columns(m)); got != 2 {
		t.Errorf("Columns() returned %d, want 2", got)
	}
}

// A model need not be a struct: Model asks only for TableName. One that is
// not has no fields to carry a relationship, and is accepted rather than
// tripping over the walk.
func TestDefineDerived_NonStructModel(t *testing.T) {
	m := orm.DefineDerived[struct{}]("odd", func(*orm.TableBuilder[struct{}]) oddModel {
		return oddModel("odd")
	})
	if got := m.TableName(); got != "odd" {
		t.Errorf("TableName() = %q, want odd", got)
	}
}

// A column with no matching field is caught the same way a stored table's
// is, since both go through the same resolution.
func TestDefineDerived_ColumnWithNoFieldPanics(t *testing.T) {
	type model struct {
		orm.DerivedTable[ranked]
		Username *orm.StringColumn
		Nope     *orm.StringColumn
	}
	got := mustPanic(t, func() {
		orm.DefineDerived[ranked]("mismatched",
			func(t *orm.TableBuilder[ranked]) *model {
				return &model{
					DerivedTable: t.Derived(),
					Username:     t.String("username"),
					Nope:         t.String("nope"),
				}
			})
	})
	if !strings.Contains(got, "nope") {
		t.Errorf("panic message %q does not name the unmatched column", got)
	}
}
