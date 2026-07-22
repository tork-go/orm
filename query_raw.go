package orm

import (
	"context"
	"fmt"
	"strings"
)

// Raw is a predicate written as SQL, for the conditions the typed API does
// not reach: a function call, an operator no column exposes, a database
// specific construct.
//
//	Users.With(db).Where(
//	    Users.Active.Equals(true),
//	    orm.Raw("lower(username) = ?", name),
//	).All(ctx)
//
// It is an ordinary predicate, so it goes wherever one goes: beside typed
// conditions as above, inside orm.Or and orm.Not, and in a conditional write's
// UpdateIf.
//
// Each ? binds the next argument as a parameter, rewritten to the dialect's
// own marker ($1 on Postgres, ? elsewhere) and numbered in with the rest of
// the statement's placeholders. A value in a Raw fragment is therefore as
// injection-safe as one anywhere else in the query: only the SQL text is
// written literally, and that text is yours. Write ?? for a literal question
// mark that binds nothing, which Postgres's jsonb operators are spelled with.
//
// The number of ? must match the number of arguments; a mismatch is reported
// when the statement compiles, like any other error in a predicate.
func Raw(sql string, args ...any) Predicate {
	return rawPredicate{sql: sql, args: args}
}

// rawPredicate is Raw's result: a fragment of SQL and the values its
// placeholders bind.
type rawPredicate struct {
	sql  string
	args []any
}

func (rawPredicate) predicate() {}

// raw renders a Raw fragment, rewriting each ? to the dialect's placeholder
// and binding the matching argument, so a raw fragment's values reach the
// statement as parameters exactly as a typed predicate's do.
func (c *compiler) raw(p rawPredicate) (string, error) {
	return rewriteRawPlaceholders(p.sql, p.args, c.args.bind)
}

// rewriteRawPlaceholders rewrites each ? in sql to whatever bind returns for
// the next argument, and each doubled ?? to one literal, unbound ?.
//
// This is Raw's own rewriting, factored out so RawQuery can reuse it
// against a statement's own argBuilder instead of a predicate's: the rules
// (numbering, the ?? escape, the count mismatch errors) are the same
// either way, and only what "bind" does with a value differs.
func rewriteRawPlaceholders(sql string, args []any, bind func(v any) string) (string, error) {
	var b strings.Builder
	b.Grow(len(sql))
	used := 0
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		if ch != '?' {
			b.WriteByte(ch)
			continue
		}
		// A doubled ?? is an escaped literal question mark that binds nothing:
		// it is how a fragment whose other ? are placeholders still writes
		// Postgres's jsonb ? operator.
		if i+1 < len(sql) && sql[i+1] == '?' {
			b.WriteByte('?')
			i++
			continue
		}
		if used >= len(args) {
			return "", fmt.Errorf("orm: %q: more ? placeholders than the %d "+
				"argument(s) given", sql, len(args))
		}
		b.WriteString(bind(args[used]))
		used++
	}
	if used != len(args) {
		return "", fmt.Errorf("orm: %q: %d argument(s) given but %d "+
			"? placeholder(s) to bind them to", sql, len(args), used)
	}
	return b.String(), nil
}

// RawQuery runs sql, a hand-written statement, and scans every row into a
// new T.
//
//	type Report struct {
//	    Country string
//	    Total   int64
//	}
//	rows, err := orm.RawQuery[Report](ctx, db,
//	    `SELECT country, COUNT(*) FROM users WHERE active = ? GROUP BY country`, true)
//
// This is the escape hatch orm.Raw is for a predicate, widened to a whole
// statement: for a query the typed API cannot express at all, rather than
// one condition it is missing. Each ? binds the next argument as a
// parameter and is rewritten to the dialect's own marker, exactly as it is
// inside a Where — a value here is exactly as injection-safe as one
// anywhere else in the package, and only the SQL text is written literally.
//
// T's exported fields, in declaration order, are matched positionally
// against the row's columns, the same convention every scan in this package
// follows. A document column's codec is not run here: T is never a
// declared model, so there is no column to consult for one.
func RawQuery[T any](ctx context.Context, db *DB, sql string, args ...any) ([]T, error) {
	if db == nil {
		return nil, fmt.Errorf("orm: RawQuery was given no database handle")
	}
	ab := &argBuilder{d: db.d}
	rewritten, err := rewriteRawPlaceholders(sql, args, ab.bind)
	if err != nil {
		return nil, err
	}

	rows, err := db.ex.Query(ctx, rewritten, ab.args...)
	if err != nil {
		return nil, fmt.Errorf("orm: RawQuery: %w", err)
	}
	defer rows.Close()

	var out []T
	for rows.Next() {
		v, err := scanStruct[T](rows)
		if err != nil {
			return nil, fmt.Errorf("orm: RawQuery: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("orm: RawQuery: reading rows: %w", err)
	}
	return out, nil
}
