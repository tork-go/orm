package orm

import (
	"fmt"
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
	return c.d.QuoteIdent(col.Name()), nil
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

// selectList renders a table's columns in declaration order, which is the
// order the scanner reads them back in.
//
// These are the statement's own columns, so unlike a caller's predicate
// there is nothing to validate: they belong to the table by construction.
func (c *compiler) selectList(cols []ColumnMeta) string {
	parts := make([]string, len(cols))
	for i, col := range cols {
		parts[i] = c.d.QuoteIdent(col.Name())
	}
	return strings.Join(parts, ", ")
}
