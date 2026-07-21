package orm

import "reflect"

// chain supplies the builder methods every column has, retyped to return
// the concrete column type instead of *Column[T].
//
// The problem it solves: Go has no return-type covariance through
// embedding. A type that embeds Column[T] inherits NotNull, but the
// inherited method still returns *Column[T], so the second link of
// col.NotNull().Unique() is a *Column[T] and the chain has silently
// downcast. The only fix is to redeclare every chain-returning builder on
// the embedding type. Written by hand that is one near-identical method
// per builder per column type; the mixins write them once, generically.
//
// Self is the concrete column type, always a pointer (*ForeignKey[T],
// *StringColumn, ...). Because Go cannot name "the type embedding me",
// each constructor passes its own address in via self, which is what the
// builders return. That makes the two-phase constructor mandatory:
// allocate the outer value first, then install the mixins pointing back
// at it. See newStringColumn for the canonical shape.
//
// chain deliberately does *not* embed *Column[T]. Embedding would promote
// all twelve of Column's builders onto every concrete column type, and
// leaving a builder mixin off would then hide nothing. Omitting
// lengthBuilder from IntColumn would simply un-shadow Column's own MaxLen,
// so Users.ID.MaxLen(3) would still compile and would quietly return
// *Column[int], breaking the chain's type. Holding the column in a named
// field instead means nothing reaches the concrete type except what chain
// and the embedded mixins choose to expose, which is what makes
// IntColumn.MaxLen and StringColumn.Numeric the compile errors they should
// be.
//
// The cost of that choice is that chain must republish Column's read
// accessors itself, since they no longer promote. They are written once
// here, generically, and are what every concrete column type's ColumnMeta
// conformance rests on.
//
// Only the six builders meaningful for every T live here. MaxLen, Numeric,
// JSON/JSONB/Serialize, and Enum apply to some kinds only, so they live in
// separate mixins (see mixin_builders.go) that a concrete type embeds only
// when they apply.
type chain[T any, Self any] struct {
	c    *Column[T]
	self Self
}

// newChain builds a column named name and binds it to self, the concrete
// column value that embeds the returned chain. self must already be
// allocated; see newStringColumn.
func newChain[T any, Self any](name string, self Self) chain[T, Self] {
	return chain[T, Self]{c: NewColumn[T](name), self: self}
}

// PrimaryKey marks the column as (part of) the table's primary key.
func (b chain[T, Self]) PrimaryKey() Self {
	b.c.PrimaryKey()
	return b.self
}

// Unique marks the column as having a unique constraint.
func (b chain[T, Self]) Unique() Self {
	b.c.Unique()
	return b.self
}

// NotNull marks the column as disallowing SQL NULL.
func (b chain[T, Self]) NotNull() Self {
	b.c.NotNull()
	return b.self
}

// Index marks the column as having a plain (non-unique) index.
func (b chain[T, Self]) Index() Self {
	b.c.Index()
	return b.self
}

// ServerDefault stores a raw SQL expression for the column's default.
func (b chain[T, Self]) ServerDefault(expr string) Self {
	b.c.ServerDefault(expr)
	return b.self
}

// GeneratedByClient stores the column's Go-side value generator.
func (b chain[T, Self]) GeneratedByClient(gen func() T) Self {
	b.c.GeneratedByClient(gen)
	return b.self
}

// The accessors below republish Column[T]'s read methods. chain holds its
// column rather than embedding it (see the type's doc comment), so nothing
// promotes automatically and these must be spelled out. Written once here,
// they are what makes every concrete column type satisfy ColumnMeta.

// Name returns the column's database name.
func (b chain[T, Self]) Name() string { return b.c.Name() }

// OwnerTable returns the name of the table this column belongs to, or ""
// if it has not been bound to one.
func (b chain[T, Self]) OwnerTable() string { return b.c.OwnerTable() }

// setOwner binds the column to its table, satisfying ownerSetter so
// DefineTable can bind a typed column it holds only as a ColumnMeta.
func (b chain[T, Self]) setOwner(name string) { b.c.setOwner(name) }

// ReferencedTable returns the table this column references, or "" if it
// references nothing.
func (b chain[T, Self]) ReferencedTable() string { return b.c.ReferencedTable() }

// ReferencedColumn returns the column this column references, or "" if it
// references nothing.
func (b chain[T, Self]) ReferencedColumn() string { return b.c.ReferencedColumn() }

// OnDeleteAction returns the action set by OnDelete.
func (b chain[T, Self]) OnDeleteAction() ForeignKeyAction { return b.c.OnDeleteAction() }

// OnUpdateAction returns the action set by OnUpdate.
func (b chain[T, Self]) OnUpdateAction() ForeignKeyAction { return b.c.OnUpdateAction() }

// Base returns the generic column this typed column wraps.
//
// It is what makes a typed column satisfy Ref[T], so it can be the target
// of a foreign key declared with References. Prefer the typed methods for
// everything else, since reaching through Base gives back the untyped
// builders the concrete types exist to hide.
func (b chain[T, Self]) Base() *Column[T] { return b.c }

// IsPrimaryKey reports whether PrimaryKey was called.
func (b chain[T, Self]) IsPrimaryKey() bool { return b.c.IsPrimaryKey() }

// IsUnique reports whether Unique was called.
func (b chain[T, Self]) IsUnique() bool { return b.c.IsUnique() }

// HasNotNull reports whether NotNull was called.
func (b chain[T, Self]) HasNotNull() bool { return b.c.HasNotNull() }

// MaxLength returns the length set by MaxLen, and whether it was set.
func (b chain[T, Self]) MaxLength() (int, bool) { return b.c.MaxLength() }

// IsIndexed reports whether Index was called.
func (b chain[T, Self]) IsIndexed() bool { return b.c.IsIndexed() }

// ServerDefaultExpr returns the expression set by ServerDefault, and
// whether it was set.
func (b chain[T, Self]) ServerDefaultExpr() (string, bool) { return b.c.ServerDefaultExpr() }

// IsClientGenerated reports whether GeneratedByClient was called.
func (b chain[T, Self]) IsClientGenerated() bool { return b.c.IsClientGenerated() }

// GoType returns the column's Go value type, T.
func (b chain[T, Self]) GoType() reflect.Type { return b.c.GoType() }

// IsNullable reports whether T is a pointer kind.
func (b chain[T, Self]) IsNullable() bool { return b.c.IsNullable() }

// NumericPrecisionScale returns the precision and scale set by Numeric,
// and whether they were set.
func (b chain[T, Self]) NumericPrecisionScale() (int, int, bool) {
	return b.c.NumericPrecisionScale()
}

// IsJSON reports whether the column is stored as JSON.
func (b chain[T, Self]) IsJSON() bool { return b.c.IsJSON() }

// IsJSONB reports whether the column is stored as JSONB.
func (b chain[T, Self]) IsJSONB() bool { return b.c.IsJSONB() }

// IsSerialized reports whether a custom marshal/unmarshal pair was set.
func (b chain[T, Self]) IsSerialized() bool { return b.c.IsSerialized() }

// EnumSpec returns the enum type name and values set by Enum, and whether
// they were set.
func (b chain[T, Self]) EnumSpec() (string, []string, bool) { return b.c.EnumSpec() }

// Generator returns the generator set by GeneratedByClient, and whether it
// was set. Like Serializer it mentions T, so it is not part of ColumnMeta.
func (b chain[T, Self]) Generator() (func() T, bool) { return b.c.Generator() }

// Serializer returns the marshal/unmarshal pair set by Serialize, and
// whether it was set.
func (b chain[T, Self]) Serializer() (func(T) ([]byte, error), func([]byte) (T, error), bool) {
	return b.c.Serializer()
}

// The three below republish ValueCodec, so a typed column satisfies it as
// well as a *Column[T] does. Query building reaches columns as ColumnMeta
// and asserts to ValueCodec when it needs to encode or decode a value, so
// a column type that failed the assertion would be silently unusable for
// generated values and document columns alike.

// GenerateAny returns a value from the column's client side generator, or
// false if it has none.
func (b chain[T, Self]) GenerateAny() (any, bool) { return b.c.GenerateAny() }

// MarshalAny encodes v for storage in a document column.
func (b chain[T, Self]) MarshalAny(v any) ([]byte, error) { return b.c.MarshalAny(v) }

// UnmarshalAny decodes b into the column's Go type.
func (b chain[T, Self]) UnmarshalAny(bs []byte) (any, error) { return b.c.UnmarshalAny(bs) }
