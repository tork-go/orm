package orm

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// The array column types mirror the scalar ones, one per element kind
// rather than one generic type over all of them.
//
// A single ArrayColumn[T] could not say which element builders applied. An
// array of strings takes MaxLen, which sizes its element and makes the
// column VARCHAR(n)[]; an array of decimals takes Numeric the same way;
// neither means anything on an array of booleans. Being generic over the
// element, one type would have to carry both and let extraction reject the
// wrong one, which is the runtime failure the typed columns exist to turn
// into a compile error.
//
// Naming each kind also puts every array behind a builder method, since a
// method cannot declare a type parameter. t.StringArray("tags") reads the
// same as t.String("name"), where a generic array would have needed the
// package level constructor that JSON columns still use.
//
// Equality compares whole arrays, which every dialect Tork targets supports.
// The membership and length tests arrayOps adds — Has, HasAll, HasAny, Len —
// vary more between databases, so the dialect renders each and one without a
// native array type returns an error naming the operation.

// BoolArrayColumn is a non-nullable bool array column.
type BoolArrayColumn struct {
	chain[[]bool, *BoolArrayColumn]
	equatable[[]bool]
	arrayOps[bool]
	assignable[[]bool]
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*BoolArrayColumn)(nil)

// NewBoolArrayColumn declares a non-nullable bool array column named name.
func NewBoolArrayColumn(name string) *BoolArrayColumn {
	x := &BoolArrayColumn{}
	x.chain = newChain[[]bool](name, x)
	c := x.chain.c
	x.equatable = equatable[[]bool]{c: c}
	x.arrayOps = arrayOps[bool]{c: c}
	x.assignable = assignable[[]bool]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableBoolArrayColumn is a nullable bool array column.
type NullableBoolArrayColumn struct {
	chain[*[]bool, *NullableBoolArrayColumn]
	nullEquatable[[]bool]
	arrayOps[bool]
	nullAssignable[[]bool]
	nullness
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*NullableBoolArrayColumn)(nil)

// NewNullableBoolArrayColumn declares a nullable bool array column named name.
func NewNullableBoolArrayColumn(name string) *NullableBoolArrayColumn {
	x := &NullableBoolArrayColumn{}
	x.chain = newChain[*[]bool](name, x)
	c := x.chain.c
	x.nullEquatable = nullEquatable[[]bool]{c: c}
	x.arrayOps = arrayOps[bool]{c: c}
	x.nullAssignable = nullAssignable[[]bool]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// IntArrayColumn is a non-nullable int array column.
type IntArrayColumn struct {
	chain[[]int, *IntArrayColumn]
	equatable[[]int]
	arrayOps[int]
	assignable[[]int]
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*IntArrayColumn)(nil)

// NewIntArrayColumn declares a non-nullable int array column named name.
func NewIntArrayColumn(name string) *IntArrayColumn {
	x := &IntArrayColumn{}
	x.chain = newChain[[]int](name, x)
	c := x.chain.c
	x.equatable = equatable[[]int]{c: c}
	x.arrayOps = arrayOps[int]{c: c}
	x.assignable = assignable[[]int]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableIntArrayColumn is a nullable int array column.
type NullableIntArrayColumn struct {
	chain[*[]int, *NullableIntArrayColumn]
	nullEquatable[[]int]
	arrayOps[int]
	nullAssignable[[]int]
	nullness
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*NullableIntArrayColumn)(nil)

// NewNullableIntArrayColumn declares a nullable int array column named name.
func NewNullableIntArrayColumn(name string) *NullableIntArrayColumn {
	x := &NullableIntArrayColumn{}
	x.chain = newChain[*[]int](name, x)
	c := x.chain.c
	x.nullEquatable = nullEquatable[[]int]{c: c}
	x.arrayOps = arrayOps[int]{c: c}
	x.nullAssignable = nullAssignable[[]int]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// Int32ArrayColumn is a non-nullable int32 array column.
type Int32ArrayColumn struct {
	chain[[]int32, *Int32ArrayColumn]
	equatable[[]int32]
	arrayOps[int32]
	assignable[[]int32]
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*Int32ArrayColumn)(nil)

// NewInt32ArrayColumn declares a non-nullable int32 array column named name.
func NewInt32ArrayColumn(name string) *Int32ArrayColumn {
	x := &Int32ArrayColumn{}
	x.chain = newChain[[]int32](name, x)
	c := x.chain.c
	x.equatable = equatable[[]int32]{c: c}
	x.arrayOps = arrayOps[int32]{c: c}
	x.assignable = assignable[[]int32]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableInt32ArrayColumn is a nullable int32 array column.
type NullableInt32ArrayColumn struct {
	chain[*[]int32, *NullableInt32ArrayColumn]
	nullEquatable[[]int32]
	arrayOps[int32]
	nullAssignable[[]int32]
	nullness
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*NullableInt32ArrayColumn)(nil)

// NewNullableInt32ArrayColumn declares a nullable int32 array column named name.
func NewNullableInt32ArrayColumn(name string) *NullableInt32ArrayColumn {
	x := &NullableInt32ArrayColumn{}
	x.chain = newChain[*[]int32](name, x)
	c := x.chain.c
	x.nullEquatable = nullEquatable[[]int32]{c: c}
	x.arrayOps = arrayOps[int32]{c: c}
	x.nullAssignable = nullAssignable[[]int32]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// BigIntArrayColumn is a non-nullable int64 array column.
type BigIntArrayColumn struct {
	chain[[]int64, *BigIntArrayColumn]
	equatable[[]int64]
	arrayOps[int64]
	assignable[[]int64]
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*BigIntArrayColumn)(nil)

// NewBigIntArrayColumn declares a non-nullable int64 array column named name.
func NewBigIntArrayColumn(name string) *BigIntArrayColumn {
	x := &BigIntArrayColumn{}
	x.chain = newChain[[]int64](name, x)
	c := x.chain.c
	x.equatable = equatable[[]int64]{c: c}
	x.arrayOps = arrayOps[int64]{c: c}
	x.assignable = assignable[[]int64]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableBigIntArrayColumn is a nullable int64 array column.
type NullableBigIntArrayColumn struct {
	chain[*[]int64, *NullableBigIntArrayColumn]
	nullEquatable[[]int64]
	arrayOps[int64]
	nullAssignable[[]int64]
	nullness
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*NullableBigIntArrayColumn)(nil)

// NewNullableBigIntArrayColumn declares a nullable int64 array column named name.
func NewNullableBigIntArrayColumn(name string) *NullableBigIntArrayColumn {
	x := &NullableBigIntArrayColumn{}
	x.chain = newChain[*[]int64](name, x)
	c := x.chain.c
	x.nullEquatable = nullEquatable[[]int64]{c: c}
	x.arrayOps = arrayOps[int64]{c: c}
	x.nullAssignable = nullAssignable[[]int64]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// FloatArrayColumn is a non-nullable float32 array column.
type FloatArrayColumn struct {
	chain[[]float32, *FloatArrayColumn]
	equatable[[]float32]
	arrayOps[float32]
	assignable[[]float32]
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*FloatArrayColumn)(nil)

// NewFloatArrayColumn declares a non-nullable float32 array column named name.
func NewFloatArrayColumn(name string) *FloatArrayColumn {
	x := &FloatArrayColumn{}
	x.chain = newChain[[]float32](name, x)
	c := x.chain.c
	x.equatable = equatable[[]float32]{c: c}
	x.arrayOps = arrayOps[float32]{c: c}
	x.assignable = assignable[[]float32]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableFloatArrayColumn is a nullable float32 array column.
type NullableFloatArrayColumn struct {
	chain[*[]float32, *NullableFloatArrayColumn]
	nullEquatable[[]float32]
	arrayOps[float32]
	nullAssignable[[]float32]
	nullness
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*NullableFloatArrayColumn)(nil)

// NewNullableFloatArrayColumn declares a nullable float32 array column named name.
func NewNullableFloatArrayColumn(name string) *NullableFloatArrayColumn {
	x := &NullableFloatArrayColumn{}
	x.chain = newChain[*[]float32](name, x)
	c := x.chain.c
	x.nullEquatable = nullEquatable[[]float32]{c: c}
	x.arrayOps = arrayOps[float32]{c: c}
	x.nullAssignable = nullAssignable[[]float32]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// DoubleArrayColumn is a non-nullable float64 array column.
type DoubleArrayColumn struct {
	chain[[]float64, *DoubleArrayColumn]
	equatable[[]float64]
	arrayOps[float64]
	assignable[[]float64]
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*DoubleArrayColumn)(nil)

// NewDoubleArrayColumn declares a non-nullable float64 array column named name.
func NewDoubleArrayColumn(name string) *DoubleArrayColumn {
	x := &DoubleArrayColumn{}
	x.chain = newChain[[]float64](name, x)
	c := x.chain.c
	x.equatable = equatable[[]float64]{c: c}
	x.arrayOps = arrayOps[float64]{c: c}
	x.assignable = assignable[[]float64]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableDoubleArrayColumn is a nullable float64 array column.
type NullableDoubleArrayColumn struct {
	chain[*[]float64, *NullableDoubleArrayColumn]
	nullEquatable[[]float64]
	arrayOps[float64]
	nullAssignable[[]float64]
	nullness
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*NullableDoubleArrayColumn)(nil)

// NewNullableDoubleArrayColumn declares a nullable float64 array column named name.
func NewNullableDoubleArrayColumn(name string) *NullableDoubleArrayColumn {
	x := &NullableDoubleArrayColumn{}
	x.chain = newChain[*[]float64](name, x)
	c := x.chain.c
	x.nullEquatable = nullEquatable[[]float64]{c: c}
	x.arrayOps = arrayOps[float64]{c: c}
	x.nullAssignable = nullAssignable[[]float64]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// DecimalArrayColumn is a non-nullable decimal.Decimal array column.
type DecimalArrayColumn struct {
	chain[[]decimal.Decimal, *DecimalArrayColumn]
	numericBuilder[[]decimal.Decimal, *DecimalArrayColumn]
	equatable[[]decimal.Decimal]
	arrayOps[decimal.Decimal]
	assignable[[]decimal.Decimal]
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*DecimalArrayColumn)(nil)

// NewDecimalArrayColumn declares a non-nullable decimal.Decimal array column named name.
func NewDecimalArrayColumn(name string) *DecimalArrayColumn {
	x := &DecimalArrayColumn{}
	x.chain = newChain[[]decimal.Decimal](name, x)
	c := x.chain.c
	x.numericBuilder = numericBuilder[[]decimal.Decimal, *DecimalArrayColumn]{c: c, self: x}
	x.equatable = equatable[[]decimal.Decimal]{c: c}
	x.arrayOps = arrayOps[decimal.Decimal]{c: c}
	x.assignable = assignable[[]decimal.Decimal]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableDecimalArrayColumn is a nullable decimal.Decimal array column.
type NullableDecimalArrayColumn struct {
	chain[*[]decimal.Decimal, *NullableDecimalArrayColumn]
	numericBuilder[*[]decimal.Decimal, *NullableDecimalArrayColumn]
	nullEquatable[[]decimal.Decimal]
	arrayOps[decimal.Decimal]
	nullAssignable[[]decimal.Decimal]
	nullness
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*NullableDecimalArrayColumn)(nil)

// NewNullableDecimalArrayColumn declares a nullable decimal.Decimal array column named name.
func NewNullableDecimalArrayColumn(name string) *NullableDecimalArrayColumn {
	x := &NullableDecimalArrayColumn{}
	x.chain = newChain[*[]decimal.Decimal](name, x)
	c := x.chain.c
	x.numericBuilder = numericBuilder[*[]decimal.Decimal, *NullableDecimalArrayColumn]{c: c, self: x}
	x.nullEquatable = nullEquatable[[]decimal.Decimal]{c: c}
	x.arrayOps = arrayOps[decimal.Decimal]{c: c}
	x.nullAssignable = nullAssignable[[]decimal.Decimal]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// StringArrayColumn is a non-nullable string array column.
type StringArrayColumn struct {
	chain[[]string, *StringArrayColumn]
	lengthBuilder[[]string, *StringArrayColumn]
	equatable[[]string]
	arrayOps[string]
	assignable[[]string]
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*StringArrayColumn)(nil)

// NewStringArrayColumn declares a non-nullable string array column named name.
func NewStringArrayColumn(name string) *StringArrayColumn {
	x := &StringArrayColumn{}
	x.chain = newChain[[]string](name, x)
	c := x.chain.c
	x.lengthBuilder = lengthBuilder[[]string, *StringArrayColumn]{c: c, self: x}
	x.equatable = equatable[[]string]{c: c}
	x.arrayOps = arrayOps[string]{c: c}
	x.assignable = assignable[[]string]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableStringArrayColumn is a nullable string array column.
type NullableStringArrayColumn struct {
	chain[*[]string, *NullableStringArrayColumn]
	lengthBuilder[*[]string, *NullableStringArrayColumn]
	nullEquatable[[]string]
	arrayOps[string]
	nullAssignable[[]string]
	nullness
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*NullableStringArrayColumn)(nil)

// NewNullableStringArrayColumn declares a nullable string array column named name.
func NewNullableStringArrayColumn(name string) *NullableStringArrayColumn {
	x := &NullableStringArrayColumn{}
	x.chain = newChain[*[]string](name, x)
	c := x.chain.c
	x.lengthBuilder = lengthBuilder[*[]string, *NullableStringArrayColumn]{c: c, self: x}
	x.nullEquatable = nullEquatable[[]string]{c: c}
	x.arrayOps = arrayOps[string]{c: c}
	x.nullAssignable = nullAssignable[[]string]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// TimeArrayColumn is a non-nullable time.Time array column.
type TimeArrayColumn struct {
	chain[[]time.Time, *TimeArrayColumn]
	equatable[[]time.Time]
	arrayOps[time.Time]
	assignable[[]time.Time]
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*TimeArrayColumn)(nil)

// NewTimeArrayColumn declares a non-nullable time.Time array column named name.
func NewTimeArrayColumn(name string) *TimeArrayColumn {
	x := &TimeArrayColumn{}
	x.chain = newChain[[]time.Time](name, x)
	c := x.chain.c
	x.equatable = equatable[[]time.Time]{c: c}
	x.arrayOps = arrayOps[time.Time]{c: c}
	x.assignable = assignable[[]time.Time]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableTimeArrayColumn is a nullable time.Time array column.
type NullableTimeArrayColumn struct {
	chain[*[]time.Time, *NullableTimeArrayColumn]
	nullEquatable[[]time.Time]
	arrayOps[time.Time]
	nullAssignable[[]time.Time]
	nullness
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*NullableTimeArrayColumn)(nil)

// NewNullableTimeArrayColumn declares a nullable time.Time array column named name.
func NewNullableTimeArrayColumn(name string) *NullableTimeArrayColumn {
	x := &NullableTimeArrayColumn{}
	x.chain = newChain[*[]time.Time](name, x)
	c := x.chain.c
	x.nullEquatable = nullEquatable[[]time.Time]{c: c}
	x.arrayOps = arrayOps[time.Time]{c: c}
	x.nullAssignable = nullAssignable[[]time.Time]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// UUIDArrayColumn is a non-nullable uuid.UUID array column.
type UUIDArrayColumn struct {
	chain[[]uuid.UUID, *UUIDArrayColumn]
	equatable[[]uuid.UUID]
	arrayOps[uuid.UUID]
	assignable[[]uuid.UUID]
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*UUIDArrayColumn)(nil)

// NewUUIDArrayColumn declares a non-nullable uuid.UUID array column named name.
func NewUUIDArrayColumn(name string) *UUIDArrayColumn {
	x := &UUIDArrayColumn{}
	x.chain = newChain[[]uuid.UUID](name, x)
	c := x.chain.c
	x.equatable = equatable[[]uuid.UUID]{c: c}
	x.arrayOps = arrayOps[uuid.UUID]{c: c}
	x.assignable = assignable[[]uuid.UUID]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableUUIDArrayColumn is a nullable uuid.UUID array column.
type NullableUUIDArrayColumn struct {
	chain[*[]uuid.UUID, *NullableUUIDArrayColumn]
	nullEquatable[[]uuid.UUID]
	arrayOps[uuid.UUID]
	nullAssignable[[]uuid.UUID]
	nullness
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*NullableUUIDArrayColumn)(nil)

// NewNullableUUIDArrayColumn declares a nullable uuid.UUID array column named name.
func NewNullableUUIDArrayColumn(name string) *NullableUUIDArrayColumn {
	x := &NullableUUIDArrayColumn{}
	x.chain = newChain[*[]uuid.UUID](name, x)
	c := x.chain.c
	x.nullEquatable = nullEquatable[[]uuid.UUID]{c: c}
	x.arrayOps = arrayOps[uuid.UUID]{c: c}
	x.nullAssignable = nullAssignable[[]uuid.UUID]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}
