package orm_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/tork-go/orm"
)

// Every array kind, through both the package level constructor and the
// builder method. The two have to agree, and each type has to keep its
// ColumnMeta conformance: an ambiguous promotion would make Go drop the
// method rather than complain, and the column would silently vanish from
// schema extraction.
func TestArrayColumns_EveryKind(t *testing.T) {
	b := &orm.TableBuilder[orm.NoEntity]{}

	tests := []struct {
		name   string
		col    orm.ColumnMeta
		viaB   orm.ColumnMeta
		goType reflect.Type
	}{
		{"bool", orm.NewBoolArrayColumn("c"), b.BoolArray("c"), reflect.TypeFor[[]bool]()},
		{"int", orm.NewIntArrayColumn("c"), b.IntArray("c"), reflect.TypeFor[[]int]()},
		{"int32", orm.NewInt32ArrayColumn("c"), b.Int32Array("c"), reflect.TypeFor[[]int32]()},
		{"bigint", orm.NewBigIntArrayColumn("c"), b.BigIntArray("c"), reflect.TypeFor[[]int64]()},
		{"float", orm.NewFloatArrayColumn("c"), b.FloatArray("c"), reflect.TypeFor[[]float32]()},
		{"double", orm.NewDoubleArrayColumn("c"), b.DoubleArray("c"), reflect.TypeFor[[]float64]()},
		{"decimal", orm.NewDecimalArrayColumn("c"), b.DecimalArray("c"), reflect.TypeFor[[]decimal.Decimal]()},
		{"string", orm.NewStringArrayColumn("c"), b.StringArray("c"), reflect.TypeFor[[]string]()},
		{"time", orm.NewTimeArrayColumn("c"), b.TimeArray("c"), reflect.TypeFor[[]time.Time]()},
		{"uuid", orm.NewUUIDArrayColumn("c"), b.UUIDArray("c"), reflect.TypeFor[[]uuid.UUID]()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.col.GoType(); got != tt.goType {
				t.Errorf("GoType() = %v, want %v", got, tt.goType)
			}
			if got := tt.viaB.GoType(); got != tt.goType {
				t.Errorf("builder GoType() = %v, want %v", got, tt.goType)
			}
			if tt.col.IsNullable() {
				t.Error("IsNullable() = true, want false")
			}
			if _, ok := tt.col.(orm.ValueCodec); !ok {
				t.Error("does not satisfy ValueCodec")
			}
		})
	}
}

func TestNullableArrayColumns_EveryKind(t *testing.T) {
	b := &orm.TableBuilder[orm.NoEntity]{}

	tests := []struct {
		name   string
		col    orm.ColumnMeta
		viaB   orm.ColumnMeta
		goType reflect.Type
	}{
		{"bool", orm.NewNullableBoolArrayColumn("c"), b.NullableBoolArray("c"), reflect.TypeFor[*[]bool]()},
		{"int", orm.NewNullableIntArrayColumn("c"), b.NullableIntArray("c"), reflect.TypeFor[*[]int]()},
		{"int32", orm.NewNullableInt32ArrayColumn("c"), b.NullableInt32Array("c"), reflect.TypeFor[*[]int32]()},
		{"bigint", orm.NewNullableBigIntArrayColumn("c"), b.NullableBigIntArray("c"), reflect.TypeFor[*[]int64]()},
		{"float", orm.NewNullableFloatArrayColumn("c"), b.NullableFloatArray("c"), reflect.TypeFor[*[]float32]()},
		{"double", orm.NewNullableDoubleArrayColumn("c"), b.NullableDoubleArray("c"), reflect.TypeFor[*[]float64]()},
		{"decimal", orm.NewNullableDecimalArrayColumn("c"), b.NullableDecimalArray("c"), reflect.TypeFor[*[]decimal.Decimal]()},
		{"string", orm.NewNullableStringArrayColumn("c"), b.NullableStringArray("c"), reflect.TypeFor[*[]string]()},
		{"time", orm.NewNullableTimeArrayColumn("c"), b.NullableTimeArray("c"), reflect.TypeFor[*[]time.Time]()},
		{"uuid", orm.NewNullableUUIDArrayColumn("c"), b.NullableUUIDArray("c"), reflect.TypeFor[*[]uuid.UUID]()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.col.GoType(); got != tt.goType {
				t.Errorf("GoType() = %v, want %v", got, tt.goType)
			}
			if got := tt.viaB.GoType(); got != tt.goType {
				t.Errorf("builder GoType() = %v, want %v", got, tt.goType)
			}
			if !tt.col.IsNullable() {
				t.Error("IsNullable() = false, want true")
			}
		})
	}
}

// The element builders exist only where they mean something, and they size
// the element rather than the array.
func TestArrayColumns_ElementBuilders(t *testing.T) {
	s := orm.NewStringArrayColumn("tags").MaxLen(20).NotNull()
	if n, ok := s.MaxLength(); !ok || n != 20 {
		t.Errorf("MaxLength() = (%d, %v), want (20, true)", n, ok)
	}
	if !s.HasNotNull() {
		t.Error("the chain lost NotNull after MaxLen")
	}

	d := orm.NewDecimalArrayColumn("prices").Numeric(10, 2).Index()
	p, sc, ok := d.NumericPrecisionScale()
	if !ok || p != 10 || sc != 2 {
		t.Errorf("NumericPrecisionScale() = (%d, %d, %v), want (10, 2, true)", p, sc, ok)
	}
	if !d.IsIndexed() {
		t.Error("the chain lost Index after Numeric")
	}
}

// Array operations compare whole arrays.
func TestArrayColumns_Operations(t *testing.T) {
	c := orm.NewStringArrayColumn("tags")
	p, ok := c.Equals([]string{"a", "b"}).(orm.Comparison)
	if !ok {
		t.Fatalf("Equals() returned %T, want orm.Comparison", c.Equals(nil))
	}
	if got, isSlice := p.Value.([]string); !isSlice || len(got) != 2 {
		t.Errorf("Equals().Value = %v, want the whole slice", p.Value)
	}
	if o := c.Asc(); o.Desc {
		t.Error("Asc().Desc = true")
	}

	n := orm.NewNullableStringArrayColumn("tags")
	if _, ok := n.IsNull().(orm.Nullness); !ok {
		t.Error("nullable array has no IsNull")
	}
	if a := n.SetNull(); a.Value != nil {
		t.Errorf("SetNull().Value = %v, want nil", a.Value)
	}
	if a := n.Set([]string{"x"}); a.Value == nil {
		t.Error("Set() lost its value")
	}
}
