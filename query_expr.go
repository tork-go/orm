package orm

// arithOp is one of the four arithmetic operators Expr renders.
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
// The default is unreachable: unlike Operator, arithOp is unexported, so
// Add/Sub/Mul/Div are the only way to produce one and every value they
// construct is already named above.
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

// Expr is an arithmetic expression usable as the right-hand side of an
// assignment: a column combined with either a value or another column.
//
//	Users.Balance.SetExpr(orm.Sub(Users.Balance, amount))
//	// "balance" = "balance" - $1
//
// Building one touches no database and cannot fail, the same as a
// Predicate: it is pure data until the statement that carries it compiles.
type Expr struct {
	left  ColumnMeta
	op    arithOp
	right any // a literal value, or a ColumnMeta for col-op-col
}

// Add is `left + right`.
func Add(left ColumnMeta, right any) Expr { return Expr{left: left, op: arithAdd, right: right} }

// Sub is `left - right`.
func Sub(left ColumnMeta, right any) Expr { return Expr{left: left, op: arithSub, right: right} }

// Mul is `left * right`.
func Mul(left ColumnMeta, right any) Expr { return Expr{left: left, op: arithMul, right: right} }

// Div is `left / right`.
func Div(left ColumnMeta, right any) Expr { return Expr{left: left, op: arithDiv, right: right} }

// numericAssignable supplies Increment, Decrement and SetExpr to numeric
// columns only.
//
// It is a mixin of its own rather than an addition to assignable, which
// every scalar column type embeds, StringColumn and BoolColumn included:
// putting Increment there would make Users.Username.Increment(1) compile.
type numericAssignable[T any] struct{ c *Column[T] }

// Increment is `col = col + delta`.
func (m numericAssignable[T]) Increment(delta T) Assignment {
	e := Add(m.c, delta)
	return Assignment{Col: m.c, Expr: &e}
}

// Decrement is `col = col - delta`.
func (m numericAssignable[T]) Decrement(delta T) Assignment {
	e := Sub(m.c, delta)
	return Assignment{Col: m.c, Expr: &e}
}

// SetExpr assigns the result of an arbitrary Expr, the escape hatch
// Increment and Decrement are shorthand for.
func (m numericAssignable[T]) SetExpr(e Expr) Assignment {
	return Assignment{Col: m.c, Expr: &e}
}
