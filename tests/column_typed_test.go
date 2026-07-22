package orm_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/tork-go/orm"
)

// allKindsModel holds one of every concrete column type. It backs
// TestColumns_DiscoversEveryColumnType, which is the guard against a mixin
// silently shadowing a ColumnMeta accessor: Go answers an ambiguous
// promotion by dropping the method from the method set, so such a column
// would stop satisfying ColumnMeta and walkFields would skip it with no
// error reported anywhere. The `var _ ColumnMeta` assertions beside each
// type catch that at build time; this catches it at test time, from the
// outside, through the same path schema extraction uses.
type jsonDoc struct {
	Theme string `json:"theme"`
}

type allKindsModel struct {
	orm.Table[orm.NoEntity]
	Bool     *orm.BoolColumn
	NBool    *orm.NullableBoolColumn
	Int      *orm.IntColumn
	NInt     *orm.NullableIntColumn
	Int32    *orm.Int32Column
	NInt32   *orm.NullableInt32Column
	BigInt   *orm.BigIntColumn
	NBigInt  *orm.NullableBigIntColumn
	Float    *orm.FloatColumn
	NFloat   *orm.NullableFloatColumn
	Double   *orm.DoubleColumn
	NDouble  *orm.NullableDoubleColumn
	Decimal  *orm.DecimalColumn
	NDecimal *orm.NullableDecimalColumn
	String   *orm.StringColumn
	NString  *orm.NullableStringColumn
	Time     *orm.TimeColumn
	NTime    *orm.NullableTimeColumn
	UUID     *orm.UUIDColumn
	NUUID    *orm.NullableUUIDColumn
	Enum     *orm.EnumColumn
	NEnum    *orm.NullableEnumColumn
	JSON     *orm.JSONColumn[jsonDoc]
	NJSON    *orm.NullableJSONColumn[jsonDoc]
	Array    *orm.StringArrayColumn
	NArray   *orm.NullableStringArrayColumn
}

func newAllKindsModel() *allKindsModel {
	return &allKindsModel{
		Table:    orm.NewTable[orm.NoEntity]("all_kinds"),
		Bool:     orm.NewBoolColumn("bool"),
		NBool:    orm.NewNullableBoolColumn("n_bool"),
		Int:      orm.NewIntColumn("int"),
		NInt:     orm.NewNullableIntColumn("n_int"),
		Int32:    orm.NewInt32Column("int32"),
		NInt32:   orm.NewNullableInt32Column("n_int32"),
		BigInt:   orm.NewBigIntColumn("big_int"),
		NBigInt:  orm.NewNullableBigIntColumn("n_big_int"),
		Float:    orm.NewFloatColumn("float"),
		NFloat:   orm.NewNullableFloatColumn("n_float"),
		Double:   orm.NewDoubleColumn("double"),
		NDouble:  orm.NewNullableDoubleColumn("n_double"),
		Decimal:  orm.NewDecimalColumn("decimal"),
		NDecimal: orm.NewNullableDecimalColumn("n_decimal"),
		String:   orm.NewStringColumn("string"),
		NString:  orm.NewNullableStringColumn("n_string"),
		Time:     orm.NewTimeColumn("time"),
		NTime:    orm.NewNullableTimeColumn("n_time"),
		UUID:     orm.NewUUIDColumn("uuid"),
		NUUID:    orm.NewNullableUUIDColumn("n_uuid"),
		Enum:     orm.NewEnumColumn("enum", "status", "on", "off"),
		NEnum:    orm.NewNullableEnumColumn("n_enum", "status", "on", "off"),
		JSON:     orm.NewJSONColumn[jsonDoc]("json"),
		NJSON:    orm.NewNullableJSONColumn[jsonDoc]("n_json"),
		Array:    orm.NewStringArrayColumn("array"),
		NArray:   orm.NewNullableStringArrayColumn("n_array"),
	}
}

func TestColumns_DiscoversEveryColumnType(t *testing.T) {
	m := newAllKindsModel()
	got := orm.Columns(m)

	want := []string{
		"bool", "n_bool", "int", "n_int", "int32", "n_int32",
		"big_int", "n_big_int", "float", "n_float", "double", "n_double",
		"decimal", "n_decimal", "string", "n_string", "time", "n_time",
		"uuid", "n_uuid", "enum", "n_enum", "json", "n_json",
		"array", "n_array",
	}
	if len(got) != len(want) {
		t.Fatalf("Columns() returned %d columns, want %d; a column type "+
			"missing here has lost its ColumnMeta conformance", len(got), len(want))
	}
	for i, c := range got {
		if c.Name() != want[i] {
			t.Errorf("Columns()[%d].Name() = %q, want %q", i, c.Name(), want[i])
		}
	}
}

func TestColumns_GoTypeAndNullability(t *testing.T) {
	m := newAllKindsModel()

	tests := []struct {
		name     string
		col      orm.ColumnMeta
		goType   reflect.Type
		nullable bool
	}{
		{"bool", m.Bool, reflect.TypeFor[bool](), false},
		{"nullable bool", m.NBool, reflect.TypeFor[*bool](), true},
		{"int", m.Int, reflect.TypeFor[int](), false},
		{"nullable int", m.NInt, reflect.TypeFor[*int](), true},
		{"int32", m.Int32, reflect.TypeFor[int32](), false},
		{"nullable int32", m.NInt32, reflect.TypeFor[*int32](), true},
		{"bigint", m.BigInt, reflect.TypeFor[int64](), false},
		{"nullable bigint", m.NBigInt, reflect.TypeFor[*int64](), true},
		{"float", m.Float, reflect.TypeFor[float32](), false},
		{"nullable float", m.NFloat, reflect.TypeFor[*float32](), true},
		{"double", m.Double, reflect.TypeFor[float64](), false},
		{"nullable double", m.NDouble, reflect.TypeFor[*float64](), true},
		{"decimal", m.Decimal, reflect.TypeFor[decimal.Decimal](), false},
		{"nullable decimal", m.NDecimal, reflect.TypeFor[*decimal.Decimal](), true},
		{"string", m.String, reflect.TypeFor[string](), false},
		{"nullable string", m.NString, reflect.TypeFor[*string](), true},
		{"time", m.Time, reflect.TypeFor[time.Time](), false},
		{"nullable time", m.NTime, reflect.TypeFor[*time.Time](), true},
		{"uuid", m.UUID, reflect.TypeFor[uuid.UUID](), false},
		{"nullable uuid", m.NUUID, reflect.TypeFor[*uuid.UUID](), true},
		{"enum", m.Enum, reflect.TypeFor[string](), false},
		{"nullable enum", m.NEnum, reflect.TypeFor[*string](), true},
		{"json", m.JSON, reflect.TypeFor[jsonDoc](), false},
		{"nullable json", m.NJSON, reflect.TypeFor[*jsonDoc](), true},
		{"array", m.Array, reflect.TypeFor[[]string](), false},
		{"nullable array", m.NArray, reflect.TypeFor[*[]string](), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.col.GoType(); got != tt.goType {
				t.Errorf("GoType() = %v, want %v", got, tt.goType)
			}
			if got := tt.col.IsNullable(); got != tt.nullable {
				t.Errorf("IsNullable() = %v, want %v", got, tt.nullable)
			}
		})
	}
}

// The declarations below only compile if every builder returns the
// concrete column type rather than *Column[T]. That is what chain and the
// builder mixins exist to guarantee, so a covariance regression is a build
// failure here rather than a subtle downcast at a call site.
var (
	_ *orm.StringColumn         = orm.NewStringColumn("s").PrimaryKey().Unique().NotNull().Index().MaxLen(30)
	_ *orm.StringColumn         = orm.NewStringColumn("s").ServerDefault("''")
	_ *orm.EnumColumn           = orm.NewEnumColumn("e", "t", "a", "b").NotNull().Index()
	_ *orm.StringArrayColumn    = orm.NewStringArrayColumn("a").NotNull().MaxLen(20)
	_ *orm.JSONColumn[jsonDoc]  = orm.NewJSONColumn[jsonDoc]("j").NotNull().JSON()
	_ *orm.IntColumn            = orm.NewIntColumn("i").PrimaryKey().NotNull().Index()
	_ *orm.DecimalColumn        = orm.NewDecimalColumn("d").Numeric(10, 2).NotNull()
	_ *orm.NullableStringColumn = orm.NewNullableStringColumn("e").MaxLen(255).Index()
	_ *orm.UUIDColumn           = orm.NewUUIDColumn("u").PrimaryKey().GeneratedByClient(uuid.New)
	// A foreign key is a column with a reference, so the chain must survive
	// References and the referential-action builders too.
	_ *orm.IntColumn = orm.NewIntColumn("fk").NotNull().Index().
		ReferencesTable("t", "id").OnDelete(orm.ActionCascade)
)

func TestChainedBuilders_ApplyToUnderlyingColumn(t *testing.T) {
	c := orm.NewStringColumn("username").Unique().NotNull().MaxLen(30).Index()

	if !c.IsUnique() || !c.HasNotNull() || !c.IsIndexed() {
		t.Errorf("IsUnique()=%v HasNotNull()=%v IsIndexed()=%v, want all true",
			c.IsUnique(), c.HasNotNull(), c.IsIndexed())
	}
	n, ok := c.MaxLength()
	if !ok || n != 30 {
		t.Errorf("MaxLength() = (%d, %v), want (30, true)", n, ok)
	}
	if c.Name() != "username" {
		t.Errorf("Name() = %q, want %q", c.Name(), "username")
	}
}

func TestComparisonOps(t *testing.T) {
	id := orm.NewIntColumn("id")

	tests := []struct {
		name string
		pred orm.Predicate
		op   orm.Operator
		val  any
	}{
		{"Equals", id.Equals(1), orm.OpEquals, 1},
		{"NotEquals", id.NotEquals(2), orm.OpNotEquals, 2},
		{"GreaterThan", id.GreaterThan(3), orm.OpGreaterThan, 3},
		{"GreaterOrEqual", id.GreaterOrEqual(4), orm.OpGreaterOrEqual, 4},
		{"LessThan", id.LessThan(5), orm.OpLessThan, 5},
		{"LessOrEqual", id.LessOrEqual(6), orm.OpLessOrEqual, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, ok := tt.pred.(orm.Comparison)
			if !ok {
				t.Fatalf("got %T, want orm.Comparison", tt.pred)
			}
			if c.Op != tt.op {
				t.Errorf("Op = %v, want %v", c.Op, tt.op)
			}
			if c.Value != tt.val {
				t.Errorf("Value = %v, want %v", c.Value, tt.val)
			}
			if c.Col.Name() != "id" {
				t.Errorf("Col.Name() = %q, want %q", c.Col.Name(), "id")
			}
		})
	}
}

func TestInList(t *testing.T) {
	id := orm.NewIntColumn("id")

	in, ok := id.In(1, 2, 3).(orm.InList)
	if !ok {
		t.Fatalf("In() returned %T, want orm.InList", id.In(1))
	}
	if in.Not {
		t.Error("In().Not = true, want false")
	}
	if !reflect.DeepEqual(in.Values, []any{1, 2, 3}) {
		t.Errorf("In().Values = %v, want [1 2 3]", in.Values)
	}

	notIn, ok := id.NotIn(4).(orm.InList)
	if !ok {
		t.Fatalf("NotIn() returned %T, want orm.InList", id.NotIn(4))
	}
	if !notIn.Not {
		t.Error("NotIn().Not = false, want true")
	}
}

func TestBetween(t *testing.T) {
	r, ok := orm.NewIntColumn("age").Between(18, 65).(orm.Range)
	if !ok {
		t.Fatalf("Between() returned %T, want orm.Range", r)
	}
	if r.Lo != 18 || r.Hi != 65 {
		t.Errorf("Between(18, 65) = (%v, %v), want (18, 65)", r.Lo, r.Hi)
	}
}

// Contains, StartsWith, and EndsWith must escape the LIKE wildcards so a
// caller's substring matches literally. Getting this wrong turns
// Contains("50%") into a prefix match against everything starting "50",
// which is a silent wrong-results bug rather than an error.
func TestPatternOps_EscapeWildcards(t *testing.T) {
	name := orm.NewStringColumn("name")

	tests := []struct {
		name string
		pred orm.Predicate
		want string
		ci   bool
	}{
		{"Contains plain", name.Contains("ali"), `%ali%`, false},
		{"StartsWith plain", name.StartsWith("a"), `a%`, false},
		{"EndsWith plain", name.EndsWith("z"), `%z`, false},
		{"Contains escapes percent", name.Contains("50%"), `%50\%%`, false},
		{"Contains escapes underscore", name.Contains("a_b"), `%a\_b%`, false},
		{"Contains escapes backslash", name.Contains(`a\b`), `%a\\b%`, false},
		{"StartsWith escapes percent", name.StartsWith("100%"), `100\%%`, false},
		{"EndsWith escapes underscore", name.EndsWith("_x"), `%\_x`, false},
		{"Like passes pattern through", name.Like("a%b_c"), `a%b_c`, false},
		{"ILike passes pattern through", name.ILike("A%"), `A%`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := tt.pred.(orm.Pattern)
			if !ok {
				t.Fatalf("got %T, want orm.Pattern", tt.pred)
			}
			if p.Value != tt.want {
				t.Errorf("Value = %q, want %q", p.Value, tt.want)
			}
			if p.CaseInsensitive != tt.ci {
				t.Errorf("CaseInsensitive = %v, want %v", p.CaseInsensitive, tt.ci)
			}
		})
	}
}

func TestNullableOps_TakeUnderlyingType(t *testing.T) {
	email := orm.NewNullableStringColumn("email")

	// The point of nullEquatable: a plain string, not a *string.
	c, ok := email.Equals("alice@example.com").(orm.Comparison)
	if !ok {
		t.Fatalf("Equals() returned %T, want orm.Comparison", email.Equals("x"))
	}
	if c.Value != "alice@example.com" {
		t.Errorf("Equals().Value = %v, want %q", c.Value, "alice@example.com")
	}

	n, ok := email.IsNull().(orm.Nullness)
	if !ok || n.Not {
		t.Errorf("IsNull() = %#v, want Nullness{Not: false}", email.IsNull())
	}
	n, ok = email.IsNotNull().(orm.Nullness)
	if !ok || !n.Not {
		t.Errorf("IsNotNull() = %#v, want Nullness{Not: true}", email.IsNotNull())
	}
}

// The nullable mixins duplicate the plain ones rather than reusing them,
// because their argument type differs (T, not *T). That duplication is
// exactly where a copy-paste slip would hide, so each one is exercised.
func TestNullableOps_FullSurface(t *testing.T) {
	age := orm.NewNullableIntColumn("age")

	tests := []struct {
		name string
		pred orm.Predicate
		op   orm.Operator
		val  any
	}{
		{"NotEquals", age.NotEquals(1), orm.OpNotEquals, 1},
		{"GreaterThan", age.GreaterThan(2), orm.OpGreaterThan, 2},
		{"GreaterOrEqual", age.GreaterOrEqual(3), orm.OpGreaterOrEqual, 3},
		{"LessThan", age.LessThan(4), orm.OpLessThan, 4},
		{"LessOrEqual", age.LessOrEqual(5), orm.OpLessOrEqual, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, ok := tt.pred.(orm.Comparison)
			if !ok {
				t.Fatalf("got %T, want orm.Comparison", tt.pred)
			}
			if c.Op != tt.op || c.Value != tt.val {
				t.Errorf("got op %v value %v, want op %v value %v",
					c.Op, c.Value, tt.op, tt.val)
			}
		})
	}

	t.Run("Between", func(t *testing.T) {
		r, ok := age.Between(18, 65).(orm.Range)
		if !ok {
			t.Fatalf("got %T, want orm.Range", age.Between(18, 65))
		}
		if r.Lo != 18 || r.Hi != 65 {
			t.Errorf("Between(18, 65) = (%v, %v), want (18, 65)", r.Lo, r.Hi)
		}
	})

	t.Run("In", func(t *testing.T) {
		in, ok := age.In(1, 2).(orm.InList)
		if !ok {
			t.Fatalf("got %T, want orm.InList", age.In(1, 2))
		}
		if in.Not || !reflect.DeepEqual(in.Values, []any{1, 2}) {
			t.Errorf("In(1, 2) = %+v, want values [1 2] and Not false", in)
		}
	})

	t.Run("NotIn", func(t *testing.T) {
		in, ok := age.NotIn(3).(orm.InList)
		if !ok {
			t.Fatalf("got %T, want orm.InList", age.NotIn(3))
		}
		if !in.Not || !reflect.DeepEqual(in.Values, []any{3}) {
			t.Errorf("NotIn(3) = %+v, want values [3] and Not true", in)
		}
	})
}

func TestEqPtr(t *testing.T) {
	email := orm.NewNullableStringColumn("email")

	v := "alice@example.com"
	c, ok := email.EqualsPtr(&v).(orm.Comparison)
	if !ok {
		t.Fatalf("EqualsPtr(&v) returned %T, want orm.Comparison", email.EqualsPtr(&v))
	}
	if c.Value != v {
		t.Errorf("EqualsPtr(&v).Value = %v, want %q (the pointee, not the pointer)", c.Value, v)
	}

	// A nil pointer must become IS NULL. Compiling it as `col = NULL`
	// would be valid SQL that silently matches no rows.
	if _, ok := email.EqualsPtr(nil).(orm.Nullness); !ok {
		t.Errorf("EqualsPtr(nil) = %T, want orm.Nullness", email.EqualsPtr(nil))
	}
}

func TestAssignments(t *testing.T) {
	email := orm.NewNullableStringColumn("email")

	if a := email.Set("new@example.com"); a.Value != "new@example.com" {
		t.Errorf("Set().Value = %v, want %q", a.Value, "new@example.com")
	}
	if a := email.SetNull(); a.Value != nil {
		t.Errorf("SetNull().Value = %v, want nil", a.Value)
	}
	if a := email.SetPtr(nil); a.Value != nil {
		t.Errorf("SetPtr(nil).Value = %v, want nil", a.Value)
	}
	v := "x"
	if a := email.SetPtr(&v); a.Value != "x" {
		t.Errorf("SetPtr(&v).Value = %v, want %q", a.Value, "x")
	}
	if a := orm.NewIntColumn("n").Set(7); a.Value != 7 || a.Col.Name() != "n" {
		t.Errorf("Set(7) = %+v, want value 7 on column n", a)
	}
}

func TestOrdering(t *testing.T) {
	id := orm.NewIntColumn("id")

	if o := id.Asc(); o.Desc || o.Col.Name() != "id" {
		t.Errorf("Asc() = %+v, want ascending on id", o)
	}
	if o := id.Desc(); !o.Desc {
		t.Errorf("Desc().Desc = false, want true")
	}
}

func TestConjunctions(t *testing.T) {
	id := orm.NewIntColumn("id")
	a, b := id.Equals(1), id.Equals(2)

	g, ok := orm.Or(a, b).(orm.Group)
	if !ok {
		t.Fatalf("Or() returned %T, want orm.Group", orm.Or(a, b))
	}
	if g.Conj != orm.ConjOr || len(g.Preds) != 2 {
		t.Errorf("Or() = %+v, want ConjOr with 2 predicates", g)
	}

	g, ok = orm.And(a, b).(orm.Group)
	if !ok || g.Conj != orm.ConjAnd {
		t.Errorf("And() = %#v, want a Group with ConjAnd", orm.And(a, b))
	}

	// A lone predicate is returned unwrapped, so the common case produces
	// no redundant parentheses.
	if got := orm.And(a); got != orm.Predicate(a) {
		t.Errorf("And(a) = %#v, want the predicate itself", got)
	}
	if got := orm.Or(a); got != orm.Predicate(a) {
		t.Errorf("Or(a) = %#v, want the predicate itself", got)
	}

	// Zero predicates still produce a well-defined group rather than nil.
	if g, ok := orm.And().(orm.Group); !ok || len(g.Preds) != 0 {
		t.Errorf("And() with no predicates = %#v, want an empty Group", orm.And())
	}

	if n, ok := orm.Not(a).(orm.Negation); !ok || n.Pred != orm.Predicate(a) {
		t.Errorf("Not(a) = %#v, want Negation wrapping a", orm.Not(a))
	}
}

func TestOperator_String(t *testing.T) {
	tests := []struct {
		op   orm.Operator
		want string
	}{
		{orm.OpEquals, "="},
		{orm.OpNotEquals, "<>"},
		{orm.OpGreaterThan, ">"},
		{orm.OpGreaterOrEqual, ">="},
		{orm.OpLessThan, "<"},
		{orm.OpLessOrEqual, "<="},
		// An operator outside the declared set has no SQL spelling. It
		// degrades to a placeholder rather than panicking, so a malformed
		// predicate surfaces as visibly wrong SQL at compile time rather
		// than taking down the caller.
		{orm.Operator(99), "?"},
	}
	for _, tt := range tests {
		if got := tt.op.String(); got != tt.want {
			t.Errorf("Operator(%d).String() = %q, want %q", tt.op, got, tt.want)
		}
	}
}

// The composed query reads the way a caller would write it, and exercises
// the mixins together rather than one at a time.
func TestComposedPredicate(t *testing.T) {
	m := newAllKindsModel()

	p := orm.And(
		m.Int.GreaterThan(100),
		m.String.StartsWith("a"),
		orm.Or(
			m.String.Contains("alice"),
			m.NString.Equals("alice@example.com"),
			m.NString.IsNull(),
		),
	)

	g, ok := p.(orm.Group)
	if !ok {
		t.Fatalf("got %T, want orm.Group", p)
	}
	if g.Conj != orm.ConjAnd || len(g.Preds) != 3 {
		t.Fatalf("outer group = %+v, want ConjAnd with 3 predicates", g)
	}
	inner, ok := g.Preds[2].(orm.Group)
	if !ok || inner.Conj != orm.ConjOr || len(inner.Preds) != 3 {
		t.Errorf("inner group = %#v, want ConjOr with 3 predicates", g.Preds[2])
	}
}

func BenchmarkColumnConstruction(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = orm.NewStringColumn("username").Unique().NotNull().MaxLen(30)
	}
}

func BenchmarkPredicateConstruction(b *testing.B) {
	c := orm.NewStringColumn("username")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = orm.Or(c.Equals("alice"), c.Contains("bob"), c.StartsWith("c"))
	}
}

// The JSON storage builders and the accessors that read them back. These
// were previously exercised only through ForeignKey[T], which carried
// every builder; with foreign keys folded into ordinary columns, JSON
// columns are the only type that has them.
func TestJSONColumn_StorageBuilders(t *testing.T) {
	jsonb := orm.NewJSONColumn[jsonDoc]("prefs")
	if !jsonb.IsJSONB() || jsonb.IsJSON() {
		t.Errorf("IsJSONB()=%v IsJSON()=%v, want true/false: JSONB is the default",
			jsonb.IsJSONB(), jsonb.IsJSON())
	}

	plain := orm.NewJSONColumn[jsonDoc]("raw").JSON()
	if !plain.IsJSON() || plain.IsJSONB() {
		t.Errorf("IsJSON()=%v IsJSONB()=%v, want true/false after JSON()",
			plain.IsJSON(), plain.IsJSONB())
	}

	back := orm.NewJSONColumn[jsonDoc]("back").JSON().JSONB()
	if !back.IsJSONB() {
		t.Error("JSONB() after JSON() did not switch back")
	}
}

func TestJSONColumn_Serialize(t *testing.T) {
	marshal := func(jsonDoc) ([]byte, error) { return []byte(`{}`), nil }
	unmarshal := func([]byte) (jsonDoc, error) { return jsonDoc{}, nil }

	c := orm.NewJSONColumn[jsonDoc]("prefs").Serialize(marshal, unmarshal)
	if !c.IsSerialized() {
		t.Error("IsSerialized() = false after Serialize")
	}
	m, u, ok := c.Serializer()
	if !ok || m == nil || u == nil {
		t.Errorf("Serializer() = (%v, %v, %v), want both functions and true", m != nil, u != nil, ok)
	}

	if orm.NewJSONColumn[jsonDoc]("plain").IsSerialized() {
		t.Error("IsSerialized() = true on a column that never called Serialize")
	}
}

// EnumColumn re-declares Enum so a caller can restate or widen the value
// set on an already-constructed column.
func TestEnumColumn_EnumBuilder(t *testing.T) {
	c := orm.NewEnumColumn("status", "order_status", "pending").
		Enum("order_status", "pending", "shipped", "done")

	name, values, ok := c.EnumSpec()
	if !ok || name != "order_status" {
		t.Fatalf("EnumSpec() = (%q, %v, %v), want order_status", name, values, ok)
	}
	if len(values) != 3 {
		t.Errorf("EnumSpec() values = %v, want 3 values", values)
	}
}

func TestColumn_ClientGenerator(t *testing.T) {
	c := orm.NewUUIDColumn("id").GeneratedByClient(uuid.New)
	if !c.IsClientGenerated() {
		t.Error("IsClientGenerated() = false after GeneratedByClient")
	}
	gen, ok := c.Generator()
	if !ok || gen == nil {
		t.Fatalf("Generator() = (%v, %v), want a function and true", gen != nil, ok)
	}
	if gen() == uuid.Nil {
		t.Error("Generator() returned a function producing the nil UUID")
	}

	if orm.NewUUIDColumn("plain").IsClientGenerated() {
		t.Error("IsClientGenerated() = true on a column that never called GeneratedByClient")
	}
}

// Base is what makes a typed column usable as a foreign key target, and
// what lets it reach the untyped Column[T] when something needs one.
func TestBase_UnwrapsToUnderlyingColumn(t *testing.T) {
	c := orm.NewIntColumn("id").NotNull()
	base := c.Base()
	if base.Name() != "id" || !base.HasNotNull() {
		t.Errorf("Base() = %+v, want the same column state", base)
	}
	// Column[T].Base returns itself, so either can be a reference target.
	plain := orm.NewColumn[int]("id")
	if plain.Base() != plain {
		t.Error("Column[T].Base() did not return the column itself")
	}
}

// A Model that is not a struct has no fields to walk. It reports none
// rather than panicking, so an odd model type is inert instead of fatal.
type notAStructModel string

func (notAStructModel) TableName() string { return "not_a_struct" }

func TestColumns_NonStructModel(t *testing.T) {
	if got := orm.Columns(notAStructModel("x")); got != nil {
		t.Errorf("Columns() = %v, want nil for a non-struct model", got)
	}
	if got := orm.ForeignKeys(notAStructModel("x")); len(got) != 0 {
		t.Errorf("ForeignKeys() = %v, want none for a non-struct model", got)
	}
}

// A model passed by value walks the same as one passed by pointer, and
// unexported fields are skipped rather than inspected.
type valueModel struct {
	orm.Table[orm.NoEntity]
	ID     *orm.IntColumn
	hidden *orm.IntColumn
}

func TestColumns_ValueModelAndUnexportedFields(t *testing.T) {
	m := valueModel{
		Table:  orm.NewTable[orm.NoEntity]("value_model"),
		ID:     orm.NewIntColumn("id"),
		hidden: orm.NewIntColumn("hidden"),
	}

	got := orm.Columns(m) // by value, not by pointer
	if len(got) != 1 {
		t.Fatalf("Columns() returned %d columns, want 1: the unexported field must be skipped", len(got))
	}
	if got[0].Name() != "id" {
		t.Errorf("Columns()[0].Name() = %q, want %q", got[0].Name(), "id")
	}
	_ = m.hidden
}
