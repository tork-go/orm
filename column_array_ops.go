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
// It is what Len returns, so Tags.Len().Gt(3) reads as one thought. The count
// is of elements, zero for an empty array, so Len().Eq(0) means "empty".
type arrayLength struct{ c ColumnMeta }

// Eq is `len(col) = n`.
func (l arrayLength) Eq(n int) Predicate { return ArrayLength{Col: l.c, Op: OpEq, Value: n} }

// NotEq is `len(col) <> n`.
func (l arrayLength) NotEq(n int) Predicate { return ArrayLength{Col: l.c, Op: OpNotEq, Value: n} }

// Gt is `len(col) > n`.
func (l arrayLength) Gt(n int) Predicate { return ArrayLength{Col: l.c, Op: OpGt, Value: n} }

// Gte is `len(col) >= n`.
func (l arrayLength) Gte(n int) Predicate { return ArrayLength{Col: l.c, Op: OpGte, Value: n} }

// Lt is `len(col) < n`.
func (l arrayLength) Lt(n int) Predicate { return ArrayLength{Col: l.c, Op: OpLt, Value: n} }

// Lte is `len(col) <= n`.
func (l arrayLength) Lte(n int) Predicate { return ArrayLength{Col: l.c, Op: OpLte, Value: n} }
