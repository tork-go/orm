package orm

// The mixins here carry the builder methods that are meaningful for some
// column kinds but not others. A concrete column type embeds only the ones
// that apply to it, which is what makes IntColumn.MaxLen and
// StringColumn.Numeric compile errors rather than silently-ignored calls.
//
// Column[T] itself still declares all of them, because Go cannot add a
// method to only some instantiations of a generic type, which is the
// limitation these mixins exist to work around one level up. Each mixin
// holds the column in a named field rather than embedding it, so chain
// stays the single promotion path to ColumnMeta (see mixin_chain.go), and
// carries self for the same covariance reason chain does.

// lengthBuilder supplies MaxLen to string-like columns.
type lengthBuilder[T any, Self any] struct {
	c    *Column[T]
	self Self
}

// MaxLen sets the column's maximum length, rendered as VARCHAR(n).
func (b lengthBuilder[T, Self]) MaxLen(n int) Self {
	b.c.MaxLen(n)
	return b.self
}

// numericBuilder supplies Numeric to fixed-point columns.
type numericBuilder[T any, Self any] struct {
	c    *Column[T]
	self Self
}

// Numeric sets explicit precision and scale, rendered as NUMERIC(p,s).
// Without it a decimal column renders as bare NUMERIC (arbitrary
// precision), the same relationship MaxLen has to a bare string column.
func (b numericBuilder[T, Self]) Numeric(precision, scale int) Self {
	b.c.Numeric(precision, scale)
	return b.self
}

// jsonBuilder supplies the JSON storage builders. It is embedded by the
// document column types, not by scalars: marking an int column as JSONB is
// not a thing a caller ever means to do.
type jsonBuilder[T any, Self any] struct {
	c    *Column[T]
	self Self
}

// JSON marks the column as stored as JSON, using encoding/json by default.
func (b jsonBuilder[T, Self]) JSON() Self {
	b.c.JSON()
	return b.self
}

// JSONB marks the column as stored as JSONB, using encoding/json by
// default.
func (b jsonBuilder[T, Self]) JSONB() Self {
	b.c.JSONB()
	return b.self
}

// Serialize overrides the default encoding/json marshal/unmarshal pair.
// Calling it alone, without JSON or JSONB, implies JSONB.
func (b jsonBuilder[T, Self]) Serialize(marshal func(T) ([]byte, error), unmarshal func([]byte) (T, error)) Self {
	b.c.Serialize(marshal, unmarshal)
	return b.self
}

// enumBuilder supplies Enum to string-backed columns.
type enumBuilder[T any, Self any] struct {
	c    *Column[T]
	self Self
}

// Enum declares the column as a native enum of type typeName with the
// given values, in order.
func (b enumBuilder[T, Self]) Enum(typeName string, values ...string) Self {
	b.c.Enum(typeName, values...)
	return b.self
}

// Ref is a column of Go type T, as the target of a foreign key.
//
// Both *Column[T] and the typed column types satisfy it, each through
// their own Base. It exists so References can insist that a key and the
// column it points at agree on their type: a Ref[int] is accepted only
// where an int key is being declared, so pointing an int column at a
// string one does not compile.
type Ref[T any] interface {
	ColumnMeta
	Base() *Column[T]
}

// refBuilder supplies the foreign-key builders to a column that can carry
// one.
//
// It has two type parameters where the other mixins have one. T is the
// column's own Go type and R is the referenced column's, and they differ
// whenever a nullable key points at a non-nullable primary key, the
// ordinary case for an optional relationship. Fixing R separately is
// what keeps the type check meaningful: a *NullableIntColumn is
// refBuilder[*int, int, ...], so it accepts an int column and rejects a
// string one, without demanding its own *int on the far side.
type refBuilder[T any, R any, Self any] struct {
	c    *Column[T]
	self Self
}

// References marks this column as a foreign key onto ref, which must be a
// column of the matching type.
//
// ref must belong to a table, meaning it comes from a model declared with
// DefineTable, since the referenced table name is read back from it. Use
// ReferencesTable for a target that has none.
func (b refBuilder[T, R, Self]) References(ref Ref[R]) Self {
	b.c.References(ref)
	return b.self
}

// ReferencesTable marks this column as a foreign key onto the named table
// and column, for targets References cannot name.
func (b refBuilder[T, R, Self]) ReferencesTable(table, column string) Self {
	b.c.ReferencesTable(table, column)
	return b.self
}

// OnDelete sets the referential action for a deleted referenced row.
func (b refBuilder[T, R, Self]) OnDelete(action ForeignKeyAction) Self {
	b.c.OnDelete(action)
	return b.self
}

// OnUpdate sets the referential action for an updated referenced row.
func (b refBuilder[T, R, Self]) OnUpdate(action ForeignKeyAction) Self {
	b.c.OnUpdate(action)
	return b.self
}
