package orm

import (
	"context"
	"fmt"
	"iter"
	"reflect"
)

// Each streams the matching rows one at a time, instead of returning them
// all at once the way All does.
//
//	for user, err := range Users.With(db).Where(Users.Active.Equals(true)).Each(ctx) {
//	    if err != nil {
//	        return err
//	    }
//	    process(user)
//	}
//
// All builds a slice holding every row, so a query matching a million rows
// is a million *User in memory before the first one is looked at. Each holds
// one at a time: it keeps the result set open and reads the next row only
// when the loop asks for it.
//
// The error is the second range value rather than something returned, since a
// range-over-func has nowhere to return one to. It is yielded once, with a nil
// row, and iteration then stops, so a loop that checks it each step sees every
// failure exactly as All would surface it: a scan that could not decode a row,
// a hook that refused it, or a connection lost partway through.
//
// Breaking out of the loop closes the result set, so an early exit leaks
// nothing. That is what makes Each safe to use with a row lock inside a
// transaction: the rows it has read stay locked until the transaction ends,
// and reading them a batch at a time is exactly the shape a work queue wants.
func (q *Query[E]) Each(ctx context.Context) iter.Seq2[*E, error] {
	return q.filtered().Each(ctx)
}

// Each streams the matching rows one at a time. See Query.Each.
func (f *Filtered[E]) Each(ctx context.Context) iter.Seq2[*E, error] {
	return func(yield func(*E, error) bool) {
		sql, args, err := f.compileSelect()
		if err != nil {
			yield(nil, err)
			return
		}
		// A load cannot stream. Eager loading collects every parent key, then
		// runs a statement per relationship over the whole set, which is the
		// row set in memory that Each exists to avoid. Refusing it names the
		// conflict rather than quietly ignoring the Load and returning rows
		// with empty relationships.
		if len(f.loads) > 0 {
			yield(nil, fmt.Errorf("orm: table %q: Each cannot stream a query with Load, "+
				"since eager loading needs every row in hand at once; use All", f.st.name))
			return
		}
		rows, err := f.db.ex.Query(ctx, sql, args...)
		if err != nil {
			yield(nil, fmt.Errorf("orm: table %q: %w", f.st.name, err))
			return
		}
		defer rows.Close()

		cols := f.columns()
		for rows.Next() {
			e := new(E)
			if err := scanRowInto(f.st, rows, reflect.ValueOf(e).Elem(), cols); err != nil {
				yield(nil, err)
				return
			}
			if err := runHook(ctx, f.st.name, "AfterLoad", any(e), AfterLoader.AfterLoad); err != nil {
				yield(nil, err)
				return
			}
			if !yield(e, nil) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(nil, fmt.Errorf("orm: table %q: reading rows: %w", f.st.name, err))
		}
	}
}

// Each streams the matching values one at a time, the single-column
// counterpart to All. See Query.Each for the shape and the error handling.
//
//	for name, err := range orm.Select(Users.With(db), Users.Username).Each(ctx) {
//	    ...
//	}
func (s *Scalars[T]) Each(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		sql, args, _, err := s.compile()
		if err != nil {
			yield(zero, err)
			return
		}
		rows, err := s.q.db.ex.Query(ctx, sql, args...)
		if err != nil {
			yield(zero, fmt.Errorf("orm: table %q: %w", s.q.st.name, err))
			return
		}
		defer rows.Close()

		for rows.Next() {
			v, err := s.scan(rows)
			if err != nil {
				yield(zero, err)
				return
			}
			if !yield(v, nil) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(zero, fmt.Errorf("orm: table %q: reading rows: %w", s.q.st.name, err))
		}
	}
}
