package orm

import (
	"context"
	"fmt"
	"strings"
)

// Grouping is spelled as one function per aggregate rather than a Group
// method followed by a Sum method, because the second of those cannot exist:
// the value's type comes from the column being aggregated, and a Go method
// cannot introduce a type parameter of its own. CountBy, SumBy, AvgBy, MinBy
// and MaxBy each name both halves at once, so nothing has to be spelled out.
//
// One key, not several. Two would need a Group with two key types and a
// constructor per arity, and the shape that actually wants several keys is
// usually better served by a view. Ordering, capping and HAVING are all here,
// since a group query almost always wants the largest few.

// Bucket is one row of a grouped query: the value the rows were grouped by,
// and what the aggregate made of them.
//
// Not Group, which is already a predicate here, and which would be the
// obvious name otherwise. Bucket is what the same shape is called wherever
// aggregation is the subject rather than a clause.
type Bucket[K, V any] struct {
	Key   K
	Value V
}

// Grouped is a grouped query, before it runs.
type Grouped[K, V any] struct {
	q   queryState
	key Ref[K]

	// fn is the aggregate's name and col the column it reads, which is nil
	// for COUNT: counting rows asks about no column in particular.
	fn  string
	col ColumnMeta

	having     []havingTerm
	ords       []Ordering
	valueOrder int // -1 descending, 1 ascending, 0 not ordered by value
	limit      *int
}

// havingTerm is one comparison against the aggregate.
type havingTerm struct {
	op    Operator
	value any
}

// CountBy counts the rows in each group.
//
//	byCountry, err := orm.CountBy(Users.With(db), Users.Country).All(ctx)
//	// []orm.Bucket[string, int64]{{Key: "TR", Value: 42}, ...}
func CountBy[K any](src QuerySource, key Ref[K]) *Grouped[K, int64] {
	return newGrouped[K, int64](src, key, "COUNT", nil)
}

// SumBy totals a column within each group.
//
//	orm.SumBy(Users.With(db), Users.Country, Users.Age).All(ctx)
//	// []orm.Bucket[string, int]
func SumBy[K, V any](src QuerySource, key Ref[K], col Ref[V]) *Grouped[K, V] {
	return newGrouped[K, V](src, key, "SUM", col)
}

// AvgBy averages a column within each group, as a float whatever the column
// holds, for the reason Avg gives.
func AvgBy[K, V any](src QuerySource, key Ref[K], col Ref[V]) *Grouped[K, float64] {
	return newGrouped[K, float64](src, key, "AVG", col)
}

// MinBy takes the smallest value of a column within each group.
func MinBy[K, V any](src QuerySource, key Ref[K], col Ref[V]) *Grouped[K, V] {
	return newGrouped[K, V](src, key, "MIN", col)
}

// MaxBy takes the largest value of a column within each group.
func MaxBy[K, V any](src QuerySource, key Ref[K], col Ref[V]) *Grouped[K, V] {
	return newGrouped[K, V](src, key, "MAX", col)
}

// newGrouped builds the value every spelling shares, carrying a missing key
// or column as the query's error so it is reported when the statement is
// built rather than by panicking here.
func newGrouped[K, V any](src QuerySource, key Ref[K], fn string, col ColumnMeta) *Grouped[K, V] {
	g := &Grouped[K, V]{fn: fn, col: col}
	if src == nil {
		g.q.err = fmt.Errorf("orm: grouping by %s was given no query", fn)
		return g
	}
	g.q = src.querySource()
	g.key = key
	switch {
	case key == nil:
		g.q.err = firstErr(g.q.err, fmt.Errorf("orm: table %q: grouping needs a key column",
			g.q.tableName()))
	case fn != "COUNT" && col == nil:
		g.q.err = firstErr(g.q.err, fmt.Errorf("orm: table %q: %s needs a column to aggregate",
			g.q.tableName(), fn))
	}
	return g
}

func (g *Grouped[K, V]) clone() *Grouped[K, V] {
	out := *g
	out.having = append([]havingTerm(nil), g.having...)
	out.ords = append([]Ordering(nil), g.ords...)
	return &out
}

// Having keeps only the groups whose aggregate compares as given.
//
//	orm.CountBy(Users.With(db), Users.Country).Having(orm.OpGreaterOrEqual, 10)
//	// HAVING COUNT(*) >= $1
//
// It takes the same Operator a column comparison does, rather than a
// vocabulary of its own, since the thing being compared is a single value and
// the six spellings are the same six.
//
// Conditions accumulate and are joined with AND.
func (g *Grouped[K, V]) Having(op Operator, v V) *Grouped[K, V] {
	out := g.clone()
	out.having = append(out.having, havingTerm{op: op, value: v})
	return out
}

// OrderBy sorts the groups by the key.
//
// Only the key can be ordered by: every other column has been collapsed into
// the aggregate, and a database asked to sort by one would either reject the
// statement or pick a row arbitrarily. Ordering by the aggregate is
// OrderByValue.
func (g *Grouped[K, V]) OrderBy(ords ...Ordering) *Grouped[K, V] {
	out := g.clone()
	out.ords = append(out.ords, ords...)
	return out
}

// OrderByValue sorts the groups by the aggregate, smallest first.
func (g *Grouped[K, V]) OrderByValue() *Grouped[K, V] {
	out := g.clone()
	out.valueOrder = 1
	return out
}

// OrderByValueDesc sorts the groups by the aggregate, largest first, which is
// what "the top ten" means.
//
//	orm.CountBy(Users.With(db), Users.Country).OrderByValueDesc().Limit(10)
func (g *Grouped[K, V]) OrderByValueDesc() *Grouped[K, V] {
	out := g.clone()
	out.valueOrder = -1
	return out
}

// Limit caps how many groups come back.
func (g *Grouped[K, V]) Limit(n int) *Grouped[K, V] {
	out := g.clone()
	if n < 0 {
		out.q.err = firstErr(out.q.err,
			fmt.Errorf("orm: table %q: Limit(%d) is negative", g.q.tableName(), n))
		return out
	}
	out.limit = &n
	return out
}

// SQL returns the statement this would run, and its bound arguments, without
// running it.
func (g *Grouped[K, V]) SQL() (string, []any, error) {
	return g.compile()
}

func (g *Grouped[K, V]) compile() (string, []any, error) {
	if err := g.q.readyToRead(); err != nil {
		return "", nil, err
	}
	if err := g.q.noLock(g.fn + "By"); err != nil {
		return "", nil, err
	}
	if err := g.q.noJoins(g.fn + "By"); err != nil {
		return "", nil, err
	}
	if err := g.q.noCTEs(g.fn + "By"); err != nil {
		return "", nil, err
	}
	if err := g.q.noDistinctOn(g.fn + "By"); err != nil {
		return "", nil, err
	}
	c := g.q.compiler()

	key, err := c.column(g.key)
	if err != nil {
		return "", nil, err
	}
	agg, err := g.aggregate(c)
	if err != nil {
		return "", nil, err
	}

	from, err := g.q.fromClause(c)
	if err != nil {
		return "", nil, err
	}
	where, err := c.where(g.q.effectivePreds())
	if err != nil {
		return "", nil, err
	}

	// The aggregate's own arguments bind after the filter's, matching where
	// each appears in the statement.
	having := g.havingClause(c, agg)
	order, err := g.orderClause(c, agg)
	if err != nil {
		return "", nil, err
	}

	sql := "SELECT " + key + ", " + agg + " FROM " + from +
		where + " GROUP BY " + key + having + order + limitOffset(g.limit, nil)
	return sql, c.args.args, nil
}

// aggregate renders the aggregate expression, which is COUNT(*) when nothing
// in particular is being counted.
func (g *Grouped[K, V]) aggregate(c *compiler) (string, error) {
	if g.col == nil {
		return "COUNT(*)", nil
	}
	name, err := c.column(g.col)
	if err != nil {
		return "", err
	}
	if g.q.distinct {
		return g.fn + "(DISTINCT " + name + ")", nil
	}
	return g.fn + "(" + name + ")", nil
}

// havingClause cannot fail: the aggregate expression is already rendered, and
// a comparison against it only binds a value.
func (g *Grouped[K, V]) havingClause(c *compiler, agg string) string {
	if len(g.having) == 0 {
		return ""
	}
	parts := make([]string, len(g.having))
	for i, h := range g.having {
		parts[i] = agg + " " + h.op.String() + " " + c.args.bind(h.value)
	}
	return " HAVING " + strings.Join(parts, " AND ")
}

// orderClause sorts by the key, by the aggregate, or by both in that order.
//
// The aggregate is written out again rather than referred to by position.
// Ordering by an output column's number is legal but reads as a magic number,
// and repeating the expression is what every database understands.
func (g *Grouped[K, V]) orderClause(c *compiler, agg string) (string, error) {
	var parts []string
	for _, o := range g.ords {
		if o.Col == nil || o.Col.Name() != g.key.Name() {
			return "", fmt.Errorf("orm: table %q: a grouped query can only be ordered by "+
				"its key, %q, or by the aggregate with OrderByValue; every other column "+
				"has been collapsed into the group", g.q.st.name, g.key.Name())
		}
		name, err := c.column(o.Col)
		if err != nil {
			return "", err
		}
		if o.Desc {
			name += " DESC"
		} else {
			name += " ASC"
		}
		parts = append(parts, name)
	}
	switch g.valueOrder {
	case 1:
		parts = append(parts, agg+" ASC")
	case -1:
		parts = append(parts, agg+" DESC")
	}
	if len(parts) == 0 {
		return "", nil
	}
	return " ORDER BY " + strings.Join(parts, ", "), nil
}

// All runs the query and returns one Bucket per distinct key.
func (g *Grouped[K, V]) All(ctx context.Context) ([]Bucket[K, V], error) {
	sql, args, err := g.compile()
	if err != nil {
		return nil, err
	}

	rows, err := g.q.db.ex.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("orm: table %q: %w", g.q.st.name, err)
	}
	defer rows.Close()

	var out []Bucket[K, V]
	for rows.Next() {
		var (
			key   K
			value V
		)
		// The key may be NULL when its column is nullable, in which case K is
		// a pointer and holds it; the aggregate may be NULL when every value
		// in the group was, which is the same situation Sum answers with a
		// zero or a nil.
		if err := scanGroupKey(rows, &key, &value); err != nil {
			return nil, fmt.Errorf("orm: table %q: scanning group: %w", g.q.st.name, err)
		}
		out = append(out, Bucket[K, V]{Key: key, Value: value})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("orm: table %q: reading groups: %w", g.q.st.name, err)
	}
	return out, nil
}

// Keys returns just the keys, for a grouped query used to find the distinct
// values that satisfy a HAVING.
func (g *Grouped[K, V]) Keys(ctx context.Context) ([]K, error) {
	groups, err := g.All(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]K, len(groups))
	for i, grp := range groups {
		keys[i] = grp.Key
	}
	return keys, nil
}

// scanGroupKey reads a key and an aggregate, giving each a destination that
// can hold a NULL for the reason scanNullable explains.
func scanGroupKey[K, V any](rows Rows, key *K, value *V) error {
	keyDest, keyDone := nullableDest(key)
	valDest, valDone := nullableDest(value)
	if err := rows.Scan(keyDest, valDest); err != nil {
		return err
	}
	keyDone()
	valDone()
	return nil
}

// nullableDest returns a destination for v that tolerates NULL, and a
// function that moves what was read into v.
//
// A pointer type holds NULL itself, so it is its own destination. Anything
// else is read through one more pointer and left at its zero value when the
// database answers NULL.
func nullableDest[T any](v *T) (any, func()) {
	if isPointerType[T]() {
		return v, func() {}
	}
	holder := new(*T)
	return holder, func() {
		if *holder != nil {
			*v = **holder
		}
	}
}
