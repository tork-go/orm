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

// An aggregate is an ordinary expression: a call, marked as one so DISTINCT
// has somewhere to attach. That is what lets it be compared against another
// aggregate, combined with arithmetic, ordered by, and read by SelectAs,
// none of which needed a rule of its own.
//
//	orm.SumOf(Sales.Revenue).DividedBy(orm.CountAll())
//	orm.SumOf(Sales.Revenue).GreaterThan(orm.SumOf(Sales.Cost))
//	orm.CountOf(Books.AuthorID).Distinct()

// CountOf is COUNT(col), how many rows have a value in that column.
func CountOf(col ColumnMeta) Expr[int64] { return aggregate[int64]("COUNT", col) }

// CountAll is COUNT(*), how many rows there are.
func CountAll() Expr[int64] {
	e := aggregate[int64]("COUNT")
	e.n.star = true
	return e
}

// AvgOf is AVG(col), always a float64 whatever the column holds, for the
// reason the free function Avg gives: a mean of integers is not an
// integer, and rounding it silently would be a lie.
func AvgOf(col ColumnMeta) Expr[float64] { return aggregate[float64]("AVG", col) }

// SumOf is SUM(col), typed by the column: T is inferred from col, the same
// way the free function Sum's is.
func SumOf[T any](col Ref[T]) Expr[T] { return aggregate[T]("SUM", col) }

// MinOf is MIN(col), typed by the column.
func MinOf[T any](col Ref[T]) Expr[T] { return aggregate[T]("MIN", col) }

// MaxOf is MAX(col), typed by the column.
func MaxOf[T any](col Ref[T]) Expr[T] { return aggregate[T]("MAX", col) }

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
func CountOfExpr[T any](e Expr[T]) Expr[int64] { return aggregate[int64]("COUNT", e) }

// SumOfExpr is SUM(expr), typed by the expression.
func SumOfExpr[T any](e Expr[T]) Expr[T] { return aggregate[T]("SUM", e) }

// AvgOfExpr is AVG(expr), always a float64 for the reason AvgOf gives.
func AvgOfExpr[T any](e Expr[T]) Expr[float64] { return aggregate[float64]("AVG", e) }

// MinOfExpr is MIN(expr), typed by the expression.
func MinOfExpr[T any](e Expr[T]) Expr[T] { return aggregate[T]("MIN", e) }

// MaxOfExpr is MAX(expr), typed by the expression.
func MaxOfExpr[T any](e Expr[T]) Expr[T] { return aggregate[T]("MAX", e) }

// Projection is a computed read: a source query's conditions and ordering,
// carried from a QuerySource the same way Scalars and Grouped carry them,
// plus a SELECT list of arbitrary columns and aggregates, and its own
// GROUP BY, HAVING, ordering and limit.
type Projection[T any] struct {
	q     queryState
	exprs []SelectExpr

	groupBy []SelectExpr
	having  []Predicate
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
//
// A term is a column or any expression the SELECT list could carry, which is
// what the report that groups by a computed value needs:
//
//	month := orm.Fn[time.Time]("date_trunc", "month", Orders.CreatedAt)
//	orm.SelectAs[Monthly](Orders.With(db), month, orm.SumOf(Orders.Total)).
//	    GroupBy(month)
//
// The expression is written out again in the GROUP BY rather than referred
// to by its position in the SELECT list. An output column's ordinal is legal
// in most databases and reads as a magic number in all of them, which is the
// reason Grouped's own ordering repeats its aggregate too.
func (p *Projection[T]) GroupBy(terms ...SelectExpr) *Projection[T] {
	out := p.clone()
	out.groupBy = append(out.groupBy, terms...)
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

// Having keeps only the groups matching the given conditions.
//
//	orm.SelectAs[Report](Sales.With(db), Sales.Region, orm.SumOf(Sales.Revenue)).
//	    GroupBy(Sales.Region).
//	    Having(orm.SumOf(Sales.Revenue).GreaterThan(1000))
//
// It takes predicates, exactly as Where does, so everything that composes a
// condition composes here: several accumulate and are joined with AND, orm.Or
// nests alternatives, and both sides may be aggregates.
//
//	.Having(orm.SumOf(Sales.Revenue).GreaterThan(orm.SumOf(Sales.Cost)))
//	.Having(orm.Or(orm.CountAll().GreaterThan(10), orm.AvgOf(Items.Price).LessThan(5.0)))
//
// A condition need not name an aggregate already in the SELECT list: SQL
// allows HAVING COUNT(*) > 5 with nothing counted in the output, and this
// follows it rather than second-guessing which aggregates a caller meant to
// see.
//
// The difference from Where is which rows each is asked about. Where filters
// rows before they are grouped; Having filters the groups afterwards, and is
// the only one of the two that may name an aggregate at all.
func (p *Projection[T]) Having(preds ...Predicate) *Projection[T] {
	out := p.clone()
	out.having = append(out.having, preds...)
	return out
}

// clone copies the projection so a builder method can narrow the copy and
// leave the original alone, for the reason Filtered.clone gives.
func (p *Projection[T]) clone() *Projection[T] {
	out := *p
	out.q.preds = append([]Predicate(nil), p.q.preds...)
	out.exprs = append([]SelectExpr(nil), p.exprs...)
	out.groupBy = append([]SelectExpr(nil), p.groupBy...)
	out.having = append([]Predicate(nil), p.having...)
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
//
// A term renders exactly as it would in the SELECT list, through the same
// selectTerm, which is what lets a computed value be grouped by: the two
// clauses have to spell it identically for the database to see one
// expression rather than two.
func (p *Projection[T]) groupByClause(c *compiler) (string, error) {
	if len(p.groupBy) == 0 {
		return "", nil
	}
	parts := make([]string, len(p.groupBy))
	for i, term := range p.groupBy {
		s, err := c.selectTerm(term)
		if err != nil {
			return "", err
		}
		parts[i] = s
	}
	return " GROUP BY " + strings.Join(parts, ", "), nil
}

// havingClause renders HAVING, or "" when there are no conditions.
//
// The conditions are rendered by the same group a WHERE's are, so they are
// joined with AND and nest through orm.Or identically. The one difference is
// where the clause lands, which is what decides whether an aggregate may
// appear in it.
//
// Each aggregate is written out again rather than referred to by its
// position in the SELECT list, the same reason Grouped's orderClause repeats
// its own: an output column's ordinal is legal in most databases and reads
// as a magic number in all of them.
func (p *Projection[T]) havingClause(c *compiler) (string, error) {
	if len(p.having) == 0 {
		return "", nil
	}
	s, err := c.group(Group{Conj: ConjAnd, Preds: p.having})
	if err != nil {
		return "", err
	}
	return " HAVING " + s, nil
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

func (p *Projection[T]) derivedErr() error { return p.q.err }
