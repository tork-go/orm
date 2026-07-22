package orm

import (
	"context"
	"fmt"
	"reflect"
)

// The aggregates are functions rather than methods for the reason Select is:
// each returns a value of the column's type, not the row's, and a Go method
// cannot introduce a type parameter of its own.
//
// They take a QuerySource, so they read the same before a Where as after one,
// and they honour Distinct, since summing each distinct value once is a
// question worth being able to ask.
//
// Ordering and paging are dropped, as Count already drops them: they change
// which rows come back, not what the whole set adds up to.

// Sum returns the total of a column over the rows the query matches.
//
//	total, err := orm.Sum(ctx, Users.With(db).Where(Users.Active.Equals(true)), Users.Age)
//
// Over no rows it is the zero value, which is what a total of nothing is.
// SQL answers NULL there, which is the same statement about an empty set made
// in a different vocabulary.
func Sum[T any](ctx context.Context, src QuerySource, col Ref[T]) (T, error) {
	v, _, err := scalarAggregate[T](ctx, src, col, "SUM")
	return v, err
}

// Avg returns the mean of a column over the rows the query matches.
//
// It returns a float whatever the column holds, because a mean of integers is
// not an integer and rounding it silently would be a lie. Over no rows there
// is no mean, so it returns ErrNoRows rather than a zero that reads as one.
func Avg[T any](ctx context.Context, src QuerySource, col Ref[T]) (float64, error) {
	v, ok, err := scalarAggregate[float64](ctx, src, col, "AVG")
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrNoRows
	}
	return v, nil
}

// Min returns the smallest value of a column over the rows the query matches,
// or ErrNoRows when it matches none.
func Min[T any](ctx context.Context, src QuerySource, col Ref[T]) (T, error) {
	return extreme[T](ctx, src, col, "MIN")
}

// Max returns the largest value of a column over the rows the query matches,
// or ErrNoRows when it matches none.
//
// Unlike Sum, an extreme of no rows is not zero: there is no such value, and
// returning one would be indistinguishable from a row that genuinely holds it.
func Max[T any](ctx context.Context, src QuerySource, col Ref[T]) (T, error) {
	return extreme[T](ctx, src, col, "MAX")
}

func extreme[T any](ctx context.Context, src QuerySource, col Ref[T], fn string) (T, error) {
	v, ok, err := scalarAggregate[T](ctx, src, col, fn)
	if err != nil {
		return v, err
	}
	if !ok {
		var zero T
		return zero, ErrNoRows
	}
	return v, nil
}

// scalarAggregate runs one aggregate and reads its single value, reporting
// whether the database gave one at all.
//
// R is what comes back, which is the column's type for Sum, Min and Max and a
// float for Avg, so the column's own type parameter and the result's are
// separate.
func scalarAggregate[R any](ctx context.Context, src QuerySource, col ColumnMeta, fn string) (R, bool, error) {
	var zero R
	if src == nil {
		return zero, false, fmt.Errorf("orm: %s was given no query", fn)
	}
	q := src.querySource()
	if col == nil {
		return zero, false, fmt.Errorf("orm: table %q: %s was given no column",
			q.tableName(), fn)
	}
	if err := q.readyToRead(); err != nil {
		return zero, false, err
	}
	if err := q.noLock(fn); err != nil {
		return zero, false, err
	}
	if err := q.noJoins(fn); err != nil {
		return zero, false, err
	}
	if err := q.noCTEs(fn); err != nil {
		return zero, false, err
	}

	c := q.compiler()
	name, err := c.column(col)
	if err != nil {
		return zero, false, err
	}
	where, err := c.where(q.effectivePreds())
	if err != nil {
		return zero, false, err
	}
	inner := name
	if q.distinct {
		inner = "DISTINCT " + name
	}
	sql := "SELECT " + fn + "(" + inner + ") FROM " + c.d.QuoteIdent(q.st.name) + where

	rows, err := q.db.ex.Query(ctx, sql, c.args.args...)
	if err != nil {
		return zero, false, fmt.Errorf("orm: table %q: %w", q.st.name, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return zero, false, fmt.Errorf("orm: table %q: %s: %w", q.st.name, fn, err)
		}
		return zero, false, fmt.Errorf("orm: table %q: %s returned no row", q.st.name, fn)
	}

	v, ok, err := scanNullable[R](rows)
	if err != nil {
		return zero, false, fmt.Errorf("orm: table %q: scanning %s: %w", q.st.name, fn, err)
	}
	return v, ok, rows.Err()
}

// scanNullable reads one value that may be NULL, reporting whether R could
// represent it if it was.
//
// An aggregate over no rows answers NULL, and so does one over rows whose
// values are all NULL, so every aggregate needs a destination that can hold
// the absence of a value. Which destination that is depends on R: a pointer
// already holds it, while anything else needs one wrapped around it. Passing
// a pointer to a pointer to a pointer would be the alternative, and no driver
// accepts one.
//
// The ok it reports is not "the database gave a value" but "R can say it did
// not". For a nullable column R is a pointer, so a NULL is a nil and that is
// the answer, not the absence of one: MAX over a column whose every value is
// NULL is NULL, and a caller asking about a nullable column already has
// somewhere to put that. Only a type with no nil to spare needs the caller
// told separately, which is what makes Min and Max report ErrNoRows there and
// not here.
func scanNullable[R any](rows Rows) (R, bool, error) {
	var zero R
	if isPointerType[R]() {
		var v R
		if err := rows.Scan(&v); err != nil {
			return zero, false, err
		}
		return v, true, nil
	}

	var v *R
	if err := rows.Scan(&v); err != nil {
		return zero, false, err
	}
	if v == nil {
		return zero, false, nil
	}
	return *v, true, nil
}

// isPointerType reports whether T is a pointer, which is what decides whether
// a value of it can hold a NULL on its own.
func isPointerType[T any]() bool {
	return reflect.TypeFor[T]().Kind() == reflect.Pointer
}
