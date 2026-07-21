package orm

import "github.com/shopspring/decimal"

// The concrete column types follow one shape throughout, here and in
// columns_string.go, columns_misc.go, and columns_document.go.
//
// Each embeds chain (which carries the universal builders and republishes
// every ColumnMeta accessor), then whichever builder and operation mixins
// its kind supports. The set it embeds *is* its API: a type that omits
// textOps has no Contains, one that omits nullness has no IsNull, one that
// omits lengthBuilder has no MaxLen, and calling any of them is a compile
// error rather than a runtime failure. That is the entire point of having
// concrete types instead of exposing Column[T] directly, and it only holds
// because chain declines to embed Column. See mixin_chain.go.
//
// Every type is followed by a `var _ ColumnMeta` assertion. A mixin that
// accidentally shadowed an accessor would make that method ambiguous, and
// Go's response to an ambiguous promotion is to drop the method from the
// type's method set rather than to complain, so the column would quietly
// stop satisfying ColumnMeta, and schema extraction's walkFields would
// skip it with no error anywhere. The assertion turns that silent failure
// into a build failure.
//
// Constructors are two-phase because the builder mixins return the address
// stored in their self field, which cannot be taken before the value
// exists. See mixin_chain.go.
//
// The numeric types map one-to-one onto the schema kinds rather than onto
// Go's convenient names: Float is float32 (KindFloat, REAL) and Double is
// float64 (KindDouble, DOUBLE PRECISION), because naming the 64-bit one
// Float would have it render as DOUBLE PRECISION while reading as REAL.
// Int and Int32 both reach KindInteger; both exist because a column's Go
// type must match its entity field exactly, so an int32 field needs an
// int32 column.

// IntColumn is a non-nullable int column (INTEGER).
type IntColumn struct {
	chain[int, *IntColumn]
	refBuilder[int, int, *IntColumn]
	equatable[int]
	ordered[int]
	assignable[int]
	sortable
}

var _ ColumnMeta = (*IntColumn)(nil)

// NewIntColumn declares a non-nullable int column named name.
func NewIntColumn(name string) *IntColumn {
	x := &IntColumn{}
	x.chain = newChain[int](name, x)
	c := x.chain.c
	x.refBuilder = refBuilder[int, int, *IntColumn]{c: c, self: x}
	x.equatable = equatable[int]{c: c}
	x.ordered = ordered[int]{c: c}
	x.assignable = assignable[int]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableIntColumn is a nullable int column, backed by Column[*int].
type NullableIntColumn struct {
	chain[*int, *NullableIntColumn]
	refBuilder[*int, int, *NullableIntColumn]
	nullEquatable[int]
	nullOrdered[int]
	nullAssignable[int]
	nullness
	sortable
}

var _ ColumnMeta = (*NullableIntColumn)(nil)

// NewNullableIntColumn declares a nullable int column named name.
func NewNullableIntColumn(name string) *NullableIntColumn {
	x := &NullableIntColumn{}
	x.chain = newChain[*int](name, x)
	c := x.chain.c
	x.refBuilder = refBuilder[*int, int, *NullableIntColumn]{c: c, self: x}
	x.nullEquatable = nullEquatable[int]{c: c}
	x.nullOrdered = nullOrdered[int]{c: c}
	x.nullAssignable = nullAssignable[int]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// Int32Column is a non-nullable int32 column (INTEGER).
type Int32Column struct {
	chain[int32, *Int32Column]
	refBuilder[int32, int32, *Int32Column]
	equatable[int32]
	ordered[int32]
	assignable[int32]
	sortable
}

var _ ColumnMeta = (*Int32Column)(nil)

// NewInt32Column declares a non-nullable int32 column named name. It
// renders identically to an int column; use it when the entity field is
// an int32, since the two must match exactly.
func NewInt32Column(name string) *Int32Column {
	x := &Int32Column{}
	x.chain = newChain[int32](name, x)
	c := x.chain.c
	x.refBuilder = refBuilder[int32, int32, *Int32Column]{c: c, self: x}
	x.equatable = equatable[int32]{c: c}
	x.ordered = ordered[int32]{c: c}
	x.assignable = assignable[int32]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableInt32Column is a nullable int32 column.
type NullableInt32Column struct {
	chain[*int32, *NullableInt32Column]
	refBuilder[*int32, int32, *NullableInt32Column]
	nullEquatable[int32]
	nullOrdered[int32]
	nullAssignable[int32]
	nullness
	sortable
}

var _ ColumnMeta = (*NullableInt32Column)(nil)

// NewNullableInt32Column declares a nullable int32 column named name.
func NewNullableInt32Column(name string) *NullableInt32Column {
	x := &NullableInt32Column{}
	x.chain = newChain[*int32](name, x)
	c := x.chain.c
	x.refBuilder = refBuilder[*int32, int32, *NullableInt32Column]{c: c, self: x}
	x.nullEquatable = nullEquatable[int32]{c: c}
	x.nullOrdered = nullOrdered[int32]{c: c}
	x.nullAssignable = nullAssignable[int32]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// BigIntColumn is a non-nullable int64 column (BIGINT).
type BigIntColumn struct {
	chain[int64, *BigIntColumn]
	refBuilder[int64, int64, *BigIntColumn]
	equatable[int64]
	ordered[int64]
	assignable[int64]
	sortable
}

var _ ColumnMeta = (*BigIntColumn)(nil)

// NewBigIntColumn declares a non-nullable int64 column named name.
func NewBigIntColumn(name string) *BigIntColumn {
	x := &BigIntColumn{}
	x.chain = newChain[int64](name, x)
	c := x.chain.c
	x.refBuilder = refBuilder[int64, int64, *BigIntColumn]{c: c, self: x}
	x.equatable = equatable[int64]{c: c}
	x.ordered = ordered[int64]{c: c}
	x.assignable = assignable[int64]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableBigIntColumn is a nullable int64 column.
type NullableBigIntColumn struct {
	chain[*int64, *NullableBigIntColumn]
	refBuilder[*int64, int64, *NullableBigIntColumn]
	nullEquatable[int64]
	nullOrdered[int64]
	nullAssignable[int64]
	nullness
	sortable
}

var _ ColumnMeta = (*NullableBigIntColumn)(nil)

// NewNullableBigIntColumn declares a nullable int64 column named name.
func NewNullableBigIntColumn(name string) *NullableBigIntColumn {
	x := &NullableBigIntColumn{}
	x.chain = newChain[*int64](name, x)
	c := x.chain.c
	x.refBuilder = refBuilder[*int64, int64, *NullableBigIntColumn]{c: c, self: x}
	x.nullEquatable = nullEquatable[int64]{c: c}
	x.nullOrdered = nullOrdered[int64]{c: c}
	x.nullAssignable = nullAssignable[int64]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// FloatColumn is a non-nullable float32 column (REAL).
type FloatColumn struct {
	chain[float32, *FloatColumn]
	equatable[float32]
	ordered[float32]
	assignable[float32]
	sortable
}

var _ ColumnMeta = (*FloatColumn)(nil)

// NewFloatColumn declares a non-nullable float32 column named name. For
// float64, use NewDoubleColumn.
func NewFloatColumn(name string) *FloatColumn {
	x := &FloatColumn{}
	x.chain = newChain[float32](name, x)
	c := x.chain.c
	x.equatable = equatable[float32]{c: c}
	x.ordered = ordered[float32]{c: c}
	x.assignable = assignable[float32]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableFloatColumn is a nullable float32 column.
type NullableFloatColumn struct {
	chain[*float32, *NullableFloatColumn]
	nullEquatable[float32]
	nullOrdered[float32]
	nullAssignable[float32]
	nullness
	sortable
}

var _ ColumnMeta = (*NullableFloatColumn)(nil)

// NewNullableFloatColumn declares a nullable float32 column named name.
func NewNullableFloatColumn(name string) *NullableFloatColumn {
	x := &NullableFloatColumn{}
	x.chain = newChain[*float32](name, x)
	c := x.chain.c
	x.nullEquatable = nullEquatable[float32]{c: c}
	x.nullOrdered = nullOrdered[float32]{c: c}
	x.nullAssignable = nullAssignable[float32]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// DoubleColumn is a non-nullable float64 column (DOUBLE PRECISION).
type DoubleColumn struct {
	chain[float64, *DoubleColumn]
	equatable[float64]
	ordered[float64]
	assignable[float64]
	sortable
}

var _ ColumnMeta = (*DoubleColumn)(nil)

// NewDoubleColumn declares a non-nullable float64 column named name.
func NewDoubleColumn(name string) *DoubleColumn {
	x := &DoubleColumn{}
	x.chain = newChain[float64](name, x)
	c := x.chain.c
	x.equatable = equatable[float64]{c: c}
	x.ordered = ordered[float64]{c: c}
	x.assignable = assignable[float64]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableDoubleColumn is a nullable float64 column.
type NullableDoubleColumn struct {
	chain[*float64, *NullableDoubleColumn]
	nullEquatable[float64]
	nullOrdered[float64]
	nullAssignable[float64]
	nullness
	sortable
}

var _ ColumnMeta = (*NullableDoubleColumn)(nil)

// NewNullableDoubleColumn declares a nullable float64 column named name.
func NewNullableDoubleColumn(name string) *NullableDoubleColumn {
	x := &NullableDoubleColumn{}
	x.chain = newChain[*float64](name, x)
	c := x.chain.c
	x.nullEquatable = nullEquatable[float64]{c: c}
	x.nullOrdered = nullOrdered[float64]{c: c}
	x.nullAssignable = nullAssignable[float64]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// DecimalColumn is a non-nullable fixed-point column (NUMERIC), backed by
// decimal.Decimal. It embeds numericBuilder, so it is the only scalar kind
// offering Numeric(precision, scale).
type DecimalColumn struct {
	chain[decimal.Decimal, *DecimalColumn]
	numericBuilder[decimal.Decimal, *DecimalColumn]
	equatable[decimal.Decimal]
	ordered[decimal.Decimal]
	assignable[decimal.Decimal]
	sortable
}

var _ ColumnMeta = (*DecimalColumn)(nil)

// NewDecimalColumn declares a non-nullable decimal column named name.
// Without a Numeric call it renders as bare NUMERIC (arbitrary precision).
func NewDecimalColumn(name string) *DecimalColumn {
	x := &DecimalColumn{}
	x.chain = newChain[decimal.Decimal](name, x)
	c := x.chain.c
	x.numericBuilder = numericBuilder[decimal.Decimal, *DecimalColumn]{c: c, self: x}
	x.equatable = equatable[decimal.Decimal]{c: c}
	x.ordered = ordered[decimal.Decimal]{c: c}
	x.assignable = assignable[decimal.Decimal]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableDecimalColumn is a nullable fixed-point column.
type NullableDecimalColumn struct {
	chain[*decimal.Decimal, *NullableDecimalColumn]
	numericBuilder[*decimal.Decimal, *NullableDecimalColumn]
	nullEquatable[decimal.Decimal]
	nullOrdered[decimal.Decimal]
	nullAssignable[decimal.Decimal]
	nullness
	sortable
}

var _ ColumnMeta = (*NullableDecimalColumn)(nil)

// NewNullableDecimalColumn declares a nullable decimal column named name.
func NewNullableDecimalColumn(name string) *NullableDecimalColumn {
	x := &NullableDecimalColumn{}
	x.chain = newChain[*decimal.Decimal](name, x)
	c := x.chain.c
	x.numericBuilder = numericBuilder[*decimal.Decimal, *NullableDecimalColumn]{c: c, self: x}
	x.nullEquatable = nullEquatable[decimal.Decimal]{c: c}
	x.nullOrdered = nullOrdered[decimal.Decimal]{c: c}
	x.nullAssignable = nullAssignable[decimal.Decimal]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}
