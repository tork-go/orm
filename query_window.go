package orm

import "strconv"

// A window function computes a value for each row from the rows around it,
// rather than collapsing them the way an aggregate does. SUM(x) turns a
// group into one number; SUM(x) OVER (ORDER BY d) leaves every row where it
// was and gives each one the running total up to it.
//
// The two are the same call with an OVER clause between them, which is why
// every aggregate here can be windowed:
//
//	orm.SumOf(Sales.Total).Over().OrderBy(Sales.Day.Asc())
//	// SUM("total") OVER (ORDER BY "day" ASC)
//
// The ranking functions below exist only as windows, so they carry an empty
// OVER clause from the start and need no call to Over.
//
// Filtering on a window function's own result — "the top three per group" —
// is written by wrapping the projection as a derived table, since no
// statement can name a window function in its own WHERE. See DefineDerived.

// windowSpec is an OVER clause: which rows share a window, in what order
// they are counted, and how much of the window each row sees.
type windowSpec struct {
	partition []ColumnMeta
	order     []Ordering
	frame     *frameSpec
}

// clone copies the spec so a builder method can extend the copy, for the
// reason Filtered.clone gives: an expression is a value, and holding one to
// branch from must not let one branch see the other's clauses.
func (w *windowSpec) clone() *windowSpec {
	if w == nil {
		return &windowSpec{}
	}
	out := *w
	out.partition = append([]ColumnMeta(nil), w.partition...)
	out.order = append([]Ordering(nil), w.order...)
	return &out
}

// RowNumber numbers each row within its window, starting at 1, with no
// ties: two rows sorted equal by OrderBy still get distinct numbers, in
// whatever order the database happens to visit them in.
func RowNumber() Expr[int64] { return windowFn[int64]("ROW_NUMBER") }

// Rank numbers each row within its window, starting at 1, with ties: rows
// sorted equal by OrderBy share a rank, and the rank after a tie skips ahead
// by the number of tied rows.
func Rank() Expr[int64] { return windowFn[int64]("RANK") }

// DenseRank is Rank without the skip: the rank after a tie is only one more
// than the tie's own rank.
func DenseRank() Expr[int64] { return windowFn[int64]("DENSE_RANK") }

// NTile splits each window into n groups of as equal a size as the rows
// allow, and reports which group a row falls in, counting from 1.
//
//	orm.NTile(4).OrderBy(Users.Age.Asc())  // NTILE($1) OVER (ORDER BY "age" ASC)
//
// Four is quartiles, a hundred is percentiles. The rows have to be ordered
// for the answer to mean anything: without an ORDER BY the database splits
// them in whatever order it visited them.
func NTile(n int) Expr[int64] { return windowFn[int64]("NTILE", n) }

// Lag reads the column's value from the row before this one in the window.
//
//	orm.Lag(Sales.Total).OrderBy(Sales.Day.Asc())
//	// LAG("total") OVER (ORDER BY "day" ASC)
//
// The first row of each window has no row before it, so the result is NULL
// there — which is why it is typed as a pointer rather than as the column's
// own type. Pair it with Coalesce where a zero reads better than a nil:
//
//	orm.Coalesce[int](orm.Lag(Sales.Total).OrderBy(Sales.Day.Asc()), 0)
//
// Reaching further back than one row is written with Fn, which takes the
// offset LAG's second argument is: orm.Fn[*int]("lag", Sales.Total, 2).
func Lag[T any](col Ref[T]) Expr[*T] { return windowFn[*T]("LAG", col) }

// Lead is Lag in the other direction: the value from the row after this one,
// and NULL at the end of each window.
func Lead[T any](col Ref[T]) Expr[*T] { return windowFn[*T]("LEAD", col) }

// FirstValue reads the column's value from the first row of the window, as
// the window's own ordering and frame define first.
//
// It is not typed as a pointer the way Lag is: a window always holds at
// least the row being computed, so there is always a first row to read.
func FirstValue[T any](col Ref[T]) Expr[T] { return windowFn[T]("FIRST_VALUE", col) }

// LastValue reads the column's value from the last row of the window.
//
// Its default frame ends at the current row, so without a frame it reads the
// current row rather than the window's final one — SQL's own rule, and
// rarely what a caller means. Say what you mean with a frame:
//
//	orm.LastValue(Sales.Total).
//	    OrderBy(Sales.Day.Asc()).
//	    Rows(orm.UnboundedPreceding(), orm.UnboundedFollowing())
func LastValue[T any](col Ref[T]) Expr[T] { return windowFn[T]("LAST_VALUE", col) }

// windowFn builds a call that is a window function from the start, which the
// ranking functions are: RANK with no OVER clause is not a thing SQL has.
func windowFn[T any](name string, args ...any) Expr[T] {
	e := Fn[T](name, args...)
	e.n.over = &windowSpec{}
	return e
}

// Over turns an aggregate into a window function, so it computes a value for
// every row instead of collapsing them into one.
//
//	orm.SumOf(Sales.Total).Over().OrderBy(Sales.Day.Asc())
//	// SUM("total") OVER (ORDER BY "day" ASC) — a running total
//
//	orm.AvgOf(Sales.Total).Over().PartitionBy(Sales.Region)
//	// AVG("total") OVER (PARTITION BY "region") — each row beside its region's mean
//
// PartitionBy, OrderBy, Rows and Range all imply a window, so Over is only
// needed when none of them follows — an aggregate over the whole result,
// which is the shape that puts a grand total beside every row.
//
// Only a call can be windowed. An OVER clause on arithmetic or on a CASE is
// reported when the statement compiles, naming what it was asked of.
func (e Expr[T]) Over() Expr[T] {
	out := e
	out.n.over = out.n.over.clone()
	return out
}

// PartitionBy restarts the window function within each group of rows sharing
// these columns' values, the way GroupBy divides a grouped read. With no
// PartitionBy, the whole result is one window.
//
// Terms accumulate across calls, and it implies Over.
func (e Expr[T]) PartitionBy(cols ...ColumnMeta) Expr[T] {
	out := e
	out.n.over = out.n.over.clone()
	out.n.over.partition = append(out.n.over.partition, cols...)
	return out
}

// OrderBy decides the order a window function counts rows in within their
// window. Without it, the database is free to visit them in any order, which
// is rarely what a caller wants from RowNumber, Rank or a running total.
//
// Terms accumulate across calls, and it implies Over.
//
// It is the window's own ordering, not the statement's: a read may sort its
// rows one way and count them another, and the two never interfere.
func (e Expr[T]) OrderBy(ords ...Ordering) Expr[T] {
	out := e
	out.n.over = out.n.over.clone()
	out.n.over.order = append(out.n.over.order, ords...)
	return out
}

// Rows narrows a window to the rows around the current one, counted as rows.
//
//	orm.SumOf(Sales.Total).
//	    OrderBy(Sales.Day.Asc()).
//	    Rows(orm.UnboundedPreceding(), orm.CurrentRow())
//	// SUM("total") OVER (ORDER BY "day" ASC ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW)
//
// That is a running total written out in full, and close to what a window
// with an ORDER BY already means, so a frame earns its place where it
// differs — a trailing average over the last three rows, say:
//
//	orm.AvgOf(Sales.Total).OrderBy(Sales.Day.Asc()).Rows(orm.Preceding(2), orm.CurrentRow())
//
// Rows counts rows; Range counts values. Two rows sorted equal are two rows
// here and one value there.
func (e Expr[T]) Rows(start, end FrameBound) Expr[T] { return e.framed(frameRows, start, end) }

// Range narrows a window by value rather than by position: every row sorted
// equal to the current one is in the frame together.
//
// With an ORDER BY and no frame at all, a window is RANGE BETWEEN UNBOUNDED
// PRECEDING AND CURRENT ROW, which is why a running total over a column with
// ties adds every tied row at once. Rows is what counts them one at a time.
func (e Expr[T]) Range(start, end FrameBound) Expr[T] { return e.framed(frameRange, start, end) }

func (e Expr[T]) framed(kind frameKind, start, end FrameBound) Expr[T] {
	out := e
	out.n.over = out.n.over.clone()
	out.n.over.frame = &frameSpec{kind: kind, start: start, end: end}
	return out
}

// frameKind is whether a frame counts rows or values.
type frameKind int

const (
	frameRows frameKind = iota
	frameRange
)

func (k frameKind) String() string {
	if k == frameRange {
		return "RANGE"
	}
	return "ROWS"
}

// frameSpec is a window's frame: BETWEEN start AND end, counted either way.
type frameSpec struct {
	kind  frameKind
	start FrameBound
	end   FrameBound
}

// FrameBound is one end of a window frame: where it starts, or where it
// stops, relative to the row being computed.
//
// The five spellings SQL has are the five constructors below. It has no
// exported fields, so a bound is always one of them rather than something
// assembled by hand.
type FrameBound struct {
	kind   boundKind
	offset int
}

// boundKind is which of the five bounds a FrameBound is.
//
// The values are declared in the order the bounds themselves fall in,
// earliest first, which is what lets a frame check that its start does not
// come after its end by comparing the two.
type boundKind int

const (
	boundUnboundedPreceding boundKind = iota
	boundPreceding
	boundCurrentRow
	boundFollowing
	boundUnboundedFollowing
)

// String returns the bound as SQL spells it, which is also how an error
// naming a bound reads: the words a caller would recognise from the
// statement rather than the constructor they wrote.
//
// The offset is written literally, not bound. It is a Go int, never text a
// caller supplied, and several databases reject a placeholder in a frame
// outright — the same reasoning limitOffset gives for LIMIT.
func (b FrameBound) String() string {
	switch b.kind {
	case boundUnboundedPreceding:
		return "UNBOUNDED PRECEDING"
	case boundPreceding:
		return strconv.Itoa(b.offset) + " PRECEDING"
	case boundFollowing:
		return strconv.Itoa(b.offset) + " FOLLOWING"
	case boundUnboundedFollowing:
		return "UNBOUNDED FOLLOWING"
	}
	return "CURRENT ROW"
}

// UnboundedPreceding is every row from the start of the window.
func UnboundedPreceding() FrameBound { return FrameBound{kind: boundUnboundedPreceding} }

// Preceding is n rows, or values, back from the current row.
func Preceding(n int) FrameBound { return FrameBound{kind: boundPreceding, offset: n} }

// CurrentRow is the row being computed.
func CurrentRow() FrameBound { return FrameBound{kind: boundCurrentRow} }

// Following is n rows, or values, forward from the current row.
func Following(n int) FrameBound { return FrameBound{kind: boundFollowing, offset: n} }

// UnboundedFollowing is every row to the end of the window.
func UnboundedFollowing() FrameBound { return FrameBound{kind: boundUnboundedFollowing} }
