package orm

import "reflect"

// arithOp is one of the four arithmetic operators an expression renders.
type arithOp int

const (
	arithAdd arithOp = iota
	arithSub
	arithMul
	arithDiv
)

// String returns the SQL spelling of the operator. Every dialect Tork
// targets spells these four identically, so this lives here rather than on
// the dialect, the same reason Operator.String does.
//
// The default is unreachable: arithOp is unexported, so the four builder
// methods are the only way to produce one and every value they construct is
// already named above.
func (o arithOp) String() string {
	switch o {
	case arithAdd:
		return "+"
	case arithSub:
		return "-"
	case arithMul:
		return "*"
	case arithDiv:
		return "/"
	}
	return "?"
}

// exprKind says which shape an exprNode holds.
type exprKind int

const (
	exprColumn exprKind = iota // a column, lifted by Value
	exprArith                  // left <op> right
	exprCase                   // CASE WHEN ... THEN ... ELSE ... END
	exprCall                   // fn(args...), which an aggregate also is
)

// exprNode is one expression, flattened into a non-generic shape.
//
// It is separate from Expr[T] because the compiler has to render an
// expression without knowing its T, and Go cannot write `case Expr[T]:` in
// a type switch: every instantiation is a distinct type, so a switch would
// have to name Expr[int], Expr[float64] and every other in advance. Holding
// the data in a non-generic node, reached through the expression interface
// below, is what lets one renderer handle all of them.
type exprNode struct {
	kind   exprKind
	goType reflect.Type // T, which a bare literal operand is checked against

	col ColumnMeta // exprColumn

	op    arithOp // exprArith
	left  any     // a ColumnMeta, an expression, or a literal
	right any

	whens []caseWhen // exprCase
	els   any

	// exprCall. fn is the function's name, checked as an identifier when the
	// statement compiles, and args its arguments, each a column, an
	// expression or a literal to bind.
	//
	// An aggregate is a call like any other, differing only in the three
	// flags below: agg marks it as one, which is what DISTINCT applies to,
	// star is COUNT(*), the one call whose argument list names nothing, and
	// distinct is COUNT(DISTINCT x).
	fn       string
	args     []any
	agg      bool
	star     bool
	distinct bool

	// over is the OVER clause a window function carries, and nil for a call
	// that is not one. A window function computes a value per row from the
	// rows around it rather than collapsing them, which is the one thing
	// that separates SUM(x) from SUM(x) OVER (...). See query_window.go.
	over *windowSpec
}

// expression is the non-generic view of an Expr[T].
//
// It is sealed to this package by exprNode being unexported, the same way
// Predicate is sealed by its own marker method: an expression is something
// this package built, never something a caller assembled by hand.
type expression interface {
	GoType() reflect.Type
	exprNode() exprNode
}

// Expr is a value computed by the database rather than supplied from Go: a
// column combined with another column, a literal, or another expression.
//
//	Items.Price.Times(Items.Qty)              // "price" * "qty"
//	Items.Price.Times(Items.Qty).Plus(10.0)   // ("price" * "qty") + $1
//
// T is the Go type the result decodes as, taken from the column the chain
// started at, which is what lets an expression be read into a struct field
// by SelectAs and assigned back to a column by SetExpr with the types
// checked rather than assumed.
//
// Building one touches no database and cannot fail, the same as a
// Predicate: it is pure data until the statement carrying it compiles.
type Expr[T any] struct{ n exprNode }

// GoType returns the Go type the expression's result decodes as. It is what
// makes an Expr a SelectExpr, so one can be read by SelectAs with no
// wrapper.
func (e Expr[T]) GoType() reflect.Type { return reflect.TypeFor[T]() }

func (e Expr[T]) exprNode() exprNode { return e.n }

// Plus is `expr + other`, where other is a column, another expression, or a
// literal of T's own type.
func (e Expr[T]) Plus(other any) Expr[T] { return e.arith(arithAdd, other) }

// Minus is `expr - other`.
func (e Expr[T]) Minus(other any) Expr[T] { return e.arith(arithSub, other) }

// Times is `expr * other`.
func (e Expr[T]) Times(other any) Expr[T] { return e.arith(arithMul, other) }

// DividedBy is `expr / other`.
func (e Expr[T]) DividedBy(other any) Expr[T] { return e.arith(arithDiv, other) }

// Equals is `expr = other`, where other is a column, another expression, or
// a literal of T's own type.
//
//	Items.With(db).Where(Items.Price.Value().Equals(Items.Cost))
//	Items.With(db).Where(Items.Price.Times(Items.Qty).Equals(100.0))
//
// The names are the ones a column already carries, so an expression's
// comparisons are nothing new to learn; what differs is only that these
// accept a column or an expression as well as a value.
func (e Expr[T]) Equals(other any) Predicate { return e.compare(OpEquals, other) }

// NotEquals is `expr <> other`.
func (e Expr[T]) NotEquals(other any) Predicate { return e.compare(OpNotEquals, other) }

// GreaterThan is `expr > other`.
func (e Expr[T]) GreaterThan(other any) Predicate { return e.compare(OpGreaterThan, other) }

// GreaterOrEqual is `expr >= other`.
func (e Expr[T]) GreaterOrEqual(other any) Predicate { return e.compare(OpGreaterOrEqual, other) }

// LessThan is `expr < other`.
func (e Expr[T]) LessThan(other any) Predicate { return e.compare(OpLessThan, other) }

// LessOrEqual is `expr <= other`.
func (e Expr[T]) LessOrEqual(other any) Predicate { return e.compare(OpLessOrEqual, other) }

func (e Expr[T]) compare(op Operator, other any) Predicate {
	return exprComparison{left: e, op: op, right: other}
}

// Fn calls a SQL function, which is what everything the database can compute
// and this package has no name of its own for goes through.
//
//	orm.Fn[time.Time]("date_trunc", "month", Orders.CreatedAt)
//	// DATE_TRUNC(CAST($1 AS TEXT), "created_at")
//
//	orm.Fn[string]("concat_ws", ", ", Users.Surname, Users.Forename)
//	orm.Fn[time.Time]("now")
//
// T is the type the result decodes as, given rather than inferred because
// nothing about a function's name says what it returns. It is what lets the
// call be read into a struct field by SelectAs and compared against a value
// of the right type, the same as any other expression.
//
// Arguments are columns, expressions and values, mixed freely. A value is
// bound as a parameter like every other value this package sends, so a
// function's argument is never a way to write SQL: only the name is written
// literally, and it is checked to be an identifier — optionally qualified,
// as pg_catalog.lower — when the statement compiles.
//
// The name is not translated between databases. Where every dialect Tork
// targets spells a function the same way, this package wraps it (see Lower,
// Coalesce and the rest of query_fn.go); where they differ, writing the call
// here is what makes the choice of spelling visible at the call site rather
// than hidden behind a name that cannot mean the same thing everywhere.
func Fn[T any](name string, args ...any) Expr[T] {
	return Expr[T]{n: exprNode{
		kind:   exprCall,
		goType: reflect.TypeFor[T](),
		fn:     name,
		args:   append([]any(nil), args...),
	}}
}

// aggregate builds the call an aggregate is, shared by every constructor in
// query_select_as.go so they differ only in their name and result type.
func aggregate[T any](name string, args ...any) Expr[T] {
	e := Fn[T](name, args...)
	e.n.agg = true
	return e
}

// Distinct restricts an aggregate to the distinct values of its argument.
//
//	orm.CountOf(Books.AuthorID).Distinct()  // COUNT(DISTINCT "author_id")
//
// It belongs to an aggregate and to nothing else — LOWER(DISTINCT x) is not
// a thing SQL has — so an expression that is not one is reported when the
// statement compiles, naming what it was asked of. COUNT(*) is refused for
// the same reason: its argument is every row, which cannot be narrowed to
// the distinct ones; count the column you mean instead.
//
// That the check is not the Go compiler's is the cost of an aggregate being
// an ordinary expression, which is what lets it be compared, ordered by, and
// combined with arithmetic.
func (e Expr[T]) Distinct() Expr[T] {
	out := e
	out.n.distinct = true
	return out
}

// Asc orders by the expression's value, smallest first.
//
//	Items.With(db).OrderBy(Items.Price.Times(Items.Qty).Desc()).All(ctx)
//
// Cursor paging cannot use one: it reads its ordering columns back out of
// a row to seek from, and a computed value has no field to read. See
// Filtered.Cursor.
func (e Expr[T]) Asc() Ordering { return Ordering{expr: e} }

// Desc orders by the expression's value, largest first.
func (e Expr[T]) Desc() Ordering { return Ordering{expr: e, Desc: true} }

// exprComparison is `<expr> <op> <operand>`, what an expression's own
// comparisons build.
//
// It is unexported, unlike Comparison, for the reason rawPredicate is: it
// is only ever produced by the methods above, never assembled by hand, and
// its left side holds the unexported expression interface regardless.
type exprComparison struct {
	left  expression
	op    Operator
	right any
}

func (exprComparison) predicate() {}

func (e Expr[T]) arith(op arithOp, other any) Expr[T] {
	return Expr[T]{n: exprNode{
		kind:   exprArith,
		goType: reflect.TypeFor[T](),
		op:     op,
		left:   e,
		right:  other,
	}}
}

// colExpr lifts a column into an expression over its own type, which is
// what Value does for every column kind and what the numeric mixin's
// arithmetic starts from.
func colExpr[T any](c ColumnMeta) Expr[T] {
	return Expr[T]{n: exprNode{kind: exprColumn, goType: reflect.TypeFor[T](), col: c}}
}

// numericOps supplies arithmetic and the expression-valued assignments to
// numeric columns only.
//
// It is a mixin of its own rather than an addition to assignable, which
// every scalar column type embeds, StringColumn and BoolColumn included:
// putting Times there would make Users.Username.Times(2) compile.
type numericOps[T any] struct{ c *Column[T] }

// Plus is `col + other`, where other is a column, an expression, or a
// literal of the column's own type.
//
//	Items.Price.Plus(Items.Shipping)   // "price" + "shipping"
//	Items.Price.Plus(10.0)             // "price" + $1
func (m numericOps[T]) Plus(other any) Expr[T] { return colExpr[T](m.c).Plus(other) }

// Minus is `col - other`.
func (m numericOps[T]) Minus(other any) Expr[T] { return colExpr[T](m.c).Minus(other) }

// Times is `col * other`.
func (m numericOps[T]) Times(other any) Expr[T] { return colExpr[T](m.c).Times(other) }

// DividedBy is `col / other`.
func (m numericOps[T]) DividedBy(other any) Expr[T] { return colExpr[T](m.c).DividedBy(other) }

// Increment is `col = col + delta`.
//
//	Users.With(db).Where(...).UpdateAll(ctx, Users.LoginCount.Increment(1))
//	// SET "login_count" = ("login_count" + $1)
//
// It never reads the current value, which is what makes it safe under
// concurrent writers where reading, adding in Go and writing back is not:
// two of those can read the same value and both write the same result,
// losing one increment.
func (m numericOps[T]) Increment(delta T) Assignment {
	return Assignment{Col: m.c, Expr: m.Plus(delta)}
}

// Decrement is `col = col - delta`.
func (m numericOps[T]) Decrement(delta T) Assignment {
	return Assignment{Col: m.c, Expr: m.Minus(delta)}
}

// SetExpr assigns the result of an arbitrary expression, the general form
// Increment and Decrement are shorthand for.
//
//	Users.Balance.SetExpr(Users.Balance.Minus(amount))
//
// The expression must yield the column's own type, so an expression over
// one column cannot be assigned to a column of another type by accident.
func (m numericOps[T]) SetExpr(e Expr[T]) Assignment {
	return Assignment{Col: m.c, Expr: e}
}
