package orm

// The mixin here carries the query operations an array column offers beyond
// whole-array equality: membership (Has, HasAll, HasAny) and length. An array
// column embeds it with its element type, so Tags.Has("go") takes a string
// where Nums.Has(3) takes an int, and Age.Has(...) does not compile at all.
//
// It holds a ColumnMeta rather than a typed column, like textOps does: none
// of its predicates carry the array itself, only its identity and the element
// values, and the element type is fixed by the mixin's own parameter. That is
// what lets one mixin type serve both the plain and the nullable form of each
// array kind.

// arrayOps supplies the membership and length tests to an array column whose
// elements are of type Elem.
type arrayOps[Elem any] struct{ c ColumnMeta }

// Has is "the array holds this element".
func (m arrayOps[Elem]) Has(v Elem) Predicate {
	return ArrayContains{Col: m.c, Elems: []Elem{v}}
}

// HasAll is "the array holds every one of these". With no arguments it is true
// of every array, since every array holds all of nothing.
func (m arrayOps[Elem]) HasAll(vs ...Elem) Predicate {
	return ArrayContains{Col: m.c, Elems: vs}
}

// HasAny is "the array holds any one of these". With no arguments it matches
// nothing, since no array overlaps the empty set.
func (m arrayOps[Elem]) HasAny(vs ...Elem) Predicate {
	return ArrayOverlaps{Col: m.c, Elems: vs}
}

// Len names the array's element count, which the comparisons then test.
func (m arrayOps[Elem]) Len() arrayLength { return arrayLength{c: m.c} }

// arrayLength is an array column's element count, waiting for the number to
// compare it against.
//
// It is what Len returns, so Tags.Len().GreaterThan(3) reads as one
// thought. The count is of elements, zero for an empty array, so
// Len().Equals(0) means "empty".
type arrayLength struct{ c ColumnMeta }

// Equals is `len(col) = n`.
func (l arrayLength) Equals(n int) Predicate { return ArrayLength{Col: l.c, Op: OpEquals, Value: n} }

// NotEquals is `len(col) <> n`.
func (l arrayLength) NotEquals(n int) Predicate {
	return ArrayLength{Col: l.c, Op: OpNotEquals, Value: n}
}

// GreaterThan is `len(col) > n`.
func (l arrayLength) GreaterThan(n int) Predicate {
	return ArrayLength{Col: l.c, Op: OpGreaterThan, Value: n}
}

// GreaterOrEqual is `len(col) >= n`.
func (l arrayLength) GreaterOrEqual(n int) Predicate {
	return ArrayLength{Col: l.c, Op: OpGreaterOrEqual, Value: n}
}

// LessThan is `len(col) < n`.
func (l arrayLength) LessThan(n int) Predicate {
	return ArrayLength{Col: l.c, Op: OpLessThan, Value: n}
}

// LessOrEqual is `len(col) <= n`.
func (l arrayLength) LessOrEqual(n int) Predicate {
	return ArrayLength{Col: l.c, Op: OpLessOrEqual, Value: n}
}
