package orm

import "reflect"

// Column declares a single typed table column. T is the column's Go value
// type; using a pointer type for T (e.g. Column[*string]) marks the column
// as nullable, mirroring Optional[T] semantics. Construct one with
// NewColumn and configure it with the chainable builder methods below:
//
//	Username := NewColumn[string]("username").Unique().NotNull().MaxLen(30)
type Column[T any] struct {
	name             string
	primaryKey       bool
	unique           bool
	notNull          bool
	maxLen           int
	maxLenSet        bool
	index            bool
	serverDefault    string
	serverDefaultSet bool
	generator        func() T
	numericPrecision int
	numericScale     int
	numericSet       bool
	jsonKindSet      bool
	isJSONB          bool
	marshal          func(T) ([]byte, error)
	unmarshal        func([]byte) (T, error)
	enumTypeName     string
	enumValues       []string
	enumSet          bool
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

// Index marks the column as having a plain (non-unique) index. If the
// column is also Unique, a unique constraint already provides an index in
// every dialect Tork targets, so extraction folds this into that single
// unique constraint rather than a redundant separate index.
func (c *Column[T]) Index() *Column[T] {
	c.index = true
	return c
}

// ServerDefault stores a raw SQL expression the database computes the
// column's value with when no value is given (e.g. "gen_random_uuid()",
// "now()"), rendered as a DEFAULT clause when a migration is generated.
func (c *Column[T]) ServerDefault(expr string) *Column[T] {
	c.serverDefault = expr
	c.serverDefaultSet = true
	return c
}

// GeneratedByClient stores gen as the column's Go-side value generator
// (e.g. uuid.New for a Column[uuid.UUID]). It has no effect on migrations
// or any DDL Tork generates today: there is no INSERT-building code yet.
// It exists so a model can declare "Go computes this value" once, now,
// rather than being revisited once a query-building package adds code
// that actually calls it.
func (c *Column[T]) GeneratedByClient(gen func() T) *Column[T] {
	c.generator = gen
	return c
}

// Numeric sets explicit precision and scale, rendered as NUMERIC(p,s).
// Only meaningful when T resolves to KindNumeric (decimal.Decimal);
// validated at extract time, mirroring MaxLen. Without a call to Numeric,
// a decimal.Decimal column renders as bare NUMERIC (arbitrary precision),
// the same relationship MaxLen has to a bare string column rendering TEXT.
func (c *Column[T]) Numeric(precision, scale int) *Column[T] {
	c.numericPrecision = precision
	c.numericScale = scale
	c.numericSet = true
	return c
}

// JSON marks the column as stored as JSON, using encoding/json.Marshal and
// Unmarshal by default for whatever T is. Chain Serialize to override the
// default marshal/unmarshal pair.
func (c *Column[T]) JSON() *Column[T] {
	c.jsonKindSet = true
	c.isJSONB = false
	return c
}

// JSONB marks the column as stored as JSONB, using encoding/json.Marshal
// and Unmarshal by default for whatever T is. Chain Serialize to override
// the default marshal/unmarshal pair.
func (c *Column[T]) JSONB() *Column[T] {
	c.jsonKindSet = true
	c.isJSONB = true
	return c
}

// Serialize overrides the default encoding/json.Marshal/Unmarshal pair
// used when this column is stored as JSON or JSONB. Calling Serialize
// alone, without JSON or JSONB, implies JSONB, matching Postgres's own
// general recommendation of jsonb over json.
func (c *Column[T]) Serialize(marshal func(T) ([]byte, error), unmarshal func([]byte) (T, error)) *Column[T] {
	c.marshal = marshal
	c.unmarshal = unmarshal
	return c
}

// Enum declares the column as a Postgres native enum of type typeName
// with the given values, in order. T must resolve to a string kind after
// unwrapping pointer nullability; validated at extract time, mirroring
// MaxLen.
func (c *Column[T]) Enum(typeName string, values ...string) *Column[T] {
	c.enumTypeName = typeName
	c.enumValues = values
	c.enumSet = true
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

// IsIndexed reports whether Index was called.
func (c *Column[T]) IsIndexed() bool {
	return c.index
}

// ServerDefaultExpr returns the value passed to ServerDefault and whether
// ServerDefault was ever called, the same (value, ok) shape as MaxLength.
func (c *Column[T]) ServerDefaultExpr() (string, bool) {
	return c.serverDefault, c.serverDefaultSet
}

// IsClientGenerated reports whether GeneratedByClient was called. Unlike
// Generator below, this doesn't mention T, so it can live on the shared
// ColumnMeta interface.
func (c *Column[T]) IsClientGenerated() bool {
	return c.generator != nil
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

// Generator returns the function passed to GeneratedByClient and whether
// one was ever set. It returns func() T, so unlike IsClientGenerated it
// can't join ColumnMeta (T would have to appear there too), and needs no
// ForeignKey override either: like MaxLength and GoType, it returns a
// type other than Self, so plain method promotion already returns the
// right thing.
func (c *Column[T]) Generator() (func() T, bool) {
	return c.generator, c.generator != nil
}

// NumericPrecisionScale returns the values passed to Numeric and whether
// Numeric was ever called, the same (value, ok) shape as MaxLength.
func (c *Column[T]) NumericPrecisionScale() (precision, scale int, ok bool) {
	return c.numericPrecision, c.numericScale, c.numericSet
}

// IsJSON reports whether JSON was called (not JSONB, and not merely
// Serialize alone, which implies JSONB rather than JSON).
func (c *Column[T]) IsJSON() bool {
	return c.jsonKindSet && !c.isJSONB
}

// IsJSONB reports whether JSONB was called, or Serialize was called
// without a preceding JSON call.
func (c *Column[T]) IsJSONB() bool {
	if c.jsonKindSet {
		return c.isJSONB
	}
	return c.IsSerialized()
}

// IsSerialized reports whether Serialize was called.
func (c *Column[T]) IsSerialized() bool {
	return c.marshal != nil || c.unmarshal != nil
}

// Serializer returns the marshal/unmarshal pair passed to Serialize and
// whether Serialize was ever called. Like Generator, this mentions T in
// its return type, so it can't join ColumnMeta and needs no ForeignKey
// override either.
func (c *Column[T]) Serializer() (marshal func(T) ([]byte, error), unmarshal func([]byte) (T, error), ok bool) {
	return c.marshal, c.unmarshal, c.IsSerialized()
}

// EnumSpec returns the values passed to Enum and whether Enum was ever
// called.
func (c *Column[T]) EnumSpec() (typeName string, values []string, ok bool) {
	return c.enumTypeName, c.enumValues, c.enumSet
}
