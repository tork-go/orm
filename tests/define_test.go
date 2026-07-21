package orm_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/tork-go/orm"
)

type definedEntity struct {
	ID        int
	Username  string
	Email     *string
	CreatedAt time.Time

	// Renamed by tag, to prove the tag wins over the snake-case rule.
	Nickname string `db:"handle"`

	// Skipped by tag: no column may claim it.
	Internal string `db:"-"`

	// Matches no column, which must be allowed. This is what lets a row
	// type carry related rows and computed values.
	Extra []string

	// Unexported, so invisible to the mapping and to reflection-based
	// scanning alike. A column can never claim it.
	secret string
}

// Referenced so the unexported field is not flagged as unused; its purpose
// is to exist during entity resolution, not to be read.
var _ = definedEntity{}.secret

type definedModel struct {
	orm.Table[definedEntity]
	ID        *orm.IntColumn
	Username  *orm.StringColumn
	Email     *orm.NullableStringColumn
	CreatedAt *orm.TimeColumn
	Handle    *orm.StringColumn
}

var defined = orm.DefineTable[definedEntity]("defined", func(t *orm.TableBuilder[definedEntity]) *definedModel {
	return &definedModel{
		Table:     t.Table(),
		ID:        t.Int("id").PrimaryKey(),
		Username:  t.String("username").Unique().NotNull().MaxLen(30),
		Email:     t.NullableString("email"),
		CreatedAt: t.Time("created_at").ServerDefault("now()"),
		Handle:    t.String("handle"),
	}
})

func TestDefineTable_TableName(t *testing.T) {
	if got := defined.TableName(); got != "defined" {
		t.Errorf("TableName() = %q, want %q", got, "defined")
	}
}

// Binding every column to its table is the thing DefineTable does that a
// hand-built model does not.
func TestDefineTable_BindsColumnsToTable(t *testing.T) {
	for _, c := range orm.Columns(defined) {
		if got := c.OwnerTable(); got != "defined" {
			t.Errorf("column %q: OwnerTable() = %q, want %q", c.Name(), got, "defined")
		}
	}
}

func TestNewColumn_HasNoOwnerTable(t *testing.T) {
	// A column built by hand belongs to no table until something binds it.
	if got := orm.NewStringColumn("loose").OwnerTable(); got != "" {
		t.Errorf("OwnerTable() = %q on an unbound column, want %q", got, "")
	}
}

// Column order is struct field order, which fixes the order a generated
// SELECT lists them and therefore how a positionally-scanned row maps back
// to fields.
func TestDefineTable_ColumnOrderIsFieldOrder(t *testing.T) {
	want := []string{"id", "username", "email", "created_at", "handle"}
	got := orm.Columns(defined)
	if len(got) != len(want) {
		t.Fatalf("Columns() returned %d columns, want %d", len(got), len(want))
	}
	for i, c := range got {
		if c.Name() != want[i] {
			t.Errorf("Columns()[%d].Name() = %q, want %q", i, c.Name(), want[i])
		}
	}
}

func TestDefineTable_BuilderProducesTypedColumns(t *testing.T) {
	// The builder's return types are the point: what is available on each
	// column is decided at the call site, not later.
	if !defined.Username.IsUnique() || !defined.Username.HasNotNull() {
		t.Error("Username lost its builder calls")
	}
	if n, ok := defined.Username.MaxLength(); !ok || n != 30 {
		t.Errorf("Username.MaxLength() = (%d, %v), want (30, true)", n, ok)
	}
	if expr, ok := defined.CreatedAt.ServerDefaultExpr(); !ok || expr != "now()" {
		t.Errorf("CreatedAt.ServerDefaultExpr() = (%q, %v), want (\"now()\", true)", expr, ok)
	}
	// Predicates work off a defined column exactly as off a loose one.
	if p, ok := defined.Username.Contains("ali").(orm.Pattern); !ok || p.Value != "%ali%" {
		t.Errorf("Contains(\"ali\") = %#v, want Pattern with %%ali%%", defined.Username.Contains("ali"))
	}
}

// mustPanic runs fn and returns the panic value as a string, failing if fn
// returns normally. DefineTable reports a bad mapping by panicking, since
// it runs in a package-level var where an error return could not be
// checked; these tests assert the message a developer would actually see.
func mustPanic(t *testing.T, fn func()) string {
	t.Helper()
	var got string
	func() {
		defer func() {
			if r := recover(); r != nil {
				got, _ = r.(string)
			}
		}()
		fn()
		t.Fatal("did not panic, want a panic")
	}()
	return got
}

func TestDefineTable_ColumnWithNoField_Panics(t *testing.T) {
	type entity struct{ ID int }
	type model struct {
		orm.Table[entity]
		ID      *orm.IntColumn
		Missing *orm.StringColumn
	}

	got := mustPanic(t, func() {
		orm.DefineTable[entity]("t", func(b *orm.TableBuilder[entity]) *model {
			return &model{Table: b.Table(), ID: b.Int("id"), Missing: b.String("missing")}
		})
	})

	for _, want := range []string{`table "t"`, `column "missing"`, "has no field", `db:"missing"`} {
		if !strings.Contains(got, want) {
			t.Errorf("panic message %q does not mention %q", got, want)
		}
	}
}

func TestDefineTable_FieldTypeMismatch_Panics(t *testing.T) {
	type entity struct{ ID string } // column is int
	type model struct {
		orm.Table[entity]
		ID *orm.IntColumn
	}

	got := mustPanic(t, func() {
		orm.DefineTable[entity]("t", func(b *orm.TableBuilder[entity]) *model {
			return &model{Table: b.Table(), ID: b.Int("id")}
		})
	})

	for _, want := range []string{`column "id"`, "int", "string"} {
		if !strings.Contains(got, want) {
			t.Errorf("panic message %q does not mention %q", got, want)
		}
	}
}

// A nullable column needs a pointer field. Catching this at definition
// time is what stops it becoming a scan error on the first query.
func TestDefineTable_NullableColumnNeedsPointerField_Panics(t *testing.T) {
	type entity struct{ Email string } // column is *string
	type model struct {
		orm.Table[entity]
		Email *orm.NullableStringColumn
	}

	got := mustPanic(t, func() {
		orm.DefineTable[entity]("t", func(b *orm.TableBuilder[entity]) *model {
			return &model{Table: b.Table(), Email: b.NullableString("email")}
		})
	})
	if !strings.Contains(got, "*string") {
		t.Errorf("panic message %q does not mention the wanted *string type", got)
	}
}

func TestDefineTable_TableNotTakenFromBuilder_Panics(t *testing.T) {
	type entity struct{ ID int }
	type model struct {
		orm.Table[entity]
		ID *orm.IntColumn
	}

	got := mustPanic(t, func() {
		orm.DefineTable[entity]("t", func(b *orm.TableBuilder[entity]) *model {
			// Table deliberately left unset, the mistake this guards.
			return &model{ID: b.Int("id")}
		})
	})
	if !strings.Contains(got, "t.Table()") {
		t.Errorf("panic message %q does not say to set Table from the builder", got)
	}
}

func TestDefineTable_EntityWithNoFields_Panics(t *testing.T) {
	type model struct {
		orm.Table[orm.NoEntity]
		ID *orm.IntColumn
	}

	got := mustPanic(t, func() {
		orm.DefineTable[orm.NoEntity]("t", func(b *orm.TableBuilder[orm.NoEntity]) *model {
			return &model{Table: b.Table(), ID: b.Int("id")}
		})
	})
	if !strings.Contains(got, "NewTable") {
		t.Errorf("panic message %q does not point at NewTable as the alternative", got)
	}
}

// The snake-case rule is what makes the common case need no tags at all,
// so the shapes it has to get right are worth pinning down. Acronyms are
// the interesting part: AuthorID must not become author_i_d.
func TestDefineTable_SnakeCaseFieldMatching(t *testing.T) {
	type entity struct {
		ID         int
		AuthorID   int
		CreatedAt  time.Time
		HTTPServer string
		URL        string
		Name       string
	}
	type model struct {
		orm.Table[entity]
		ID         *orm.IntColumn
		AuthorID   *orm.IntColumn
		CreatedAt  *orm.TimeColumn
		HTTPServer *orm.StringColumn
		URL        *orm.StringColumn
		Name       *orm.StringColumn
	}

	m := orm.DefineTable[entity]("t", func(b *orm.TableBuilder[entity]) *model {
		return &model{
			Table:      b.Table(),
			ID:         b.Int("id"),
			AuthorID:   b.Int("author_id"),
			CreatedAt:  b.Time("created_at"),
			HTTPServer: b.String("http_server"),
			URL:        b.String("url"),
			Name:       b.String("name"),
		}
	})

	// Reaching here at all means every column found its field; the panic
	// would have fired otherwise. Confirm the table came out whole.
	if got := len(orm.Columns(m)); got != 6 {
		t.Errorf("Columns() returned %d columns, want 6", got)
	}
}

func TestNewTable_NoEntity(t *testing.T) {
	type model struct {
		orm.Table[orm.NoEntity]
		ID *orm.IntColumn
	}
	// NewTable does none of DefineTable's work, so a model with no row type
	// is declared without triggering entity validation at all.
	m := &model{Table: orm.NewTable[orm.NoEntity]("legacy"), ID: orm.NewIntColumn("id")}

	if got := m.TableName(); got != "legacy" {
		t.Errorf("TableName() = %q, want %q", got, "legacy")
	}
	if got := m.ID.OwnerTable(); got != "" {
		t.Errorf("OwnerTable() = %q, want %q: NewTable binds nothing", got, "")
	}
}

func TestTable_ZeroValueTableNameIsEmpty(t *testing.T) {
	// A partially built model reports an empty name rather than panicking,
	// so the problem is diagnosable downstream instead of crashing here.
	var tbl orm.Table[definedEntity]
	if got := tbl.TableName(); got != "" {
		t.Errorf("zero Table.TableName() = %q, want %q", got, "")
	}
}

type allBuilderEntity struct {
	Bool            bool
	NullableBool    *bool
	Int             int
	NullableInt     *int
	Int32           int32
	NullableInt32   *int32
	BigInt          int64
	NullableBigInt  *int64
	Float           float32
	NullableFloat   *float32
	Double          float64
	NullableDouble  *float64
	Decimal         decimal.Decimal
	NullableDecimal *decimal.Decimal
	String          string
	NullableString  *string
	Time            time.Time
	NullableTime    *time.Time
	UUID            uuid.UUID
	NullableUUID    *uuid.UUID
	Status          string
	NullableStatus  *string
	Prefs           map[string]string
	Tags            []string
}

type allBuilderModel struct {
	orm.Table[allBuilderEntity]
	Bool            *orm.BoolColumn
	NullableBool    *orm.NullableBoolColumn
	Int             *orm.IntColumn
	NullableInt     *orm.NullableIntColumn
	Int32           *orm.Int32Column
	NullableInt32   *orm.NullableInt32Column
	BigInt          *orm.BigIntColumn
	NullableBigInt  *orm.NullableBigIntColumn
	Float           *orm.FloatColumn
	NullableFloat   *orm.NullableFloatColumn
	Double          *orm.DoubleColumn
	NullableDouble  *orm.NullableDoubleColumn
	Decimal         *orm.DecimalColumn
	NullableDecimal *orm.NullableDecimalColumn
	String          *orm.StringColumn
	NullableString  *orm.NullableStringColumn
	Time            *orm.TimeColumn
	NullableTime    *orm.NullableTimeColumn
	UUID            *orm.UUIDColumn
	NullableUUID    *orm.NullableUUIDColumn
	Status          *orm.EnumColumn
	NullableStatus  *orm.NullableEnumColumn
	Prefs           *orm.JSONColumn[map[string]string]
	Tags            *orm.ArrayColumn[string]
}

// Every builder method, exercised through a table that resolves cleanly.
// Each column's Go type has to line up with its entity field for this to
// define at all, so it doubles as a check that the builders return the
// column kinds their names promise.
func TestTableBuilder_EveryColumnKind(t *testing.T) {
	m := orm.DefineTable[allBuilderEntity]("all_builders",
		func(b *orm.TableBuilder[allBuilderEntity]) *allBuilderModel {
			return &allBuilderModel{
				Table:           b.Table(),
				Bool:            b.Bool("bool"),
				NullableBool:    b.NullableBool("nullable_bool"),
				Int:             b.Int("int"),
				NullableInt:     b.NullableInt("nullable_int"),
				Int32:           b.Int32("int32"),
				NullableInt32:   b.NullableInt32("nullable_int32"),
				BigInt:          b.BigInt("big_int"),
				NullableBigInt:  b.NullableBigInt("nullable_big_int"),
				Float:           b.Float("float"),
				NullableFloat:   b.NullableFloat("nullable_float"),
				Double:          b.Double("double"),
				NullableDouble:  b.NullableDouble("nullable_double"),
				Decimal:         b.Decimal("decimal"),
				NullableDecimal: b.NullableDecimal("nullable_decimal"),
				String:          b.String("string"),
				NullableString:  b.NullableString("nullable_string"),
				Time:            b.Time("time"),
				NullableTime:    b.NullableTime("nullable_time"),
				UUID:            b.UUID("uuid"),
				NullableUUID:    b.NullableUUID("nullable_uuid"),
				Status:          b.Enum("status", "status_kind", "on", "off"),
				NullableStatus:  b.NullableEnum("nullable_status", "status_kind", "on", "off"),
				// JSON and array columns take a type parameter, so they are
				// built with the package-level constructors rather than a
				// builder method. DefineTable binds them all the same.
				Prefs: orm.NewJSONColumn[map[string]string]("prefs"),
				Tags:  orm.NewArrayColumn[string]("tags"),
			}
		})

	cols := orm.Columns(m)
	if len(cols) != 24 {
		t.Fatalf("Columns() returned %d columns, want 24", len(cols))
	}
	for _, c := range cols {
		if c.OwnerTable() != "all_builders" {
			t.Errorf("column %q: OwnerTable() = %q, want %q", c.Name(), c.OwnerTable(), "all_builders")
		}
	}
}

func TestDefineTable_NonStructEntity_Panics(t *testing.T) {
	type model struct {
		orm.Table[int]
		ID *orm.IntColumn
	}

	got := mustPanic(t, func() {
		orm.DefineTable[int]("t", func(b *orm.TableBuilder[int]) *model {
			return &model{Table: b.Table(), ID: b.Int("id")}
		})
	})
	if !strings.Contains(got, "not a struct") {
		t.Errorf("panic message %q does not say the entity type is not a struct", got)
	}
}

func BenchmarkDefineTable(b *testing.B) {
	type entity struct {
		ID       int
		Username string
	}
	type model struct {
		orm.Table[entity]
		ID       *orm.IntColumn
		Username *orm.StringColumn
	}
	for i := 0; i < b.N; i++ {
		orm.DefineTable[entity]("t", func(t *orm.TableBuilder[entity]) *model {
			return &model{Table: t.Table(), ID: t.Int("id"), Username: t.String("username")}
		})
	}
}

// Embedding a shared base struct in the row type is a standard Go pattern,
// so a column has to find a field promoted from one. The index paths this
// produces are multi element, which is the case a single level walk would
// have got wrong.
type auditFields struct {
	CreatedAt time.Time
	UpdatedAt time.Time
}

type embeddedEntity struct {
	auditFields
	ID    int
	Title string
}

type embeddedModel struct {
	orm.Table[embeddedEntity]
	ID        *orm.IntColumn
	Title     *orm.StringColumn
	CreatedAt *orm.TimeColumn
	UpdatedAt *orm.TimeColumn
}

func TestDefineTable_EmbeddedEntityFields(t *testing.T) {
	m := orm.DefineTable[embeddedEntity]("articles",
		func(b *orm.TableBuilder[embeddedEntity]) *embeddedModel {
			return &embeddedModel{
				Table:     b.Table(),
				ID:        b.Int("id"),
				Title:     b.String("title"),
				CreatedAt: b.Time("created_at"),
				UpdatedAt: b.Time("updated_at"),
			}
		})
	if got := len(orm.Columns(m)); got != 4 {
		t.Errorf("Columns() returned %d columns, want 4", got)
	}
}

// A field declared directly on the entity shadows one promoted from an
// embedded struct, matching Go's own selector rules.
func TestDefineTable_OuterFieldShadowsEmbedded(t *testing.T) {
	type base struct{ Title string }
	type entity struct {
		base
		Title int // shadows base.Title, and the column is an int
	}
	type model struct {
		orm.Table[entity]
		Title *orm.IntColumn
	}

	// Resolving against base.Title (a string) would be a type mismatch, so
	// defining at all proves the outer field won.
	m := orm.DefineTable[entity]("t", func(b *orm.TableBuilder[entity]) *model {
		return &model{Table: b.Table(), Title: b.Int("title")}
	})
	if got := len(orm.Columns(m)); got != 1 {
		t.Errorf("Columns() returned %d columns, want 1", got)
	}
}

// Two embedded structs promoting the same name at the same depth is
// ambiguous in Go, and reporting it beats silently picking one.
func TestDefineTable_AmbiguousEmbeddedField_Panics(t *testing.T) {
	type left struct{ Name string }
	type right struct{ Name string }
	type entity struct {
		left
		right
	}
	type model struct {
		orm.Table[entity]
		Name *orm.StringColumn
	}

	got := mustPanic(t, func() {
		orm.DefineTable[entity]("t", func(b *orm.TableBuilder[entity]) *model {
			return &model{Table: b.Table(), Name: b.String("name")}
		})
	})
	if !strings.Contains(got, "ambiguous") {
		t.Errorf("panic message %q does not report the ambiguity", got)
	}
}

// An ambiguous name no column references is not a problem, exactly as an
// ambiguous selector nobody writes is not a compile error.
func TestDefineTable_UnreferencedAmbiguityIsFine(t *testing.T) {
	type left struct{ Name string }
	type right struct{ Name string }
	type entity struct {
		left
		right
		ID int
	}
	type model struct {
		orm.Table[entity]
		ID *orm.IntColumn
	}

	m := orm.DefineTable[entity]("t", func(b *orm.TableBuilder[entity]) *model {
		return &model{Table: b.Table(), ID: b.Int("id")}
	})
	if got := len(orm.Columns(m)); got != 1 {
		t.Errorf("Columns() returned %d columns, want 1", got)
	}
}

// A struct that embeds its own type transitively must not queue forever.
func TestDefineTable_RecursiveEmbeddingTerminates(t *testing.T) {
	type node struct {
		ID   int
		Next *node
	}
	type model struct {
		orm.Table[node]
		ID *orm.IntColumn
	}

	m := orm.DefineTable[node]("nodes", func(b *orm.TableBuilder[node]) *model {
		return &model{Table: b.Table(), ID: b.Int("id")}
	})
	if got := len(orm.Columns(m)); got != 1 {
		t.Errorf("Columns() returned %d columns, want 1", got)
	}
}
