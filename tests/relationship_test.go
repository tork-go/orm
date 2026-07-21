package orm_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/tests/fixtures"
)

// A marker names only the related row type, so resolving one means finding
// the table declared for that type and reading the keys already on both
// sides. The fixtures are the ordinary case: posts.author_id references
// users.id, and nothing else joins the two.
func TestHasMany_InfersTheKey(t *testing.T) {
	got, err := fixtures.Users.Posts.Relation()
	if err != nil {
		t.Fatalf("Relation() error = %v", err)
	}
	if got.Kind != orm.KindHasMany {
		t.Errorf("Kind = %v, want HasMany", got.Kind)
	}
	if got.LocalTable != "users" || got.ForeignTable != "posts" {
		t.Errorf("tables = %s -> %s, want users -> posts", got.LocalTable, got.ForeignTable)
	}
	// The key lives on the far side, so the local column is the one it
	// points at.
	if got.LocalColumn.Name() != "id" {
		t.Errorf("LocalColumn = %q, want id", got.LocalColumn.Name())
	}
	if got.ForeignColumn.Name() != "author_id" {
		t.Errorf("ForeignColumn = %q, want author_id", got.ForeignColumn.Name())
	}
}

// BelongsTo is the mirror: the declaring table owns the key.
func TestBelongsTo_InfersTheKey(t *testing.T) {
	got, err := fixtures.Posts.Author.Relation()
	if err != nil {
		t.Fatalf("Relation() error = %v", err)
	}
	if got.Kind != orm.KindBelongsTo {
		t.Errorf("Kind = %v, want BelongsTo", got.Kind)
	}
	if got.LocalColumn.Name() != "author_id" {
		t.Errorf("LocalColumn = %q, want author_id", got.LocalColumn.Name())
	}
	if got.ForeignColumn.Name() != "id" {
		t.Errorf("ForeignColumn = %q, want id", got.ForeignColumn.Name())
	}
	if got.LocalTable != "posts" || got.ForeignTable != "users" {
		t.Errorf("tables = %s -> %s, want posts -> users", got.LocalTable, got.ForeignTable)
	}
}

// Resolution is cached, so a repeated call cannot give a different answer
// or redo the work.
func TestRelation_IsResolvedOnce(t *testing.T) {
	first, err := fixtures.Users.Posts.Relation()
	if err != nil {
		t.Fatalf("Relation() error = %v", err)
	}
	second, _ := fixtures.Users.Posts.Relation()
	if first.ForeignColumn != second.ForeignColumn {
		t.Error("Relation() gave two different answers")
	}
}

type noKeyEntity struct{ ID int }
type noKeyModel struct {
	orm.Table[noKeyEntity]
	ID    *orm.IntColumn
	Other orm.HasMany[strandedEntity]
}

type strandedEntity struct{ ID int }
type strandedModel struct {
	orm.Table[strandedEntity]
	ID *orm.IntColumn
}

var (
	_ = orm.DefineTable[strandedEntity]("stranded", func(t *orm.TableBuilder[strandedEntity]) *strandedModel {
		return &strandedModel{Table: t.Table(), ID: t.Int("id")}
	})
	noKeyTable = orm.DefineTable[noKeyEntity]("no_key", func(t *orm.TableBuilder[noKeyEntity]) *noKeyModel {
		return &noKeyModel{Table: t.Table(), ID: t.Int("id")}
	})
)

func TestRelation_NoKeyBetweenTheTables(t *testing.T) {
	_, err := noKeyTable.Other.Relation()
	if err == nil {
		t.Fatal("Relation() error = nil, want a missing key error")
	}
	for _, want := range []string{"no column on stranded references no_key", "References", "Relations"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// Two keys into the same table give inference nothing to choose on, so it
// has to say so rather than pick one.
type authorEntity struct{ ID int }
type authorModel struct {
	orm.Table[authorEntity]
	ID    *orm.IntColumn
	Wrote orm.HasMany[articleEntity]
}

type articleEntity struct {
	ID       int
	AuthorID int
	EditorID int
}
type articleModel struct {
	orm.Table[articleEntity]
	ID       *orm.IntColumn
	AuthorID *orm.IntColumn
	EditorID *orm.IntColumn
}

var (
	authorTable = orm.DefineTable[authorEntity]("authors", func(t *orm.TableBuilder[authorEntity]) *authorModel {
		return &authorModel{Table: t.Table(), ID: t.Int("id").PrimaryKey()}
	})
	articleTable = orm.DefineTable[articleEntity]("articles2", func(t *orm.TableBuilder[articleEntity]) *articleModel {
		return &articleModel{
			Table:    t.Table(),
			ID:       t.Int("id").PrimaryKey(),
			AuthorID: t.Int("author_id").References(authorTable.ID),
			EditorID: t.Int("editor_id").References(authorTable.ID),
		}
	})
)

func TestRelation_AmbiguousKey(t *testing.T) {
	_, err := authorTable.Wrote.Relation()
	if err == nil {
		t.Fatal("Relation() error = nil, want an ambiguity error")
	}
	for _, want := range []string{"2 columns referencing authors", `"author_id"`, `"editor_id"`, "Relations"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	_ = articleTable
}

// Relations names the key when inference cannot. It is a method rather
// than something declared beside the columns because a method body is not
// part of any variable's initialiser, so it can mention another table
// without closing an initialisation cycle.
type taggedEntity struct{ ID int }
type taggedModel struct {
	orm.Table[taggedEntity]
	ID      *orm.IntColumn
	Primary orm.HasMany[itemEntity]
}

func (m *taggedModel) Relations() []orm.RelationDef {
	return []orm.RelationDef{orm.Via(&m.Primary, itemTable.PrimaryTagID)}
}

type itemEntity struct {
	ID           int
	PrimaryTagID int
	BackupTagID  int
}
type itemModel struct {
	orm.Table[itemEntity]
	ID           *orm.IntColumn
	PrimaryTagID *orm.IntColumn
	BackupTagID  *orm.IntColumn
}

var (
	taggedTable = orm.DefineTable[taggedEntity]("tags", func(t *orm.TableBuilder[taggedEntity]) *taggedModel {
		return &taggedModel{Table: t.Table(), ID: t.Int("id").PrimaryKey()}
	})
	itemTable = orm.DefineTable[itemEntity]("items", func(t *orm.TableBuilder[itemEntity]) *itemModel {
		return &itemModel{
			Table:        t.Table(),
			ID:           t.Int("id").PrimaryKey(),
			PrimaryTagID: t.Int("primary_tag_id").References(taggedTable.ID),
			BackupTagID:  t.Int("backup_tag_id").References(taggedTable.ID),
		}
	})
)

func TestRelations_NamesTheKeyExplicitly(t *testing.T) {
	got, err := taggedTable.Primary.Relation()
	if err != nil {
		t.Fatalf("Relation() error = %v: two keys join these tables, and Relations named one", err)
	}
	if got.ForeignColumn.Name() != "primary_tag_id" {
		t.Errorf("ForeignColumn = %q, want primary_tag_id", got.ForeignColumn.Name())
	}
}

// A many to many needs a join table that nothing in the declaration names,
// so it reports that rather than inferring the wrong join.
type crewEntity struct{ ID int }
type crewModel struct {
	orm.Table[crewEntity]
	ID     *orm.IntColumn
	Shifts orm.ManyToMany[strandedEntity]
}

var crewTable = orm.DefineTable[crewEntity]("crew", func(t *orm.TableBuilder[crewEntity]) *crewModel {
	return &crewModel{Table: t.Table(), ID: t.Int("id")}
})

func TestManyToMany_ReportsUnsupported(t *testing.T) {
	_, err := crewTable.Shifts.Relation()
	if err == nil {
		t.Fatal("Relation() error = nil, want an unsupported error")
	}
	if !strings.Contains(err.Error(), "not supported yet") {
		t.Errorf("error %q does not say many to many is unbuilt", err)
	}
}

// A marker on a model built with NewTable was never attached to a table.
func TestRelation_UnboundModel(t *testing.T) {
	type model struct {
		orm.Table[orm.NoEntity]
		Others orm.HasMany[strandedEntity]
	}
	m := &model{Table: orm.NewTable[orm.NoEntity]("loose")}

	_, err := m.Others.Relation()
	if err == nil {
		t.Fatal("Relation() error = nil, want an unattached error")
	}
	if !strings.Contains(err.Error(), "DefineTable") {
		t.Errorf("error %q does not point at DefineTable", err)
	}
}

// The markers stay legal left uninitialised, which is what lets a model
// mention a table declared after it.
func TestMarkers_ZeroValueInLiteral(t *testing.T) {
	type model struct {
		orm.Table[orm.NoEntity]
		A orm.HasMany[strandedEntity]
		B orm.HasOne[strandedEntity]
		C orm.BelongsTo[strandedEntity]
		D orm.ManyToMany[strandedEntity]
	}
	m := &model{Table: orm.NewTable[orm.NoEntity]("markers")}
	if got := len(orm.Columns(m)); got != 0 {
		t.Errorf("Columns() found %d columns, want 0: markers are not columns", got)
	}
}

func TestRelationKind_String(t *testing.T) {
	tests := []struct {
		kind orm.RelationKind
		want string
	}{
		{orm.KindHasMany, "HasMany"},
		{orm.KindHasOne, "HasOne"},
		{orm.KindBelongsTo, "BelongsTo"},
		{orm.KindManyToMany, "ManyToMany"},
		{orm.RelationKind(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("RelationKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

// HasOne resolves exactly as HasMany does; the two differ only in what a
// model says about how many rows to expect.
type accountEntity struct{ ID int }
type accountModel struct {
	orm.Table[accountEntity]
	ID      *orm.IntColumn
	Profile orm.HasOne[profileEntity]
}

type profileEntity struct {
	ID        int
	AccountID int
}
type profileModel struct {
	orm.Table[profileEntity]
	ID        *orm.IntColumn
	AccountID *orm.IntColumn
	Account   orm.BelongsTo[accountEntity]
}

var (
	accountTable = orm.DefineTable[accountEntity]("accounts", func(t *orm.TableBuilder[accountEntity]) *accountModel {
		return &accountModel{Table: t.Table(), ID: t.Int("id").PrimaryKey()}
	})
	profileTable = orm.DefineTable[profileEntity]("profiles", func(t *orm.TableBuilder[profileEntity]) *profileModel {
		return &profileModel{
			Table:     t.Table(),
			ID:        t.Int("id").PrimaryKey(),
			AccountID: t.Int("account_id").Unique().References(accountTable.ID),
		}
	})
)

func TestHasOne_InfersTheKey(t *testing.T) {
	got, err := accountTable.Profile.Relation()
	if err != nil {
		t.Fatalf("Relation() error = %v", err)
	}
	if got.Kind != orm.KindHasOne {
		t.Errorf("Kind = %v, want HasOne", got.Kind)
	}
	if got.LocalColumn.Name() != "id" || got.ForeignColumn.Name() != "account_id" {
		t.Errorf("join = %s.%s -> %s.%s, want accounts.id -> profiles.account_id",
			got.LocalTable, got.LocalColumn.Name(), got.ForeignTable, got.ForeignColumn.Name())
	}
}

// Via has to work for every marker, not just HasMany.
type ownerEntity struct{ ID int }
type ownerModel struct {
	orm.Table[ownerEntity]
	ID *orm.IntColumn
}

type petEntity struct {
	ID      int
	OwnerID int
	VetID   int
}
type petModel struct {
	orm.Table[petEntity]
	ID      *orm.IntColumn
	OwnerID *orm.IntColumn
	VetID   *orm.IntColumn
	Owner   orm.BelongsTo[ownerEntity]
}

func (m *petModel) Relations() []orm.RelationDef {
	return []orm.RelationDef{orm.Via(&m.Owner, m.OwnerID)}
}

var (
	ownerTable = orm.DefineTable[ownerEntity]("owners", func(t *orm.TableBuilder[ownerEntity]) *ownerModel {
		return &ownerModel{Table: t.Table(), ID: t.Int("id").PrimaryKey()}
	})
	petTable = orm.DefineTable[petEntity]("pets", func(t *orm.TableBuilder[petEntity]) *petModel {
		return &petModel{
			Table:   t.Table(),
			ID:      t.Int("id").PrimaryKey(),
			OwnerID: t.Int("owner_id").References(ownerTable.ID),
			VetID:   t.Int("vet_id").References(ownerTable.ID),
		}
	})
)

func TestVia_OnBelongsTo(t *testing.T) {
	got, err := petTable.Owner.Relation()
	if err != nil {
		t.Fatalf("Relation() error = %v: two keys join these tables and Relations named one", err)
	}
	if got.LocalColumn.Name() != "owner_id" {
		t.Errorf("LocalColumn = %q, want owner_id", got.LocalColumn.Name())
	}
}

// A key pointing at a column the target does not declare cannot produce a
// join, which is reachable through ReferencesTable naming anything.
type danglingEntity struct{ ID int }
type danglingModel struct {
	orm.Table[danglingEntity]
	ID    *orm.IntColumn
	Items orm.HasMany[danglingChildEntity]
}

type danglingChildEntity struct {
	ID       int
	ParentID int
}
type danglingChildModel struct {
	orm.Table[danglingChildEntity]
	ID       *orm.IntColumn
	ParentID *orm.IntColumn
}

var (
	danglingTable = orm.DefineTable[danglingEntity]("dangling", func(t *orm.TableBuilder[danglingEntity]) *danglingModel {
		return &danglingModel{Table: t.Table(), ID: t.Int("id")}
	})
	_ = orm.DefineTable[danglingChildEntity]("dangling_children", func(t *orm.TableBuilder[danglingChildEntity]) *danglingChildModel {
		return &danglingChildModel{
			Table:    t.Table(),
			ID:       t.Int("id"),
			ParentID: t.Int("parent_id").ReferencesTable("dangling", "no_such_column"),
		}
	})
)

func TestRelation_KeyReferencesUnknownColumn(t *testing.T) {
	_, err := danglingTable.Items.Relation()
	if err == nil {
		t.Fatal("Relation() error = nil, want a dangling reference error")
	}
	if !strings.Contains(err.Error(), "no_such_column") {
		t.Errorf("error %q does not name the missing column", err)
	}
}

// Relations naming a nil column is a mistake in the model, reported when
// the relationship is used rather than silently falling back to inference.
type nilKeyEntity struct{ ID int }
type nilKeyModel struct {
	orm.Table[nilKeyEntity]
	ID   *orm.IntColumn
	Kids orm.HasMany[strandedEntity]
}

func (m *nilKeyModel) Relations() []orm.RelationDef {
	return []orm.RelationDef{orm.Via(&m.Kids, nil)}
}

var nilKeyTable = orm.DefineTable[nilKeyEntity]("nil_key", func(t *orm.TableBuilder[nilKeyEntity]) *nilKeyModel {
	return &nilKeyModel{Table: t.Table(), ID: t.Int("id")}
})

func TestRelations_NilColumnIsReported(t *testing.T) {
	_, err := nilKeyTable.Kids.Relation()
	if err == nil {
		t.Fatal("Relation() error = nil, want a nil column error")
	}
	if !strings.Contains(err.Error(), "nil column") {
		t.Errorf("error %q does not report the nil column", err)
	}
}

// Binding writes to the marker the model holds, so the model has to be
// addressable. A value model is otherwise fine, so this is reported rather
// than leaving the relationships quietly unusable.
type valueRelEntity struct{ ID int }
type valueRelModel struct {
	orm.Table[valueRelEntity]
	ID   *orm.IntColumn
	Kids orm.HasMany[strandedEntity]
}

func TestDefineTable_ValueModelWithRelationship_Panics(t *testing.T) {
	got := mustPanic(t, func() {
		orm.DefineTable[valueRelEntity]("value_rel", func(b *orm.TableBuilder[valueRelEntity]) valueRelModel {
			return valueRelModel{Table: b.Table(), ID: b.Int("id")}
		})
	})
	if !strings.Contains(got, "addressable") || !strings.Contains(got, "pointer") {
		t.Errorf("panic message %q does not explain that a pointer model is needed", got)
	}
}

// Via has to reach the relation behind every marker, including the ones
// whose resolution differs.
func TestVia_ReachesEveryMarker(t *testing.T) {
	type entity struct{ ID int }
	type model struct {
		orm.Table[entity]
		ID     *orm.IntColumn
		Many   orm.HasMany[strandedEntity]
		One    orm.HasOne[strandedEntity]
		Belong orm.BelongsTo[strandedEntity]
		M2M    orm.ManyToMany[strandedEntity]
	}
	m := orm.DefineTable[entity]("via_all", func(b *orm.TableBuilder[entity]) *model {
		return &model{Table: b.Table(), ID: b.Int("id")}
	})

	col := orm.NewIntColumn("x")
	for _, def := range []orm.RelationDef{
		orm.Via(&m.Many, col),
		orm.Via(&m.One, col),
		orm.Via(&m.Belong, col),
		orm.Via(&m.M2M, col),
	} {
		if def == (orm.RelationDef{}) {
			t.Error("Via returned a zero RelationDef, so it did not reach the relation")
		}
	}
}

// A key that is a ColumnMeta but not a foreign key names no referenced
// column, so no join can be built from it.
type notAKeyEntity struct{ ID int }
type notAKeyModel struct {
	orm.Table[notAKeyEntity]
	ID   *orm.IntColumn
	Kids orm.HasMany[strandedEntity]
}

func (m *notAKeyModel) Relations() []orm.RelationDef {
	return []orm.RelationDef{orm.Via(&m.Kids, noCodecColumn{orm.NewIntColumn("not_a_key")})}
}

var notAKeyTable = orm.DefineTable[notAKeyEntity]("not_a_key", func(t *orm.TableBuilder[notAKeyEntity]) *notAKeyModel {
	return &notAKeyModel{Table: t.Table(), ID: t.Int("id")}
})

func TestRelations_KeyIsNotAForeignKey(t *testing.T) {
	_, err := notAKeyTable.Kids.Relation()
	if err == nil {
		t.Fatal("Relation() error = nil, want a dangling reference error")
	}
	if !strings.Contains(err.Error(), "not declared on") {
		t.Errorf("error %q does not explain that no join can be built", err)
	}
}

// A model naming one relationship leaves its others to inference, so the
// lookup has to fall through rather than latch onto the first def.
type mixedEntity struct{ ID int }
type mixedModel struct {
	orm.Table[mixedEntity]
	ID       *orm.IntColumn
	Named    orm.HasMany[mixedChildEntity]
	Inferred orm.HasMany[profileEntity]
}

func (m *mixedModel) Relations() []orm.RelationDef {
	return []orm.RelationDef{orm.Via(&m.Named, mixedChildTable.LeftID)}
}

type mixedChildEntity struct {
	ID      int
	LeftID  int
	RightID int
}
type mixedChildModel struct {
	orm.Table[mixedChildEntity]
	ID      *orm.IntColumn
	LeftID  *orm.IntColumn
	RightID *orm.IntColumn
}

var (
	mixedTable = orm.DefineTable[mixedEntity]("mixed", func(t *orm.TableBuilder[mixedEntity]) *mixedModel {
		return &mixedModel{Table: t.Table(), ID: t.Int("id").PrimaryKey()}
	})
	mixedChildTable = orm.DefineTable[mixedChildEntity]("mixed_children", func(t *orm.TableBuilder[mixedChildEntity]) *mixedChildModel {
		return &mixedChildModel{
			Table:   t.Table(),
			ID:      t.Int("id"),
			LeftID:  t.Int("left_id").References(mixedTable.ID),
			RightID: t.Int("right_id").References(mixedTable.ID),
		}
	})
)

func TestRelations_UnnamedRelationshipStillInfers(t *testing.T) {
	named, err := mixedTable.Named.Relation()
	if err != nil {
		t.Fatalf("named relationship: %v", err)
	}
	if named.ForeignColumn.Name() != "left_id" {
		t.Errorf("named ForeignColumn = %q, want left_id", named.ForeignColumn.Name())
	}

	// This one is not named, so it falls through to inference and finds
	// the single key from profiles back to accounts is not it: profiles
	// references accounts, not mixed, so this must report no key.
	if _, err := mixedTable.Inferred.Relation(); err == nil {
		t.Error("unnamed relationship resolved, want it to fall through to inference and fail")
	}
}

// A row type nothing declared a table for cannot be related to, and
// saying so beats resolving to nothing.
type unregisteredEntity struct{ ID int }

type orphanEntity struct{ ID int }
type orphanModel struct {
	orm.Table[orphanEntity]
	ID   *orm.IntColumn
	Kids orm.HasMany[unregisteredEntity]
}

var orphanTable = orm.DefineTable[orphanEntity]("orphans", func(t *orm.TableBuilder[orphanEntity]) *orphanModel {
	return &orphanModel{Table: t.Table(), ID: t.Int("id")}
})

func TestRelation_RelatedTypeHasNoTable(t *testing.T) {
	_, err := orphanTable.Kids.Relation()
	if err == nil {
		t.Fatal("Relation() error = nil, want an unregistered row type error")
	}
	for _, want := range []string{"no table is declared", "DefineTable"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// A model need not be a struct. One that is not has no fields to bind, so
// binding walks away rather than reflecting into something it cannot read.
type bareModel string

func (bareModel) TableName() string { return "bare" }

func TestDefineTable_NonStructModel(t *testing.T) {
	m := orm.DefineTable[orm.NoEntity]("bare", func(*orm.TableBuilder[orm.NoEntity]) bareModel {
		return bareModel("bare")
	})
	if m.TableName() != "bare" {
		t.Errorf("TableName() = %q, want bare", m.TableName())
	}
	if got := len(orm.Columns(m)); got != 0 {
		t.Errorf("Columns() found %d columns on a non-struct model, want 0", got)
	}
}

// Unexported model fields are skipped by binding, as they are by the
// column walk: nothing outside the declaring package could reach them.
type withHiddenEntity struct{ ID int }
type withHiddenModel struct {
	orm.Table[withHiddenEntity]
	ID     *orm.IntColumn
	hidden orm.HasMany[strandedEntity]
}

func TestDefineTable_SkipsUnexportedFields(t *testing.T) {
	m := orm.DefineTable[withHiddenEntity]("with_hidden", func(b *orm.TableBuilder[withHiddenEntity]) *withHiddenModel {
		return &withHiddenModel{Table: b.Table(), ID: b.Int("id")}
	})
	if got := len(orm.Columns(m)); got != 1 {
		t.Errorf("Columns() returned %d columns, want 1", got)
	}
	// The unexported marker was never bound, so it reports that rather
	// than resolving against a table it was never attached to.
	if _, err := m.hidden.Relation(); err == nil {
		t.Error("Relation() on an unexported marker resolved, want an unattached error")
	}
}
