package orm

import "reflect"

// caseWhen is one arm of a CASE: the condition that selects it and the
// value it yields.
type caseWhen struct {
	cond Predicate
	then any
}

// Case begins a conditional expression, whose result is a T.
//
//	orm.Case[int]().
//	    When(Users.Active.Equals(true), 1).
//	    Else(0)
//	// CASE WHEN "active" = $1 THEN $2 ELSE $3 END
//
// It is a function rather than a method because it introduces T, which a Go
// method cannot; everything after it is a method, so the arms chain.
//
// T is named rather than inferred because nothing in a Case fixes it: the
// arms are values, and an untyped constant like 1 would infer int where the
// column being compared against is a float. Naming it once at the front is
// what makes the arms and the eventual destination agree.
//
// The result is an ordinary expression, so a Case goes wherever one goes: a
// SELECT list, a WHERE, an ORDER BY, or the right of an assignment.
//
//	// a label computed per row
//	orm.SelectAs[Row](Users.With(db), Users.Name,
//	    orm.Case[string]().When(Users.Age.LessThan(18), "minor").Else("adult"))
//
//	// a sort that puts one group first
//	Users.With(db).OrderBy(
//	    orm.Case[int]().When(Users.Role.Equals("admin"), 0).Else(1).Asc())
//
// Aggregating one is what SumOfExpr and its siblings are for, since SQL has
// no COUNT with a condition and writes a conditional tally as a SUM over a
// CASE:
//
//	orm.SumOfExpr(orm.Case[int]().When(cond, 1).Else(0))
func Case[T any]() CaseBuilder[T] { return CaseBuilder[T]{} }

// CaseBuilder collects the arms of a Case until Else closes it.
type CaseBuilder[T any] struct{ whens []caseWhen }

// When adds an arm: when cond holds, the expression yields then, which may
// be a column, another expression, or a literal of T's own type.
//
// Arms accumulate in the order given, and the first whose condition holds
// is the one that decides the value, which is what CASE means in SQL.
func (b CaseBuilder[T]) When(cond Predicate, then any) CaseBuilder[T] {
	out := b
	out.whens = append(append([]caseWhen(nil), b.whens...), caseWhen{cond: cond, then: then})
	return out
}

// Else closes the expression with the value used when no arm matched, and
// hands back the expression itself.
//
// It is required rather than optional, and that is the point: a CASE with
// no ELSE yields NULL when nothing matches, which cannot be scanned into a
// T that is not a pointer. Demanding an Else means T never has to stand for
// absence, and the builder only becomes an expression once its value is
// total. Where NULL really is the answer, name a pointer type — Case[*int]
// — and pass nil.
func (b CaseBuilder[T]) Else(v any) Expr[T] {
	return Expr[T]{n: exprNode{
		kind:   exprCase,
		goType: reflect.TypeFor[T](),
		whens:  b.whens,
		els:    v,
	}}
}
