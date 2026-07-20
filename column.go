package orm

import "reflect"

// Column declares a single typed table column. T is the column's Go value
// type; using a pointer type for T (e.g. Column[*string]) marks the column
// as nullable, mirroring Optional[T] semantics. Construct one with
// NewColumn and configure it with the chainable builder methods below:
//
//	Username := NewColumn[string]("username").Unique().NotNull().MaxLen(30)
type Column[T any] struct {
	name       string
	primaryKey bool
	unique     bool
	notNull    bool
	maxLen     int
	maxLenSet  bool
}

// newColumn builds the shared Column[T] value used by both NewColumn and
// NewForeignKey, so a foreign key column starts from identical zero state
// to a plain column.
func newColumn[T any](name string) Column[T] {
	return Column[T]{name: name}
}

// NewColumn declares a column named name with Go value type T.
func NewColumn[T any](name string) *Column[T] {
	c := newColumn[T](name)
	return &c
}

// PrimaryKey marks the column as (part of) the table's primary key.
func (c *Column[T]) PrimaryKey() *Column[T] {
	c.primaryKey = true
	return c
}

// Unique marks the column as having a unique constraint.
func (c *Column[T]) Unique() *Column[T] {
	c.unique = true
	return c
}

// NotNull marks the column as disallowing SQL NULL.
//
// This is independent of T. Nullability of the Go representation comes
// from IsNullable (whether T is a pointer), not from NotNull. Calling
// NotNull on a Column[*T] is allowed here; reconciling the two is left to
// the future DDL-generation phase.
func (c *Column[T]) NotNull() *Column[T] {
	c.notNull = true
	return c
}

// MaxLen sets the column's maximum length.
//
// Go generics cannot add a method to only some instantiations of a type,
// so MaxLen exists on every Column[T] even though it only makes sense for
// string-like T. It is stored as-is here; validating that it was used on
// an applicable T is left to the future DDL-generation phase.
func (c *Column[T]) MaxLen(n int) *Column[T] {
	c.maxLen = n
	c.maxLenSet = true
	return c
}

// Name returns the column's database name.
func (c *Column[T]) Name() string {
	return c.name
}

// IsPrimaryKey reports whether PrimaryKey was called.
func (c *Column[T]) IsPrimaryKey() bool {
	return c.primaryKey
}

// IsUnique reports whether Unique was called.
func (c *Column[T]) IsUnique() bool {
	return c.unique
}

// IsNotNull reports whether NotNull was called.
func (c *Column[T]) IsNotNull() bool {
	return c.notNull
}

// MaxLength returns the value passed to MaxLen and whether MaxLen was ever
// called. The ok return distinguishes "never called" from "explicitly set
// to 0", which a bare int result could not.
func (c *Column[T]) MaxLength() (n int, ok bool) {
	return c.maxLen, c.maxLenSet
}

// GoType returns T's reflect.Type.
func (c *Column[T]) GoType() reflect.Type {
	return reflect.TypeFor[T]()
}

// IsNullable reports whether T is a pointer kind, this package's convention
// for a nullable column (analogous to Optional[T]).
func (c *Column[T]) IsNullable() bool {
	return c.GoType().Kind() == reflect.Pointer
}
