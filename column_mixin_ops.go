package orm

// The mixins here carry the query operations a column offers. A concrete
// column type embeds the ones its kind supports, and that composition is
// the whole type-safety story: IntColumn does not embed textOps, so
// Users.ID.Contains("1") is a compile error rather than a runtime
// surprise, and StringColumn does not embed nullness, so
// Users.Username.IsNull() is one too.
//
// Each mixin holds its column in a named field rather than embedding it,
// so chain stays the single promotion path to ColumnMeta and no column
// type ever has an ambiguous one. An ambiguous promotion would be worse
// than a compile error here: Go drops the ambiguous method from the type's
// method set, so the column would quietly stop satisfying ColumnMeta and
// walkFields would skip it with no diagnostic at all. The tests assert
// every column type is still discovered by orm.Columns for exactly that
// reason.
//
// Mixins that need only the column's identity (textOps, sortable,
// nullness) hold a ColumnMeta, so one mixin serves both the plain and the
// nullable form of a kind even though their T differs. Mixins that accept
// a value hold the typed *Column[T], because that is what fixes the
// argument type callers see.

// equatable supplies equality and set membership.
type equatable[T any] struct{ c *Column[T] }

// Eq is `col = v`.
func (m equatable[T]) Eq(v T) Predicate { return Comparison{Col: m.c, Op: OpEq, Value: v} }

// NotEq is `col <> v`.
func (m equatable[T]) NotEq(v T) Predicate { return Comparison{Col: m.c, Op: OpNotEq, Value: v} }

// In is `col IN (vs...)`.
func (m equatable[T]) In(vs ...T) Predicate { return InList{Col: m.c, Values: anySlice(vs)} }

// NotIn is `col NOT IN (vs...)`.
func (m equatable[T]) NotIn(vs ...T) Predicate {
	return InList{Col: m.c, Values: anySlice(vs), Not: true}
}

// ordered supplies the inequalities, for kinds with a meaningful ordering.
type ordered[T any] struct{ c *Column[T] }

// Gt is `col > v`.
func (m ordered[T]) Gt(v T) Predicate { return Comparison{Col: m.c, Op: OpGt, Value: v} }

// Gte is `col >= v`.
func (m ordered[T]) Gte(v T) Predicate { return Comparison{Col: m.c, Op: OpGte, Value: v} }

// Lt is `col < v`.
func (m ordered[T]) Lt(v T) Predicate { return Comparison{Col: m.c, Op: OpLt, Value: v} }

// Lte is `col <= v`.
func (m ordered[T]) Lte(v T) Predicate { return Comparison{Col: m.c, Op: OpLte, Value: v} }

// Between is `col BETWEEN lo AND hi`, inclusive at both ends.
func (m ordered[T]) Between(lo, hi T) Predicate { return Range{Col: m.c, Lo: lo, Hi: hi} }

// textOps supplies pattern matching. It holds a ColumnMeta so the same
// mixin serves StringColumn (Column[string]) and NullableStringColumn
// (Column[*string]).
type textOps struct{ c ColumnMeta }

// Like is `col LIKE pattern`. The pattern is passed through untouched, so
// % and _ keep their wildcard meaning; use Contains, StartsWith, or
// EndsWith to match a substring literally.
func (m textOps) Like(pattern string) Predicate {
	return Pattern{Col: m.c, Value: pattern}
}

// ILike is Like, case-insensitively.
func (m textOps) ILike(pattern string) Predicate {
	return Pattern{Col: m.c, Value: pattern, CaseInsensitive: true}
}

// Contains matches rows whose value contains s literally. Wildcards in s
// are escaped, so Contains("50%") matches the text "50%" rather than
// anything starting "50".
func (m textOps) Contains(s string) Predicate {
	return Pattern{Col: m.c, Value: "%" + escapeLike(s) + "%"}
}

// StartsWith matches rows whose value begins with s literally.
func (m textOps) StartsWith(s string) Predicate {
	return Pattern{Col: m.c, Value: escapeLike(s) + "%"}
}

// EndsWith matches rows whose value ends with s literally.
func (m textOps) EndsWith(s string) Predicate {
	return Pattern{Col: m.c, Value: "%" + escapeLike(s)}
}

// sortable supplies ORDER BY terms. Every column kind is sortable, so
// every column type embeds this, including the nullable ones.
type sortable struct{ c ColumnMeta }

// Asc orders by this column ascending.
func (m sortable) Asc() Ordering { return Ordering{Col: m.c} }

// Desc orders by this column descending.
func (m sortable) Desc() Ordering { return Ordering{Col: m.c, Desc: true} }

// assignable supplies the UPDATE SET term for a non-nullable column.
type assignable[T any] struct{ c *Column[T] }

// Set assigns v to this column.
func (m assignable[T]) Set(v T) Assignment { return Assignment{Col: m.c, Value: v} }

// nullness supplies the NULL tests. Only nullable column types embed it,
// which is what makes Users.Username.IsNull() a compile error.
type nullness struct{ c ColumnMeta }

// IsNull is `col IS NULL`.
func (m nullness) IsNull() Predicate { return Nullness{Col: m.c} }

// IsNotNull is `col IS NOT NULL`.
func (m nullness) IsNotNull() Predicate { return Nullness{Col: m.c, Not: true} }

// nullEquatable is equatable for a nullable column.
//
// Its methods take the underlying T, not *T, so a caller writes
// Users.Email.Eq("alice@example.com") rather than
// Users.Email.Eq(ptr("alice@example.com")). Comparing against a value is
// overwhelmingly the common case, and needing a pointer helper to express
// it is a papercut on every single call. The *T form is still available as
// EqPtr for when the caller genuinely holds a pointer.
type nullEquatable[T any] struct{ c *Column[*T] }

// Eq is `col = v`.
func (m nullEquatable[T]) Eq(v T) Predicate { return Comparison{Col: m.c, Op: OpEq, Value: v} }

// NotEq is `col <> v`.
func (m nullEquatable[T]) NotEq(v T) Predicate { return Comparison{Col: m.c, Op: OpNotEq, Value: v} }

// In is `col IN (vs...)`.
func (m nullEquatable[T]) In(vs ...T) Predicate { return InList{Col: m.c, Values: anySlice(vs)} }

// NotIn is `col NOT IN (vs...)`.
func (m nullEquatable[T]) NotIn(vs ...T) Predicate {
	return InList{Col: m.c, Values: anySlice(vs), Not: true}
}

// EqPtr is `col = *v`, or `col IS NULL` when v is nil.
//
// The nil case is redirected deliberately. `col = NULL` is never true in
// SQL, so compiling it literally would turn a nil pointer into a query
// that silently matches nothing. Redirecting to IS NULL matches what the
// caller meant and follows SQLAlchemy's precedent for comparing a column
// against None. Use IsNull directly when the intent is unconditional.
func (m nullEquatable[T]) EqPtr(v *T) Predicate {
	if v == nil {
		return Nullness{Col: m.c}
	}
	return Comparison{Col: m.c, Op: OpEq, Value: *v}
}

// nullOrdered is ordered for a nullable column, taking T rather than *T
// for the same reason nullEquatable does.
type nullOrdered[T any] struct{ c *Column[*T] }

// Gt is `col > v`.
func (m nullOrdered[T]) Gt(v T) Predicate { return Comparison{Col: m.c, Op: OpGt, Value: v} }

// Gte is `col >= v`.
func (m nullOrdered[T]) Gte(v T) Predicate { return Comparison{Col: m.c, Op: OpGte, Value: v} }

// Lt is `col < v`.
func (m nullOrdered[T]) Lt(v T) Predicate { return Comparison{Col: m.c, Op: OpLt, Value: v} }

// Lte is `col <= v`.
func (m nullOrdered[T]) Lte(v T) Predicate { return Comparison{Col: m.c, Op: OpLte, Value: v} }

// Between is `col BETWEEN lo AND hi`, inclusive at both ends.
func (m nullOrdered[T]) Between(lo, hi T) Predicate { return Range{Col: m.c, Lo: lo, Hi: hi} }

// nullAssignable supplies the UPDATE SET terms for a nullable column.
type nullAssignable[T any] struct{ c *Column[*T] }

// Set assigns v to this column.
func (m nullAssignable[T]) Set(v T) Assignment { return Assignment{Col: m.c, Value: v} }

// SetPtr assigns *v, or SQL NULL when v is nil.
func (m nullAssignable[T]) SetPtr(v *T) Assignment {
	if v == nil {
		return Assignment{Col: m.c, Value: nil}
	}
	return Assignment{Col: m.c, Value: *v}
}

// SetNull assigns SQL NULL to this column.
func (m nullAssignable[T]) SetNull() Assignment { return Assignment{Col: m.c, Value: nil} }

// anySlice widens a typed slice for storage in a predicate, whose fields
// are untyped so one predicate shape serves every column kind.
func anySlice[T any](vs []T) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = v
	}
	return out
}
