package orm

import (
	"fmt"
	"strings"
)

// Raw is a predicate written as SQL, for the conditions the typed API does
// not reach: a function call, an operator no column exposes, a database
// specific construct.
//
//	Users.With(db).Where(
//	    Users.Active.Eq(true),
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
	var b strings.Builder
	b.Grow(len(p.sql))
	used := 0
	for i := 0; i < len(p.sql); i++ {
		ch := p.sql[i]
		if ch != '?' {
			b.WriteByte(ch)
			continue
		}
		// A doubled ?? is an escaped literal question mark that binds nothing:
		// it is how a fragment whose other ? are placeholders still writes
		// Postgres's jsonb ? operator.
		if i+1 < len(p.sql) && p.sql[i+1] == '?' {
			b.WriteByte('?')
			i++
			continue
		}
		if used >= len(p.args) {
			return "", fmt.Errorf("orm: Raw(%q): more ? placeholders than the %d "+
				"argument(s) given", p.sql, len(p.args))
		}
		b.WriteString(c.args.bind(p.args[used]))
		used++
	}
	if used != len(p.args) {
		return "", fmt.Errorf("orm: Raw(%q): %d argument(s) given but %d "+
			"? placeholder(s) to bind them to", p.sql, len(p.args), used)
	}
	return b.String(), nil
}
