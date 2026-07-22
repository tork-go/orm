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

// Equals is `col = v`.
func (m equatable[T]) Equals(v T) Predicate { return Comparison{Col: m.c, Op: OpEquals, Value: v} }

// NotEquals is `col <> v`.
func (m equatable[T]) NotEquals(v T) Predicate {
	return Comparison{Col: m.c, Op: OpNotEquals, Value: v}
}

// In is `col IN (vs...)`.
func (m equatable[T]) In(vs ...T) Predicate { return InList{Col: m.c, Values: anySlice(vs)} }

// NotIn is `col NOT IN (vs...)`.
func (m equatable[T]) NotIn(vs ...T) Predicate {
	return InList{Col: m.c, Values: anySlice(vs), Not: true}
}

// InQuery is `col IN (SELECT ...)`, matching rows whose value another query
// also yields.
//
//	authors := orm.Select(Posts.With(db).Where(Posts.Published.Equals(true)), Posts.AuthorID)
//	Users.With(db).Where(Users.ID.InQuery(authors)).All(ctx)
//
// The subquery is embedded rather than run, so the database does the matching
// in one statement instead of the caller shipping a list of keys back to it.
// That is also what makes it the right shape for a set too large to bind: an
// IN list is limited by the dialect's parameter ceiling, and this is not.
func (m equatable[T]) InQuery(sub SubqueryOf[T]) Predicate {
	return InSubquery{Col: m.c, Sub: subqueryOf(sub)}
}

// NotInQuery is `col NOT IN (SELECT ...)`.
//
//	Users.With(db).Where(Users.ID.NotInQuery(authors)).All(ctx)
//
// NOT IN is never true once the subquery yields a NULL, which is SQL's sharpest
// edge: the outer query returns nothing at all and looks like it simply matched
// nothing. A nullable column cannot reach here to cause it, since selecting one
// gives a SubqueryOf[*T] and this takes a SubqueryOf[T]; orm.NonNull converts,
// and excludes the NULLs while it does.
func (m equatable[T]) NotInQuery(sub SubqueryOf[T]) Predicate {
	return InSubquery{Col: m.c, Sub: subqueryOf(sub), Not: true}
}

// subqueryOf unwraps a subquery for storage in a predicate.
//
// A nil one is kept nil rather than boxed into a non-nil interface holding
// nothing, so the compiler reports it by name instead of the caller seeing a
// panic from inside a render.
func subqueryOf[T any](sub SubqueryOf[T]) subquerySource {
	if sub == nil {
		return nil
	}
	return sub
}

// ordered supplies the inequalities, for kinds with a meaningful ordering.
type ordered[T any] struct{ c *Column[T] }

// GreaterThan is `col > v`.
func (m ordered[T]) GreaterThan(v T) Predicate {
	return Comparison{Col: m.c, Op: OpGreaterThan, Value: v}
}

// GreaterOrEqual is `col >= v`.
func (m ordered[T]) GreaterOrEqual(v T) Predicate {
	return Comparison{Col: m.c, Op: OpGreaterOrEqual, Value: v}
}

// LessThan is `col < v`.
func (m ordered[T]) LessThan(v T) Predicate { return Comparison{Col: m.c, Op: OpLessThan, Value: v} }

// LessOrEqual is `col <= v`.
func (m ordered[T]) LessOrEqual(v T) Predicate {
	return Comparison{Col: m.c, Op: OpLessOrEqual, Value: v}
}

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

// Matches is a full-text search: rows whose text matches the query.
//
// The query is parsed leniently — quoted "phrases", -exclusions and or are
// understood, and malformed input matches nothing rather than erroring — so it
// is safe to pass a search box's contents straight in. Unlike the other text
// operations this one has no portable spelling, so the dialect writes it and a
// database without full-text search returns an error naming the operation.
func (m textOps) Matches(query string) Predicate {
	return FullText{Col: m.c, Query: query}
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
// Users.Email.Equals("alice@example.com") rather than
// Users.Email.Equals(ptr("alice@example.com")). Comparing against a value is
// overwhelmingly the common case, and needing a pointer helper to express
// it is a papercut on every single call. The *T form is still available as
// EqualsPtr for when the caller genuinely holds a pointer.
type nullEquatable[T any] struct{ c *Column[*T] }

// Equals is `col = v`.
func (m nullEquatable[T]) Equals(v T) Predicate { return Comparison{Col: m.c, Op: OpEquals, Value: v} }

// NotEquals is `col <> v`.
func (m nullEquatable[T]) NotEquals(v T) Predicate {
	return Comparison{Col: m.c, Op: OpNotEquals, Value: v}
}

// In is `col IN (vs...)`.
func (m nullEquatable[T]) In(vs ...T) Predicate { return InList{Col: m.c, Values: anySlice(vs)} }

// NotIn is `col NOT IN (vs...)`.
func (m nullEquatable[T]) NotIn(vs ...T) Predicate {
	return InList{Col: m.c, Values: anySlice(vs), Not: true}
}

// InQuery is `col IN (SELECT ...)`. See equatable.InQuery.
//
// It takes a subquery of T rather than of *T, for the reason Equals takes
// a T: the values being matched against are values, and a NULL among them
// would match nothing anyway.
func (m nullEquatable[T]) InQuery(sub SubqueryOf[T]) Predicate {
	return InSubquery{Col: m.c, Sub: subqueryOf(sub)}
}

// NotInQuery is `col NOT IN (SELECT ...)`. See equatable.NotInQuery.
//
// A row whose own value is NULL matches neither this nor InQuery, which is
// ordinary SQL and what IsNull is for.
func (m nullEquatable[T]) NotInQuery(sub SubqueryOf[T]) Predicate {
	return InSubquery{Col: m.c, Sub: subqueryOf(sub), Not: true}
}

// EqualsPtr is `col = *v`, or `col IS NULL` when v is nil.
//
// The nil case is redirected deliberately. `col = NULL` is never true in
// SQL, so compiling it literally would turn a nil pointer into a query
// that silently matches nothing. Redirecting to IS NULL matches what the
// caller meant and follows SQLAlchemy's precedent for comparing a column
// against None. Use IsNull directly when the intent is unconditional.
func (m nullEquatable[T]) EqualsPtr(v *T) Predicate {
	if v == nil {
		return Nullness{Col: m.c}
	}
	return Comparison{Col: m.c, Op: OpEquals, Value: *v}
}

// nullOrdered is ordered for a nullable column, taking T rather than *T
// for the same reason nullEquatable does.
type nullOrdered[T any] struct{ c *Column[*T] }

// GreaterThan is `col > v`.
func (m nullOrdered[T]) GreaterThan(v T) Predicate {
	return Comparison{Col: m.c, Op: OpGreaterThan, Value: v}
}

// GreaterOrEqual is `col >= v`.
func (m nullOrdered[T]) GreaterOrEqual(v T) Predicate {
	return Comparison{Col: m.c, Op: OpGreaterOrEqual, Value: v}
}

// LessThan is `col < v`.
func (m nullOrdered[T]) LessThan(v T) Predicate {
	return Comparison{Col: m.c, Op: OpLessThan, Value: v}
}

// LessOrEqual is `col <= v`.
func (m nullOrdered[T]) LessOrEqual(v T) Predicate {
	return Comparison{Col: m.c, Op: OpLessOrEqual, Value: v}
}

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
