package orm

import (
	"context"
	"fmt"
	"reflect"
	"strings"
)

// SelectExpr is one item read into a SelectAs projection: a column, or an
// aggregate or window expression over one.
//
// Its contract is deliberately just GoType, the one method ColumnMeta
// already has, so every existing typed column satisfies SelectExpr for
// free — Users.Username can be passed to SelectAs directly, with no
// wrapper type needed for the plain-column case.
type SelectExpr interface {
	GoType() reflect.Type
}

// AggregateExpr is one aggregate expression in a SelectAs projection or a
// GroupBy's aggregate slot: COUNT, SUM, AVG, MIN or MAX over a column, or
// COUNT(*) for CountAll.
type AggregateExpr struct {
	fn  string     // COUNT, SUM, AVG, MIN, MAX
	col ColumnMeta // nil for CountAll, and for the OfExpr forms

	// expr is set instead of col by the OfExpr forms, which aggregate a
	// computed value rather than a stored one. Both nil is CountAll.
	expr   expression
	goType reflect.Type
}

// GoType returns the Go type the aggregate's result decodes as.
func (a AggregateExpr) GoType() reflect.Type { return a.goType }

// CountOf is COUNT(col).
func CountOf(col ColumnMeta) AggregateExpr {
	return AggregateExpr{fn: "COUNT", col: col, goType: reflect.TypeFor[int64]()}
}

// CountAll is COUNT(*).
func CountAll() AggregateExpr {
	return AggregateExpr{fn: "COUNT", goType: reflect.TypeFor[int64]()}
}

// AvgOf is AVG(col), always a float64 whatever the column holds, for the
// reason the free function Avg gives: a mean of integers is not an
// integer, and rounding it silently would be a lie.
func AvgOf(col ColumnMeta) AggregateExpr {
	return AggregateExpr{fn: "AVG", col: col, goType: reflect.TypeFor[float64]()}
}

// SumOf is SUM(col), typed by the column: T is inferred from col, the same
// way the free function Sum's is.
func SumOf[T any](col Ref[T]) AggregateExpr {
	return AggregateExpr{fn: "SUM", col: col, goType: reflect.TypeFor[T]()}
}

// MinOf is MIN(col), typed by the column.
func MinOf[T any](col Ref[T]) AggregateExpr {
	return AggregateExpr{fn: "MIN", col: col, goType: reflect.TypeFor[T]()}
}

// MaxOf is MAX(col), typed by the column.
func MaxOf[T any](col Ref[T]) AggregateExpr {
	return AggregateExpr{fn: "MAX", col: col, goType: reflect.TypeFor[T]()}
}

// The OfExpr forms aggregate a value the database computes rather than a
// stored column: arithmetic, a CASE, anything an expression can be.
//
// The conditional tally is what they are most often for, since SQL has no
// COUNT with a condition and writes one as a SUM over a CASE:
//
//	orm.SelectAs[Report](Users.With(db), Users.Country,
//	    orm.SumOfExpr(orm.Case[int]().
//	        When(Users.Active.Equals(true), 1).
//	        Else(0)),
//	).GroupBy(Users.Country)
//	// SELECT "country", SUM(CASE WHEN "active" = $1 THEN $2 ELSE $3 END) ...
//
// They are separate constructors rather than a widening of SumOf and its
// siblings because an Expr[T] cannot satisfy Ref[T] — it has no column
// behind it to return from Base — and a parameter general enough to accept
// either would leave T with nothing to be inferred from. Taking Expr[T]
// keeps T inferred from the expression, exactly as SumOf infers it from
// the column.

// CountOfExpr is COUNT(expr).
func CountOfExpr[T any](e Expr[T]) AggregateExpr {
	return AggregateExpr{fn: "COUNT", expr: e, goType: reflect.TypeFor[int64]()}
}

// SumOfExpr is SUM(expr), typed by the expression.
func SumOfExpr[T any](e Expr[T]) AggregateExpr {
	return AggregateExpr{fn: "SUM", expr: e, goType: reflect.TypeFor[T]()}
}

// AvgOfExpr is AVG(expr), always a float64 for the reason AvgOf gives.
func AvgOfExpr[T any](e Expr[T]) AggregateExpr {
	return AggregateExpr{fn: "AVG", expr: e, goType: reflect.TypeFor[float64]()}
}

// MinOfExpr is MIN(expr), typed by the expression.
func MinOfExpr[T any](e Expr[T]) AggregateExpr {
	return AggregateExpr{fn: "MIN", expr: e, goType: reflect.TypeFor[T]()}
}

// MaxOfExpr is MAX(expr), typed by the expression.
func MaxOfExpr[T any](e Expr[T]) AggregateExpr {
	return AggregateExpr{fn: "MAX", expr: e, goType: reflect.TypeFor[T]()}
}

// projectionHaving is one HAVING term of a Projection: which of its own
// AggregateExpr values it compares, so a Having naming an aggregate the
// SELECT list never listed is rejected rather than silently accepted.
type projectionHaving struct {
	expr  AggregateExpr
	op    Operator
	value any
}

// Projection is a computed read: a source query's conditions and ordering,
// carried from a QuerySource the same way Scalars and Grouped carry them,
// plus a SELECT list of arbitrary columns and aggregates, and its own
// GROUP BY, HAVING, ordering and limit.
type Projection[T any] struct {
	q     queryState
	exprs []SelectExpr

	groupBy []ColumnMeta
	having  []projectionHaving
	ords    []Ordering
	limit   *int
}

// SelectAs reads exprs — any mix of typed columns and aggregate or window
// expressions — into T, one value per row.
//
//	type UserReport struct {
//	    Username  string
//	    PostCount int64
//	}
//
//	rows, err := orm.SelectAs[UserReport](
//	    Users.With(db).LeftJoin(Users.Posts),
//	    Users.Username,
//	    orm.CountOf(Posts.ID),
//	).GroupBy(Users.Username).All(ctx)
//
// T's exported fields, in declaration order, are matched positionally
// against exprs: no string aliasing, the same convention every scan in
// this package already follows, since driver.Rows exposes no column names
// to match by name in the first place. A count or type mismatch between
// T's fields and exprs is reported once the statement runs.
//
// It carries the whole source query — conditions, a Join included — the
// same way orm.Select and the aggregate functions do.
func SelectAs[T any](src QuerySource, exprs ...SelectExpr) *Projection[T] {
	p := &Projection[T]{exprs: exprs}
	if src == nil {
		p.q.err = fmt.Errorf("orm: SelectAs was given no query")
		return p
	}
	p.q = src.querySource()
	if err := checkProjectionShape[T](exprs); err != nil {
		p.q.err = firstErr(p.q.err, err)
	}
	return p
}

// checkProjectionShape validates that T's exported fields, in order, match
// exprs one for one in count and in Go type, the same style Find's own key
// type check uses.
func checkProjectionShape[T any](exprs []SelectExpr) error {
	t := reflect.TypeFor[T]()
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("orm: SelectAs: %s is not a struct", t)
	}
	var fields []reflect.StructField
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).IsExported() {
			fields = append(fields, t.Field(i))
		}
	}
	if len(fields) != len(exprs) {
		return fmt.Errorf("orm: SelectAs: %s has %d exported field(s) but %d "+
			"expression(s) were given; they are matched one for one, in order",
			t, len(fields), len(exprs))
	}
	for i, f := range fields {
		if got := exprs[i].GoType(); !got.AssignableTo(f.Type) {
			return fmt.Errorf("orm: SelectAs: field %d, %q, is %s but expression %d is %s",
				i, f.Name, f.Type, i, got)
		}
	}
	return nil
}

// GroupBy adds a GROUP BY clause. Terms accumulate across calls.
func (p *Projection[T]) GroupBy(cols ...ColumnMeta) *Projection[T] {
	out := p.clone()
	out.groupBy = append(out.groupBy, cols...)
	return out
}

// OrderBy sorts the results. Terms accumulate across calls.
func (p *Projection[T]) OrderBy(ords ...Ordering) *Projection[T] {
	out := p.clone()
	out.ords = append(out.ords, ords...)
	return out
}

// Limit caps the number of rows returned. A negative Limit is an error,
// reported from whichever terminal runs.
func (p *Projection[T]) Limit(n int) *Projection[T] {
	out := p.clone()
	if n < 0 {
		out.q.err = firstErr(out.q.err, fmt.Errorf("orm: SelectAs: Limit(%d) is negative", n))
		return out
	}
	out.limit = &n
	return out
}

// Having keeps only the groups whose aggregate compares as given. expr is
// rendered on its own terms, the same as any AggregateExpr passed to
// SelectAs is, so it need not be one already in the SELECT list — SQL
// itself allows HAVING COUNT(*) > 5 without COUNT(*) selected, and this
// follows it. Passing one of SelectAs's own expressions is the usual case;
// a Projection may carry several aggregates, unlike Grouped's implicit
// single one, so Having has to say which.
func (p *Projection[T]) Having(expr AggregateExpr, op Operator, value any) *Projection[T] {
	out := p.clone()
	out.having = append(out.having, projectionHaving{expr: expr, op: op, value: value})
	return out
}

// clone copies the projection so a builder method can narrow the copy and
// leave the original alone, for the reason Filtered.clone gives.
func (p *Projection[T]) clone() *Projection[T] {
	out := *p
	out.q.preds = append([]Predicate(nil), p.q.preds...)
	out.exprs = append([]SelectExpr(nil), p.exprs...)
	out.groupBy = append([]ColumnMeta(nil), p.groupBy...)
	out.having = append([]projectionHaving(nil), p.having...)
	out.ords = append([]Ordering(nil), p.ords...)
	return &out
}

// SQL returns the statement this would run, and its bound arguments,
// without running it.
func (p *Projection[T]) SQL() (string, []any, error) { return p.compile() }

func (p *Projection[T]) compile() (string, []any, error) {
	c, err := p.compiler()
	if err != nil {
		return "", nil, err
	}
	sql, err := p.render(c, nil)
	if err != nil {
		return "", nil, err
	}
	return sql, c.args.args, nil
}

// compiler starts this projection's own compiler, after the checks that
// decide whether it can be compiled at all.
func (p *Projection[T]) compiler() (*compiler, error) {
	if err := p.q.readyToRead(); err != nil {
		return nil, err
	}
	if err := p.q.noLock("SelectAs"); err != nil {
		return nil, err
	}
	if err := p.q.noCTEs("SelectAs"); err != nil {
		return nil, err
	}
	return p.q.compilerJoined()
}

// render writes the statement against c. aliases, when given, names each
// selected expression with AS, which is what a derived table needs so the
// enclosing query can refer to the columns it declares.
func (p *Projection[T]) render(c *compiler, aliases []string) (string, error) {
	list, err := c.selectExprListAs(p.exprs, aliases)
	if err != nil {
		return "", err
	}
	// The FROM binds before the WHERE and after the SELECT list, which is
	// where it sits in the finished statement; see queryState.fromClause.
	from, err := p.q.fromClause(c)
	if err != nil {
		return "", err
	}
	where, err := c.where(p.q.effectivePreds())
	if err != nil {
		return "", err
	}
	groupBy, err := p.groupByClause(c)
	if err != nil {
		return "", err
	}
	having, err := p.havingClause(c)
	if err != nil {
		return "", err
	}
	order, err := c.orderBy(p.ords)
	if err != nil {
		return "", err
	}
	return "SELECT " + list + " FROM " + from + c.joinsClause() +
		where + groupBy + having + order + limitOffset(p.limit, nil), nil
}

// derivedSource renders this projection as a derived table's rows, sharing
// the enclosing statement's arguments so placeholders number continuously
// across the boundary — the same technique compiler.sub uses.
func (p *Projection[T]) derivedSource(outer *compiler, aliases []string) (string, error) {
	c, err := p.compiler()
	if err != nil {
		return "", err
	}
	c.args = outer.args
	return p.render(c, aliases)
}

// derivedShape is the Go type each selected expression yields.
func (p *Projection[T]) derivedShape() []reflect.Type {
	out := make([]reflect.Type, len(p.exprs))
	for i, e := range p.exprs {
		out[i] = e.GoType()
	}
	return out
}

func (p *Projection[T]) derivedDB() *DB { return p.q.db }

// groupByClause renders GROUP BY, or "" when there is nothing to group by.
func (p *Projection[T]) groupByClause(c *compiler) (string, error) {
	if len(p.groupBy) == 0 {
		return "", nil
	}
	parts := make([]string, len(p.groupBy))
	for i, col := range p.groupBy {
		name, err := c.column(col)
		if err != nil {
			return "", err
		}
		parts[i] = name
	}
	return " GROUP BY " + strings.Join(parts, ", "), nil
}

// havingClause renders HAVING, or "" when there are no terms.
//
// Each term's aggregate is re-rendered rather than referred to by
// position, the same reason Grouped's orderClause re-renders its own
// aggregate: repeating the expression is what every database understands,
// where an output column's ordinal position is legal but reads as a magic
// number.
func (p *Projection[T]) havingClause(c *compiler) (string, error) {
	if len(p.having) == 0 {
		return "", nil
	}
	parts := make([]string, len(p.having))
	for i, h := range p.having {
		agg, err := c.aggregateExpr(h.expr)
		if err != nil {
			return "", err
		}
		parts[i] = agg + " " + h.op.String() + " " + c.args.bind(h.value)
	}
	return " HAVING " + strings.Join(parts, " AND "), nil
}

// All runs the query and returns every matching row.
func (p *Projection[T]) All(ctx context.Context) ([]T, error) {
	sql, args, err := p.compile()
	if err != nil {
		return nil, err
	}
	rows, err := p.q.db.ex.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("orm: table %q: %w", p.q.tableName(), err)
	}
	defer rows.Close()

	var out []T
	for rows.Next() {
		v, err := scanStruct[T](rows)
		if err != nil {
			return nil, fmt.Errorf("orm: table %q: %w", p.q.tableName(), err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("orm: table %q: reading rows: %w", p.q.tableName(), err)
	}
	return out, nil
}

// First returns the first matching row, or ErrNoRows when the query
// matched none.
//
// A limit the caller set is narrowed rather than respected: one row is all
// this reads either way, and Limit's own clone keeps that invisible to the
// query it was called on.
func (p *Projection[T]) First(ctx context.Context) (T, error) {
	var zero T
	rows, err := p.Limit(1).All(ctx)
	if err != nil {
		return zero, err
	}
	if len(rows) == 0 {
		return zero, ErrNoRows
	}
	return rows[0], nil
}
