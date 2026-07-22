package orm

// The mixins here carry the query operations a JSON column offers, kept out
// of column_mixin_ops.go because they are the only ones a document column
// has and none of the scalar columns do. A JSON column embeds one of them,
// and that is what makes Users.Prefs.HasKey("theme") compile while
// Users.Age.HasKey("theme") does not.
//
// There are two, the plain and the nullable, for the same reason equatable
// and nullEquatable are two: Contains takes the document's Go value, and the
// nullable column stores a *T where the plain one stores a T, so the value a
// caller passes has to be encoded through the codec each column actually has.
// HasKey and Key do not depend on the value at all and read identically in
// both, which is the same duplication the equatable pair already accepts.

// jsonOps supplies the JSON query operations to a non-nullable document
// column.
type jsonOps[T any] struct{ c *Column[T] }

// HasKey is "the document has this top-level key".
func (m jsonOps[T]) HasKey(key string) Predicate { return JSONHasKey{Col: m.c, Key: key} }

// Contains is "the document contains v, as a subtree". The value is encoded
// through the column's own codec, exactly as a stored document is, so a custom
// Serialize pair applies here too.
func (m jsonOps[T]) Contains(v T) Predicate { return JSONContains{Col: m.c, Value: v} }

// Key names a top-level key, whose text can then be compared with Eq or NotEq.
func (m jsonOps[T]) Key(key string) jsonPath { return jsonPath{c: m.c, key: key} }

// nullJSONOps is jsonOps for a nullable document column.
//
// Contains takes the underlying T, not *T, matching how nullEquatable takes T
// rather than a pointer: containment asks about a value, and the pointer is an
// implementation detail of the column, not of the question. It takes the
// address before storing it so the value reaches the column's *T codec as the
// *T that codec expects.
type nullJSONOps[T any] struct{ c *Column[*T] }

// HasKey is "the document has this top-level key".
func (m nullJSONOps[T]) HasKey(key string) Predicate { return JSONHasKey{Col: m.c, Key: key} }

// Contains is "the document contains v, as a subtree".
func (m nullJSONOps[T]) Contains(v T) Predicate { return JSONContains{Col: m.c, Value: &v} }

// Key names a top-level key, whose text can then be compared with Eq or NotEq.
func (m nullJSONOps[T]) Key(key string) jsonPath { return jsonPath{c: m.c, key: key} }

// jsonPath is a top-level key of a JSON column, extracted as text, waiting for
// the value to compare it against.
//
// It is what Key returns, so Users.Prefs.Key("theme").Eq("dark") reads as one
// thought. The comparison is on text because ->> yields text; a number or a
// nested value is what Contains and orm.Raw are for.
type jsonPath struct {
	c   ColumnMeta
	key string
}

// Eq is `(col ->> key) = v`.
func (p jsonPath) Eq(v string) Predicate {
	return JSONKey{Col: p.c, Key: p.key, Op: OpEq, Value: v}
}

// NotEq is `(col ->> key) <> v`.
func (p jsonPath) NotEq(v string) Predicate {
	return JSONKey{Col: p.c, Key: p.key, Op: OpNotEq, Value: v}
}
