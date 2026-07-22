package orm

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// argBuilder collects a statement's bound values and hands back the
// placeholder for each.
//
// Every value a caller supplies goes through here, which is what makes the
// query API injection-safe by construction: a value can only reach the
// statement as a parameter, never as text. The only SQL written literally
// is what a caller wrote themselves, in a server default, a check
// expression, or an index predicate.
//
// Placeholder counts from one, which is exactly len(args) once the value
// has been appended, so the numbering needs no separate counter to drift.
type argBuilder struct {
	d    QueryDialect
	args []any
}

func (b *argBuilder) bind(v any) string {
	b.args = append(b.args, v)
	return b.d.Placeholder(len(b.args))
}

// compiler turns predicates, orderings and assignments into SQL for one
// statement.
//
// Every statement it writes names one table, so column references are
// bare. A statement over more than one, which is what eager loading needs,
// will have to qualify them; SQL is not consistent about where that is
// even allowed, since an UPDATE's SET clause must stay bare, so the choice
// belongs per clause rather than per compiler when it arrives.
type compiler struct {
	d     QueryDialect
	args  *argBuilder
	table string // the statement's table, for validating column references

	// qualify prefixes a column with its table. A statement over one table
	// needs no prefix and reads better without one, so this is set only
	// inside a subquery, where the outer statement's table is also in scope
	// and an unqualified name would be resolved by how the two happen to be
	// nested rather than by what the caller wrote.
	qualify bool

	// unscoped carries the outer query's Unscoped call into whatever this
	// compiler renders, so a Has or HasNone predicate it compiles, or a
	// sub-compiler it starts, treats the related table's default scope the
	// same way the outer query treats its own. See existsDirect and
	// existsThrough.
	unscoped bool
}

// column renders a column reference, checking it belongs to the statement.
//
// A predicate over another table's column would otherwise compile into a
// reference to a table absent from the statement, and the caller would get
// whatever the database says about that rather than something naming the
// mistake. A column with no owner is accepted: only DefineTable binds a
// column to its table, and a model built by hand still has to be usable.
func (c *compiler) column(col ColumnMeta) (string, error) {
	if owner := col.OwnerTable(); owner != "" && owner != c.table {
		return "", fmt.Errorf("orm: table %q: column %q belongs to table %q, "+
			"which this statement does not select from",
			c.table, col.Name(), owner)
	}
	if c.qualify {
		return c.qualified(c.table, col), nil
	}
	return c.d.QuoteIdent(col.Name()), nil
}

// qualified names a column of a table other than this statement's, which is
// how a subquery refers back to the one containing it.
//
// It skips the ownership check on purpose: the whole point of a correlated
// subquery is to name a column the inner statement does not select from.
func (c *compiler) qualified(table string, col ColumnMeta) string {
	return c.d.QuoteIdent(table) + "." + c.d.QuoteIdent(col.Name())
}

// sub starts a compiler for a subquery over table.
//
// It shares this one's arguments, so placeholders keep counting across the
// boundary rather than restarting and colliding, and it qualifies, since two
// tables are now in scope.
func (c *compiler) sub(table string) *compiler {
	return &compiler{d: c.d, args: c.args, table: table, qualify: true, unscoped: c.unscoped}
}

// The two constants below stand in for an always true and an always false
// condition. They are written as comparisons rather than as TRUE and FALSE
// because those are not universal across the databases Tork targets, and
// because a comparison of two literals needs neither a dialect method nor
// a bound parameter.
const (
	sqlTrue  = "(1 = 1)"
	sqlFalse = "(1 = 0)"
)

// predicate renders one condition.
func (c *compiler) predicate(p Predicate) (string, error) {
	switch p := p.(type) {
	case Comparison:
		col, err := c.column(p.Col)
		if err != nil {
			return "", err
		}
		v, err := c.value(p.Col, p.Value)
		if err != nil {
			return "", err
		}
		return col + " " + p.Op.String() + " " + c.args.bind(v), nil

	case InList:
		return c.inList(p)

	case Range:
		col, err := c.column(p.Col)
		if err != nil {
			return "", err
		}
		lo, err := c.value(p.Col, p.Lo)
		if err != nil {
			return "", err
		}
		hi, err := c.value(p.Col, p.Hi)
		if err != nil {
			return "", err
		}
		op := " BETWEEN "
		if p.Not {
			op = " NOT BETWEEN "
		}
		return col + op + c.args.bind(lo) + " AND " + c.args.bind(hi), nil

	case Pattern:
		col, err := c.column(p.Col)
		if err != nil {
			return "", err
		}
		like := c.d.RenderLike(col, c.args.bind(p.Value), p.CaseInsensitive)
		if p.Not {
			return "NOT (" + like + ")", nil
		}
		return like, nil

	case Nullness:
		col, err := c.column(p.Col)
		if err != nil {
			return "", err
		}
		if p.Not {
			return col + " IS NOT NULL", nil
		}
		return col + " IS NULL", nil

	case Group:
		return c.group(p)

	case Negation:
		inner, err := c.predicate(p.Pred)
		if err != nil {
			return "", err
		}
		return "NOT (" + inner + ")", nil

	case Existence:
		return c.existence(p)

	case InSubquery:
		return c.inSubquery(p)

	case JSONHasKey:
		col, err := c.column(p.Col)
		if err != nil {
			return "", err
		}
		return c.d.RenderJSONHasKey(col, c.args.bind(p.Key))

	case JSONContains:
		col, err := c.column(p.Col)
		if err != nil {
			return "", err
		}
		v, err := c.value(p.Col, p.Value)
		if err != nil {
			return "", err
		}
		// The encoded document is bound as text, not as the []byte the codec
		// hands back: a []byte reaches the driver as the database's binary
		// type, which will not cast to a JSON type, where text will.
		if b, ok := v.([]byte); ok {
			v = string(b)
		}
		return c.d.RenderJSONContains(col, c.args.bind(v))

	case JSONKey:
		col, err := c.column(p.Col)
		if err != nil {
			return "", err
		}
		// Key before value, so the placeholders number in the order they read.
		key := c.args.bind(p.Key)
		return c.d.RenderJSONKey(col, key, p.Op, c.args.bind(p.Value))

	case ArrayContains:
		return c.arrayMembership(p.Col, p.Elems, true)

	case ArrayOverlaps:
		return c.arrayMembership(p.Col, p.Elems, false)

	case ArrayLength:
		col, err := c.column(p.Col)
		if err != nil {
			return "", err
		}
		return c.d.RenderArrayLength(col, p.Op, c.args.bind(p.Value))

	case FullText:
		col, err := c.column(p.Col)
		if err != nil {
			return "", err
		}
		return c.d.RenderFullText(col, c.args.bind(p.Query))

	case rawPredicate:
		return c.raw(p)
	}
	return "", fmt.Errorf("orm: table %q: unknown predicate %T", c.table, p)
}

// inList renders IN and NOT IN.
//
// An empty list is the interesting case. IN () is a syntax error in every
// database Tork targets, so it compiles to the always false condition, and
// NOT IN () to the always true one. That is what the set semantics mean:
// nothing is a member of the empty set, and everything is outside it.
// SQL's three valued logic would answer UNKNOWN rather than FALSE for a
// NULL operand, but a WHERE treats the two alike, so the difference cannot
// be observed here.
func (c *compiler) inList(p InList) (string, error) {
	if len(p.Values) == 0 {
		if p.Not {
			return sqlTrue, nil
		}
		return sqlFalse, nil
	}
	col, err := c.column(p.Col)
	if err != nil {
		return "", err
	}
	marks := make([]string, len(p.Values))
	for i, v := range p.Values {
		bound, err := c.value(p.Col, v)
		if err != nil {
			return "", err
		}
		marks[i] = c.args.bind(bound)
	}
	op := " IN ("
	if p.Not {
		op = " NOT IN ("
	}
	return col + op + strings.Join(marks, ", ") + ")", nil
}

// arrayMembership renders ArrayContains (Has, HasAll) and ArrayOverlaps
// (HasAny). elems is a typed slice, bound whole as one array parameter.
//
// An empty list is defined rather than left to the database, the same way an
// empty IN list is: containing all of nothing is true of every array, and
// overlapping nothing is false for every one. Answering it here also means the
// empty case never has to bind an empty array whose element type the driver
// would have to be told.
func (c *compiler) arrayMembership(col ColumnMeta, elems any, all bool) (string, error) {
	if reflect.ValueOf(elems).Len() == 0 {
		if all {
			return sqlTrue, nil
		}
		return sqlFalse, nil
	}
	quoted, err := c.column(col)
	if err != nil {
		return "", err
	}
	mark := c.args.bind(elems)
	if all {
		return c.d.RenderArrayContains(quoted, mark)
	}
	return c.d.RenderArrayOverlaps(quoted, mark)
}

// inSubquery renders IN and NOT IN over another query.
//
// Unlike inList there is no empty case to define: the subquery decides at run
// time how many values it yields, and a database given one that yields none
// answers the same false that an empty list compiles to.
func (c *compiler) inSubquery(p InSubquery) (string, error) {
	if p.Sub == nil {
		return "", fmt.Errorf("orm: table %q: column %q was given no subquery to match "+
			"against", c.table, p.Col.Name())
	}
	col, err := c.column(p.Col)
	if err != nil {
		return "", err
	}
	sub, err := p.Sub.compileWithin(c)
	if err != nil {
		return "", err
	}
	op := " IN ("
	if p.Not {
		op = " NOT IN ("
	}
	return col + op + sub + ")", nil
}

// group renders a parenthesised list joined by AND or OR.
//
// An empty group compiles to the identity of its conjunction, which is
// what And and Or document a zero argument call to mean: true for AND,
// false for OR.
func (c *compiler) group(g Group) (string, error) {
	if len(g.Preds) == 0 {
		if g.Conj == ConjOr {
			return sqlFalse, nil
		}
		return sqlTrue, nil
	}
	parts := make([]string, len(g.Preds))
	for i, p := range g.Preds {
		s, err := c.predicate(p)
		if err != nil {
			return "", err
		}
		parts[i] = s
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	sep := " AND "
	if g.Conj == ConjOr {
		sep = " OR "
	}
	return "(" + strings.Join(parts, sep) + ")", nil
}

// value prepares a Go value for binding.
//
// A document column's value travels as encoded bytes, exactly as it does
// on the way back out, so a predicate or assignment over one has to encode
// through the column's own codec rather than hand the driver a Go struct
// it has no way to write.
func (c *compiler) value(col ColumnMeta, v any) (any, error) {
	if v == nil || !isDocumentColumn(col) {
		return v, nil
	}
	codec, ok := col.(ValueCodec)
	if !ok {
		return nil, fmt.Errorf("orm: table %q: column %q cannot encode its value",
			c.table, col.Name())
	}
	b, err := codec.MarshalAny(v)
	if err != nil {
		return nil, fmt.Errorf("orm: table %q: %w", c.table, err)
	}
	return b, nil
}

// existence renders Has and HasNone as a correlated EXISTS.
//
// EXISTS rather than a join because the question is whether a related row is
// there, not what is in it: a join would multiply the rows a user appears in
// by the number of posts they have, and the caller would then have to collapse
// them back down. It also composes, being a predicate like any other, where a
// join is a property of the whole statement.
func (c *compiler) existence(e Existence) (string, error) {
	if e.Rel == nil {
		return "", fmt.Errorf("orm: table %q: Has was given no relationship, or one "+
			"attached to no table; declare the model with DefineTable rather than "+
			"NewTable", c.table)
	}
	info, err := e.Rel.info()
	if err != nil {
		return "", err
	}
	if info.LocalTable != c.table {
		return "", fmt.Errorf("orm: table %q: this relationship belongs to table %q, "+
			"which this statement does not select from", c.table, info.LocalTable)
	}

	var inner string
	if info.Kind == KindManyToMany {
		inner, err = c.existsThrough(e, info)
	} else {
		inner, err = c.existsDirect(e, info)
	}
	if err != nil {
		return "", err
	}
	if e.Not {
		return "NOT " + inner, nil
	}
	return inner, nil
}

// existsDirect renders the three shapes that join on a single key.
func (c *compiler) existsDirect(e Existence, info RelationInfo) (string, error) {
	sub := c.sub(info.ForeignTable)
	correlate := sub.qualified(info.ForeignTable, info.ForeignColumn) + " = " +
		sub.qualified(info.LocalTable, info.LocalColumn)

	preds := scopedPreds(c, e)
	where, err := sub.conditions(correlate, preds)
	if err != nil {
		return "", err
	}
	return "EXISTS (SELECT 1 FROM " + c.d.QuoteIdent(info.ForeignTable) + " WHERE " +
		where + ")", nil
}

// existsThrough renders a many to many, whose two hops become two EXISTS
// rather than a join.
//
// The inner one is written whenever there is something to ask about the far
// rows: conditions the caller gave Has or HasNone, or the far table's own
// default scope. Without either, a row in the join table is already the
// answer: a foreign key means the row it names is there.
func (c *compiler) existsThrough(e Existence, info RelationInfo) (string, error) {
	join := c.sub(info.JoinTable)
	correlate := join.qualified(info.JoinTable, info.LocalJoinColumn) + " = " +
		join.qualified(info.LocalTable, info.LocalColumn)

	farPreds := scopedPreds(c, e)
	var nested []string
	if len(farPreds) > 0 {
		far := join.sub(info.ForeignTable)
		farCorrelate := far.qualified(info.ForeignTable, info.ForeignColumn) + " = " +
			far.qualified(info.JoinTable, info.ForeignJoinColumn)
		farWhere, err := far.conditions(farCorrelate, farPreds)
		if err != nil {
			return "", err
		}
		nested = append(nested, "EXISTS (SELECT 1 FROM "+
			c.d.QuoteIdent(info.ForeignTable)+" WHERE "+farWhere+")")
	}

	where, err := join.conditionsWith(correlate, nil, nested)
	if err != nil {
		return "", err
	}
	return "EXISTS (SELECT 1 FROM " + c.d.QuoteIdent(info.JoinTable) + " WHERE " +
		where + ")", nil
}

// scopedPreds is what Has and HasNone actually test against: the caller's
// own conditions, plus the related table's default scope, unless the outer
// query was Unscoped. The join table in a many to many carries no scope of
// its own here; only the far, related table's does.
//
// lookupTable failing is unreachable here: existence, the only caller, has
// already called e.Rel.info(), which looks up the same table by the same
// entity and fails first if it is not registered. It is checked anyway
// rather than assumed, since a lookup that can fail should never be treated
// as one that cannot.
func scopedPreds(c *compiler, e Existence) []Predicate {
	if c.unscoped {
		return e.Preds
	}
	related, ok := lookupTable(e.Rel.entity)
	if !ok {
		return e.Preds
	}
	scope := related.defaultScope()
	if scope == nil {
		return e.Preds
	}
	return append(append([]Predicate(nil), e.Preds...), scope)
}

// conditions joins a subquery's correlation to the caller's own conditions.
func (c *compiler) conditions(correlate string, preds []Predicate) (string, error) {
	return c.conditionsWith(correlate, preds, nil)
}

// conditionsWith is conditions, plus clauses already rendered.
func (c *compiler) conditionsWith(correlate string, preds []Predicate, rendered []string) (string, error) {
	parts := append([]string{correlate}, rendered...)
	for _, p := range preds {
		s, err := c.predicate(p)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " AND "), nil
}

// set renders an UPDATE's SET clause from a list of assignments.
//
// Column names are written unqualified, deliberately and not merely because
// nothing qualifies them yet: SQL is not consistent about where a
// qualification is allowed, and Postgres rejects a table-qualified column on
// the left of a SET outright. So the ownership check goes through c.column,
// whose error names the mistake, while the rendering does not use what it
// returns. When c.column learns to qualify for a statement over more than
// one table, this clause must not follow it.
//
// Values go through c.value, so an assignment to a document column is
// encoded exactly as one in a predicate is, and as the row itself is on the
// way back out.
func (c *compiler) set(sets []Assignment) (string, error) {
	parts := make([]string, len(sets))
	for i, a := range sets {
		if a.Col == nil {
			return "", fmt.Errorf("orm: table %q: assignment %d names no column",
				c.table, i)
		}
		if _, err := c.column(a.Col); err != nil {
			return "", err
		}
		v, err := c.value(a.Col, a.Value)
		if err != nil {
			return "", err
		}
		parts[i] = c.d.QuoteIdent(a.Col.Name()) + " = " + c.args.bind(v)
	}
	return strings.Join(parts, ", "), nil
}

// rowsPerStatement returns how many of total rows one statement may carry
// when each row binds perRow parameters.
//
// A statement that binds a value per column per row is the only place a
// caller's own input decides how many parameters are sent, so it is the only
// place the dialect's ceiling can be reached. Splitting to stay under it is
// what turns a driver error naming a number nobody chose into several
// statements nobody has to think about.
//
// A single row that already exceeds the ceiling cannot be split any further,
// so one row per statement is attempted and the database is left to report
// what it makes of a row that wide.
func rowsPerStatement(maxParams, perRow, total int) int {
	if maxParams <= 0 || perRow <= 0 {
		return total
	}
	switch n := maxParams / perRow; {
	case n < 1:
		return 1
	case n > total:
		return total
	default:
		return n
	}
}

// where renders a WHERE clause, or "" when there is nothing to filter on.
//
// Predicates are joined with AND, which is what passing several to Where
// means. A filter that compiles to the always true condition is dropped
// rather than emitted, since it reads as noise in generated SQL and the
// statement means the same without it.
func (c *compiler) where(preds []Predicate) (string, error) {
	if len(preds) == 0 {
		return "", nil
	}
	s, err := c.group(Group{Conj: ConjAnd, Preds: preds})
	if err != nil {
		return "", err
	}
	if s == sqlTrue {
		return "", nil
	}
	return " WHERE " + s, nil
}

// orderBy renders an ORDER BY clause, or "" when nothing was ordered.
func (c *compiler) orderBy(ords []Ordering) (string, error) {
	if len(ords) == 0 {
		return "", nil
	}
	parts := make([]string, len(ords))
	for i, o := range ords {
		col, err := c.column(o.Col)
		if err != nil {
			return "", err
		}
		if o.Desc {
			col += " DESC"
		} else {
			col += " ASC"
		}
		parts[i] = col
	}
	return " ORDER BY " + strings.Join(parts, ", "), nil
}

// limitOffset renders LIMIT and OFFSET.
//
// Both are written as literals rather than bound. They are Go ints, never
// text a caller supplied, so there is nothing to inject; some databases
// reject a placeholder in either position; and a literal keeps the
// generated SQL readable, which matters because the expected SQL in tests
// is written out by hand.
func limitOffset(limit, offset *int) string {
	var b strings.Builder
	if limit != nil {
		b.WriteString(" LIMIT " + strconv.Itoa(*limit))
	}
	if offset != nil {
		b.WriteString(" OFFSET " + strconv.Itoa(*offset))
	}
	return b.String()
}

// selectList renders the columns a read covers, in the order the scanner
// reads them back in.
//
// It validates them, which it did not have to while the list was always the
// table's own: those belong to the table by construction. A projection is a
// caller's list, so a column from another table can reach here and has to be
// reported rather than compiled into a reference the statement cannot resolve.
//
// The names come from c.column rather than being quoted here, so a list
// rendered inside a subquery is qualified along with the rest of it. A
// statement over one table qualifies nothing, which is every read but that one.
func (c *compiler) selectList(cols []ColumnMeta) (string, error) {
	parts := make([]string, len(cols))
	for i, col := range cols {
		name, err := c.column(col)
		if err != nil {
			return "", err
		}
		parts[i] = name
	}
	return strings.Join(parts, ", "), nil
}
