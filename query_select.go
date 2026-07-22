package orm

import (
	"context"
	"fmt"
)

// Select reads one column, typed.
//
//	names, err := orm.Select(Users.With(db), Users.Username).All(ctx)      // []string
//	emails, err := orm.Select(Users.With(db), Users.Email).All(ctx)        // []*string
//
//	countries, err := orm.Select(Users.With(db).Where(Users.Active.Equals(true)),
//	    Users.Country).Distinct().All(ctx)
//
// It is a function rather than a method because the result's type is the
// column's, not the row's, and a Go method cannot introduce a type parameter
// of its own. That is also why it is worth having: Filtered.Select narrows
// which columns are read but still hands back a *E with the rest left zero,
// where this hands back exactly the values asked for and nothing to mistake
// for data.
//
// A nullable column gives a pointer, since T is the column's Go type: a
// NullableStringColumn is a Column[*string], so this reads []*string and a
// NULL is a nil rather than an empty string.
//
// It takes a QuerySource, so narrowing before or after a Where reads the same.
func Select[T any](src QuerySource, col Ref[T]) *Scalars[T] {
	q := src.querySource()
	if col == nil {
		q.err = firstErr(q.err, fmt.Errorf("orm: table %q: Select was given no column",
			q.tableName()))
	}
	return &Scalars[T]{q: q, col: col}
}

// Scalars reads a single column as a slice of its own type.
//
// It carries the query it came from, so conditions, ordering and paging all
// still apply, and adds only what makes sense for one column.
// The column is held as a Ref[T] rather than a ColumnMeta, which is what
// makes decoding a document total: Ref[T] promises a Column[T] behind it, so
// the value that comes back is a T and there is no assertion to fail.
type Scalars[T any] struct {
	q   queryState
	col Ref[T]
}

// Distinct drops duplicate values.
func (s *Scalars[T]) Distinct() *Scalars[T] {
	out := *s
	out.q.distinct = true
	return &out
}

// SQL returns the statement this would run, and its bound arguments, without
// running it.
func (s *Scalars[T]) SQL() (string, []any, error) {
	sql, args, _, err := s.compile()
	return sql, args, err
}

// compile builds the statement, and hands back the compiler so a caller
// needing to add to it does not start a second one.
func (s *Scalars[T]) compile() (string, []any, *compiler, error) {
	if err := s.ready(); err != nil {
		return "", nil, nil, err
	}
	if err := s.q.noJoins("Select"); err != nil {
		return "", nil, nil, err
	}
	if err := s.q.noCTEs("Select"); err != nil {
		return "", nil, nil, err
	}
	c := s.q.compiler()
	list, err := c.selectList([]ColumnMeta{s.col})
	if err != nil {
		return "", nil, nil, err
	}
	sql, err := s.q.compileRead(c, list)
	if err != nil {
		return "", nil, nil, err
	}
	return sql, c.args.args, c, nil
}

// ready is the query's own check, minus the entity mapping.
//
// Reading one column scans into a T rather than into a row, so a model
// declared with NewTable can still be read from this way even though it can
// never hand back a *E.
//
// A missing column needs no check here: Select records that as the query's
// error the moment it happens, so it arrives through q.err like any other.
func (s *Scalars[T]) ready() error { return s.q.readyToRead() }

// All returns every value the query matches.
func (s *Scalars[T]) All(ctx context.Context) ([]T, error) {
	sql, args, _, err := s.compile()
	if err != nil {
		return nil, err
	}

	rows, err := s.q.db.ex.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("orm: table %q: %w", s.q.st.name, err)
	}
	defer rows.Close()

	var out []T
	for rows.Next() {
		v, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("orm: table %q: reading rows: %w", s.q.st.name, err)
	}
	return out, nil
}

// First returns the first value, or ErrNoRows when the query matched none.
func (s *Scalars[T]) First(ctx context.Context) (T, error) {
	var zero T
	narrowed := *s
	// One row is all this reads, whatever limit the query carried. The copy
	// keeps that invisible to the query it came from.
	one := 1
	narrowed.q.limit = &one

	vs, err := narrowed.All(ctx)
	if err != nil {
		return zero, err
	}
	if len(vs) == 0 {
		return zero, ErrNoRows
	}
	return vs[0], nil
}

// Count returns how many values the query matches, counting each distinct one
// once when Distinct was called.
//
// It counts the column rather than the rows, so a NULL is not counted: that is
// what COUNT of a column means in SQL, and it is the answer a caller asking
// how many values there are wants.
func (s *Scalars[T]) Count(ctx context.Context) (int64, error) {
	if err := s.ready(); err != nil {
		return 0, err
	}
	if err := s.q.noLock("Count"); err != nil {
		return 0, err
	}
	if err := s.q.noJoins("Count"); err != nil {
		return 0, err
	}
	if err := s.q.noCTEs("Count"); err != nil {
		return 0, err
	}
	c := s.q.compiler()
	name, err := c.column(s.col)
	if err != nil {
		return 0, err
	}
	where, err := c.where(s.q.effectivePreds())
	if err != nil {
		return 0, err
	}
	inner := name
	if s.q.distinct {
		inner = "DISTINCT " + name
	}
	sql := "SELECT COUNT(" + inner + ") FROM " + c.d.QuoteIdent(s.q.st.name) + where

	return scanCount(ctx, s.q.db, s.q.st.name, sql, c.args.args)
}

// SubqueryOf is a query yielding one column of T, for use inside another
// query's condition rather than being run on its own.
//
//	authors := orm.Select(Posts.With(db).Where(Posts.Published.Equals(true)), Posts.AuthorID)
//	Users.With(db).Where(Users.ID.InQuery(authors)).All(ctx)
//
// It is what InQuery and NotInQuery accept, and only this package's own query
// values satisfy it. Naming T is what makes a subquery over the wrong column a
// compile error: a query yielding usernames cannot be handed to a condition on
// an ID.
//
// A *Scalars[T] is one, so the same value that All would run is what gets
// embedded. Selecting a nullable column gives a *Scalars[*T], which is a
// SubqueryOf[*T] and so not accepted by a condition on a T; NonNull is how to
// cross that gap, and says something worth saying while it does.
type SubqueryOf[T any] interface {
	subquerySource
	subqueryOf(T)
}

func (s *Scalars[T]) subqueryOf(T) {}

// compileWithin renders this query as a subquery of another statement.
//
// It shares the outer compiler's arguments, so placeholders keep counting
// across the boundary rather than restarting and colliding, and it qualifies
// its columns, since the outer statement's table is in scope here too and an
// unqualified name would be resolved by how the two happen to be nested rather
// than by what the caller wrote.
func (s *Scalars[T]) compileWithin(outer *compiler) (string, error) {
	if s == nil {
		return "", fmt.Errorf("orm: table %q: the subquery is nil", outer.table)
	}
	if err := s.ready(); err != nil {
		return "", err
	}
	c := outer.sub(s.q.st.name)
	list, err := c.selectList([]ColumnMeta{s.col})
	if err != nil {
		return "", err
	}
	return s.q.compileRead(c, list)
}

// NonNull narrows a subquery over a nullable column to the rows where it has a
// value, and gives back one yielding T rather than *T.
//
//	authors := orm.Select(Posts.With(db), Posts.EditorID)   // *Scalars[*int]
//	Users.With(db).Where(Users.ID.NotInQuery(orm.NonNull(authors))).All(ctx)
//
// Both halves matter. The type stops a *T subquery reaching a condition on a T,
// which would otherwise need a conversion nobody would think about; and the
// added IS NOT NULL disarms SQL's worst set-membership trap, where NOT IN is
// never true if the subquery yields a single NULL, so the outer query silently
// returns nothing. Requiring this call is what turns that trap into a compile
// error whose fix is also the correct one.
func NonNull[T any](sub *Scalars[*T]) SubqueryOf[T] {
	return nonNullSubquery[T]{sub: sub}
}

// nonNullSubquery is NonNull's result: the query it was given, plus the
// condition that gives it its name.
type nonNullSubquery[T any] struct{ sub *Scalars[*T] }

func (nonNullSubquery[T]) subqueryOf(T) {}

func (n nonNullSubquery[T]) compileWithin(outer *compiler) (string, error) {
	if n.sub == nil {
		return "", fmt.Errorf("orm: table %q: NonNull was given no subquery", outer.table)
	}
	// The condition is added to a copy, so the query handed to NonNull is
	// unchanged and can still be run or embedded on its own terms.
	narrowed := *n.sub
	narrowed.q.preds = append(append([]Predicate(nil), n.sub.q.preds...),
		Nullness{Col: n.sub.col, Not: true})
	return narrowed.compileWithin(outer)
}

// scan reads one value, decoding it through the column's codec when the
// column stores a document, exactly as a row scan does.
func (s *Scalars[T]) scan(rows Rows) (T, error) {
	var zero T
	if isDocumentColumn(s.col) {
		var buf []byte
		if err := rows.Scan(&buf); err != nil {
			return zero, fmt.Errorf("orm: table %q: scanning %q: %w",
				s.q.st.name, s.col.Name(), err)
		}
		// A NULL document is the zero value, which for a nullable column is
		// the nil pointer NULL means.
		if buf == nil {
			return zero, nil
		}
		v, err := s.col.Base().unmarshalTyped(buf)
		if err != nil {
			return zero, fmt.Errorf("orm: table %q: %w", s.q.st.name, err)
		}
		return v, nil
	}

	var v T
	if err := rows.Scan(&v); err != nil {
		return zero, fmt.Errorf("orm: table %q: scanning %q: %w",
			s.q.st.name, s.col.Name(), err)
	}
	return v, nil
}

// firstErr keeps the earlier of two errors, matching Filtered.fail: a later
// failure is usually a consequence of the first.
func firstErr(existing, err error) error {
	if existing != nil {
		return existing
	}
	return err
}
