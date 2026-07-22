package orm

// JSONColumn is a non-nullable JSON document column holding a T.
//
// It is constructed as JSONB, matching Postgres's own recommendation of
// jsonb over json; chain JSON() to store it as json instead. T is whatever
// Go value the document marshals from, so it is usually a struct or a map,
// and it needs no entry in the Go-type-to-kind table: schema extraction
// checks for a JSON kind before it consults that table.
//
// It offers no equality or ordering. Postgres's json type has no equality
// operator at all, so `json_col = json_col` is an error rather than a false,
// and a JSONColumn may be either kind. Exposing Equals would hand out a method
// that fails at query time for half its instantiations.
//
// What it does offer is the JSON tests jsonOps carries: HasKey, Contains, and
// Key(...).Equals. These are the operations every JSON storing database can
// express in some spelling, so the dialect writes each one and a driver that
// cannot returns an error naming it.
type JSONColumn[T any] struct {
	chain[T, *JSONColumn[T]]
	jsonBuilder[T, *JSONColumn[T]]
	jsonOps[T]
	assignable[T]
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*JSONColumn[struct{}])(nil)

// NewJSONColumn declares a non-nullable JSON document column named name,
// stored as JSONB. Chain JSON() to store it as json, or Serialize to
// replace the default encoding/json marshal and unmarshal pair.
func NewJSONColumn[T any](name string) *JSONColumn[T] {
	x := &JSONColumn[T]{}
	x.chain = newChain[T](name, x)
	c := x.chain.c
	c.JSONB()
	x.jsonBuilder = jsonBuilder[T, *JSONColumn[T]]{c: c, self: x}
	x.jsonOps = jsonOps[T]{c: c}
	x.assignable = assignable[T]{c: c}
	return x
}

// NullableJSONColumn is a nullable JSON document column holding a T.
type NullableJSONColumn[T any] struct {
	chain[*T, *NullableJSONColumn[T]]
	jsonBuilder[*T, *NullableJSONColumn[T]]
	nullJSONOps[T]
	nullAssignable[T]
	nullness
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*NullableJSONColumn[struct{}])(nil)

// NewNullableJSONColumn declares a nullable JSON document column named
// name, stored as JSONB.
func NewNullableJSONColumn[T any](name string) *NullableJSONColumn[T] {
	x := &NullableJSONColumn[T]{}
	x.chain = newChain[*T](name, x)
	c := x.chain.c
	c.JSONB()
	x.jsonBuilder = jsonBuilder[*T, *NullableJSONColumn[T]]{c: c, self: x}
	x.nullJSONOps = nullJSONOps[T]{c: c}
	x.nullAssignable = nullAssignable[T]{c: c}
	x.nullness = nullness{c: c}
	return x
}

// EnumColumn is a non-nullable native enum column, backed by a string.
//
// It is separate from StringColumn rather than a mode of it, which is what
// makes MaxLen and Enum mutually exclusive at compile time: an enum column
// with a length is rejected during extraction, and a type that offers only
// one of the two builders cannot get into that state. For the same reason
// it omits textOps. LIKE against an enum needs an explicit cast to text in
// Postgres, so offering Contains here would generate SQL that does not
// run.
//
// Values are ordered by their declaration order, which is what Asc and
// Desc sort by.
type EnumColumn struct {
	chain[string, *EnumColumn]
	enumBuilder[string, *EnumColumn]
	equatable[string]
	assignable[string]
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*EnumColumn)(nil)

// NewEnumColumn declares a non-nullable enum column named name of the
// database enum type typeName, with the given values in order.
func NewEnumColumn(name, typeName string, values ...string) *EnumColumn {
	x := &EnumColumn{}
	x.chain = newChain[string](name, x)
	c := x.chain.c
	c.Enum(typeName, values...)
	x.enumBuilder = enumBuilder[string, *EnumColumn]{c: c, self: x}
	x.equatable = equatable[string]{c: c}
	x.assignable = assignable[string]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableEnumColumn is a nullable native enum column.
type NullableEnumColumn struct {
	chain[*string, *NullableEnumColumn]
	enumBuilder[*string, *NullableEnumColumn]
	nullEquatable[string]
	nullAssignable[string]
	nullness
	sortable
}

var _ interface {
	ColumnMeta
	ValueCodec
} = (*NullableEnumColumn)(nil)

// NewNullableEnumColumn declares a nullable enum column named name of the
// database enum type typeName, with the given values in order.
func NewNullableEnumColumn(name, typeName string, values ...string) *NullableEnumColumn {
	x := &NullableEnumColumn{}
	x.chain = newChain[*string](name, x)
	c := x.chain.c
	c.Enum(typeName, values...)
	x.enumBuilder = enumBuilder[*string, *NullableEnumColumn]{c: c, self: x}
	x.nullEquatable = nullEquatable[string]{c: c}
	x.nullAssignable = nullAssignable[string]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}
