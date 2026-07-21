package schema_test

import (
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/schema"
)

type prefsDoc struct {
	Theme string `json:"theme"`
}

// typedKindsModel exercises every concrete column type through schema
// extraction. The typed columns are wrappers, so what matters is that each
// one still resolves to the schema kind its name promises: FloatColumn to
// KindFloat rather than KindDouble, EnumColumn to KindEnum, and so on.
// A wrapper that lost or mangled its underlying column would show up here
// as a wrong kind rather than as a compile error.
type typedKindsModel struct {
	orm.Table[orm.NoEntity]
	Bool        *orm.BoolColumn
	Int         *orm.IntColumn
	Int32       *orm.Int32Column
	BigInt      *orm.BigIntColumn
	Float       *orm.FloatColumn
	Double      *orm.DoubleColumn
	Decimal     *orm.DecimalColumn
	DecimalPS   *orm.DecimalColumn
	Text        *orm.StringColumn
	Varchar     *orm.StringColumn
	Time        *orm.TimeColumn
	UUID        *orm.UUIDColumn
	Enum        *orm.EnumColumn
	JSONB       *orm.JSONColumn[prefsDoc]
	JSON        *orm.JSONColumn[prefsDoc]
	TextArray   *orm.StringArrayColumn
	VarcharArr  *orm.StringArrayColumn
	IntArray    *orm.IntArrayColumn
	NullableInt *orm.NullableIntColumn
}

func newTypedKindsModel() *typedKindsModel {
	return &typedKindsModel{
		Table:       orm.NewTable[orm.NoEntity]("typed_kinds"),
		Bool:        orm.NewBoolColumn("bool"),
		Int:         orm.NewIntColumn("int"),
		Int32:       orm.NewInt32Column("int32"),
		BigInt:      orm.NewBigIntColumn("big_int"),
		Float:       orm.NewFloatColumn("float"),
		Double:      orm.NewDoubleColumn("double"),
		Decimal:     orm.NewDecimalColumn("decimal"),
		DecimalPS:   orm.NewDecimalColumn("decimal_ps").Numeric(10, 2),
		Text:        orm.NewStringColumn("text"),
		Varchar:     orm.NewStringColumn("varchar").MaxLen(30),
		Time:        orm.NewTimeColumn("time"),
		UUID:        orm.NewUUIDColumn("uuid"),
		Enum:        orm.NewEnumColumn("enum", "order_status", "pending", "done"),
		JSONB:       orm.NewJSONColumn[prefsDoc]("jsonb"),
		JSON:        orm.NewJSONColumn[prefsDoc]("json").JSON(),
		TextArray:   orm.NewStringArrayColumn("text_array"),
		VarcharArr:  orm.NewStringArrayColumn("varchar_arr").MaxLen(20),
		IntArray:    orm.NewIntArrayColumn("int_array"),
		NullableInt: orm.NewNullableIntColumn("nullable_int"),
	}
}

func TestExtractSchema_TypedColumnKinds(t *testing.T) {
	s, err := schema.ExtractSchema(newTypedKindsModel())
	if err != nil {
		t.Fatalf("ExtractSchema() error = %v", err)
	}
	if len(s.Tables) != 1 {
		t.Fatalf("ExtractSchema() returned %d tables, want 1", len(s.Tables))
	}

	byName := make(map[string]schema.Column, len(s.Tables[0].Columns))
	for _, c := range s.Tables[0].Columns {
		byName[c.Name] = c
	}

	text := schema.ColumnType{Kind: schema.KindText}
	varchar20 := schema.ColumnType{Kind: schema.KindVarchar, Length: 20}
	integer := schema.ColumnType{Kind: schema.KindInteger}

	tests := []struct {
		column string
		want   schema.ColumnType
	}{
		{"bool", schema.ColumnType{Kind: schema.KindBoolean}},
		{"int", schema.ColumnType{Kind: schema.KindInteger}},
		{"int32", schema.ColumnType{Kind: schema.KindInteger}},
		{"big_int", schema.ColumnType{Kind: schema.KindBigInteger}},
		// The reason Float and Double are separate types: float32 is REAL,
		// float64 is DOUBLE PRECISION, and one name cannot mean both.
		{"float", schema.ColumnType{Kind: schema.KindFloat}},
		{"double", schema.ColumnType{Kind: schema.KindDouble}},
		{"decimal", schema.ColumnType{Kind: schema.KindNumeric}},
		{"decimal_ps", schema.ColumnType{Kind: schema.KindNumeric, Precision: 10, Scale: 2}},
		{"text", schema.ColumnType{Kind: schema.KindText}},
		{"varchar", schema.ColumnType{Kind: schema.KindVarchar, Length: 30}},
		{"time", schema.ColumnType{Kind: schema.KindTimestamp}},
		{"uuid", schema.ColumnType{Kind: schema.KindUUID}},
		{"enum", schema.ColumnType{Kind: schema.KindEnum, TypeName: "order_status"}},
		// JSONColumn is JSONB unless JSON() says otherwise.
		{"jsonb", schema.ColumnType{Kind: schema.KindJSONB}},
		{"json", schema.ColumnType{Kind: schema.KindJSON}},
		{"text_array", schema.ColumnType{Kind: schema.KindArray, Elem: &text}},
		// MaxLen on an array applies to its element.
		{"varchar_arr", schema.ColumnType{Kind: schema.KindArray, Elem: &varchar20}},
		{"int_array", schema.ColumnType{Kind: schema.KindArray, Elem: &integer}},
		{"nullable_int", schema.ColumnType{Kind: schema.KindInteger}},
	}

	for _, tt := range tests {
		t.Run(tt.column, func(t *testing.T) {
			got, ok := byName[tt.column]
			if !ok {
				t.Fatalf("column %q missing from extracted schema", tt.column)
			}
			if !got.Type.Equal(tt.want) {
				t.Errorf("Type = %+v, want %+v", got.Type, tt.want)
			}
		})
	}
}

// A non-pointer column type is NOT NULL without anyone saying so; a
// nullable one is not. That distinction is carried entirely by whether the
// wrapper's underlying T is a pointer, so it is worth asserting through
// the wrappers rather than only through Column[T].
func TestExtractSchema_TypedColumnNullability(t *testing.T) {
	s, err := schema.ExtractSchema(newTypedKindsModel())
	if err != nil {
		t.Fatalf("ExtractSchema() error = %v", err)
	}

	byName := make(map[string]schema.Column, len(s.Tables[0].Columns))
	for _, c := range s.Tables[0].Columns {
		byName[c.Name] = c
	}

	if !byName["int"].NotNull {
		t.Error("int: NotNull = false, want true on a non-nullable column type")
	}
	if byName["nullable_int"].NotNull {
		t.Error("nullable_int: NotNull = true, want false on a nullable column type")
	}
}

// An EnumColumn must register its type schema-wide, not just name it on
// the column, or no CREATE TYPE would ever be generated.
func TestExtractSchema_TypedEnumRegistersType(t *testing.T) {
	s, err := schema.ExtractSchema(newTypedKindsModel())
	if err != nil {
		t.Fatalf("ExtractSchema() error = %v", err)
	}

	if len(s.EnumTypes) != 1 {
		t.Fatalf("ExtractSchema() returned %d enum types, want 1", len(s.EnumTypes))
	}
	got := s.EnumTypes[0]
	if got.Name != "order_status" {
		t.Errorf("EnumTypes[0].Name = %q, want %q", got.Name, "order_status")
	}
	want := []string{"pending", "done"}
	if len(got.Values) != len(want) {
		t.Fatalf("EnumTypes[0].Values = %v, want %v", got.Values, want)
	}
	for i, v := range want {
		if got.Values[i] != v {
			t.Errorf("EnumTypes[0].Values[%d] = %q, want %q", i, got.Values[i], v)
		}
	}
}

// Each array kind is its own type, so the element builders land only where
// they mean something. These check the element kind reaches the schema.
type arrayKindsModel struct {
	orm.Table[orm.NoEntity]
	Bools    *orm.BoolArrayColumn
	Ints     *orm.IntArrayColumn
	BigInts  *orm.BigIntArrayColumn
	Doubles  *orm.DoubleArrayColumn
	Strings  *orm.StringArrayColumn
	Varchars *orm.StringArrayColumn
	Decimals *orm.DecimalArrayColumn
	Times    *orm.TimeArrayColumn
	UUIDs    *orm.UUIDArrayColumn
	Nullable *orm.NullableStringArrayColumn
}

func TestExtractSchema_ArrayColumnKinds(t *testing.T) {
	m := &arrayKindsModel{
		Table:    orm.NewTable[orm.NoEntity]("array_kinds"),
		Bools:    orm.NewBoolArrayColumn("bools"),
		Ints:     orm.NewIntArrayColumn("ints"),
		BigInts:  orm.NewBigIntArrayColumn("big_ints"),
		Doubles:  orm.NewDoubleArrayColumn("doubles"),
		Strings:  orm.NewStringArrayColumn("strings"),
		Varchars: orm.NewStringArrayColumn("varchars").MaxLen(20),
		Decimals: orm.NewDecimalArrayColumn("decimals").Numeric(10, 2),
		Times:    orm.NewTimeArrayColumn("times"),
		UUIDs:    orm.NewUUIDArrayColumn("uuids"),
		Nullable: orm.NewNullableStringArrayColumn("nullable"),
	}

	s, err := schema.ExtractSchema(m)
	if err != nil {
		t.Fatalf("ExtractSchema() error = %v", err)
	}
	byName := map[string]schema.Column{}
	for _, c := range s.Tables[0].Columns {
		byName[c.Name] = c
	}

	tests := []struct {
		column string
		elem   schema.ColumnType
	}{
		{"bools", schema.ColumnType{Kind: schema.KindBoolean}},
		{"ints", schema.ColumnType{Kind: schema.KindInteger}},
		{"big_ints", schema.ColumnType{Kind: schema.KindBigInteger}},
		{"doubles", schema.ColumnType{Kind: schema.KindDouble}},
		{"strings", schema.ColumnType{Kind: schema.KindText}},
		// MaxLen and Numeric size the element, not the array.
		{"varchars", schema.ColumnType{Kind: schema.KindVarchar, Length: 20}},
		{"decimals", schema.ColumnType{Kind: schema.KindNumeric, Precision: 10, Scale: 2}},
		{"times", schema.ColumnType{Kind: schema.KindTimestamp}},
		{"uuids", schema.ColumnType{Kind: schema.KindUUID}},
		{"nullable", schema.ColumnType{Kind: schema.KindText}},
	}
	for _, tt := range tests {
		t.Run(tt.column, func(t *testing.T) {
			got, ok := byName[tt.column]
			if !ok {
				t.Fatalf("column %q missing", tt.column)
			}
			if got.Type.Kind != schema.KindArray {
				t.Fatalf("Kind = %v, want KindArray", got.Type.Kind)
			}
			if got.Type.Elem == nil || !got.Type.Elem.Equal(tt.elem) {
				t.Errorf("element = %+v, want %+v", got.Type.Elem, tt.elem)
			}
		})
	}

	if byName["nullable"].NotNull {
		t.Error("nullable array is NOT NULL, want nullable")
	}
	if !byName["strings"].NotNull {
		t.Error("strings array is nullable, want NOT NULL")
	}
}
