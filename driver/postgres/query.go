package postgres

import (
	"errors"
	"strconv"
	"strings"
)

// The methods here are Postgres's answers to orm.QueryDialect, the pieces
// of a query the shared compiler cannot write for itself. They sit beside
// the DDL rendering rather than in it because they answer questions about
// statements rather than about schema changes, but QuoteIdent is the same
// quoting the DDL has always used, so it delegates rather than repeating
// the rule.

// QuoteIdent double quotes an identifier, escaping any embedded double
// quotes.
func (Dialect) QuoteIdent(name string) string { return quoteIdent(name) }

// Placeholder returns Postgres's numbered parameter marker, counting from
// one.
func (Dialect) Placeholder(n int) string { return "$" + strconv.Itoa(n) }

// RenderLike returns a LIKE comparison, using Postgres's ILIKE for the
// case insensitive form.
//
// The ESCAPE clause is stated rather than left to the default. Backslash
// is already Postgres's default escape character, but saying so keeps the
// generated SQL correct if a database or a session setting says otherwise,
// and the patterns reaching here are escaped with backslash by Contains,
// StartsWith, and EndsWith.
func (Dialect) RenderLike(quotedColumn, placeholder string, caseInsensitive bool) string {
	op := " LIKE "
	if caseInsensitive {
		op = " ILIKE "
	}
	return quotedColumn + op + placeholder + ` ESCAPE '\'`
}

// SupportsReturning reports that Postgres can return the row an INSERT
// wrote, so generated values come back from the same statement.
func (Dialect) SupportsReturning() bool { return true }

// RenderUpsertDoNothing returns Postgres's ON CONFLICT DO NOTHING.
//
// With no target it covers every constraint on the table, which Postgres
// allows here and only here: the row is skipped whichever uniqueness it
// violated, and nothing has to be named because nothing is being written.
func (Dialect) RenderUpsertDoNothing(target []string) (string, error) {
	if len(target) == 0 {
		return "ON CONFLICT DO NOTHING", nil
	}
	return "ON CONFLICT (" + strings.Join(target, ", ") + ") DO NOTHING", nil
}

// RenderUpsertDoUpdate returns Postgres's ON CONFLICT ... DO UPDATE, taking
// each new value from EXCLUDED, the pseudo-table holding the row the insert
// proposed.
//
// A target is required, and Postgres is the one saying so: DO UPDATE has to
// know which existing row it is updating, and only a named constraint or
// column list identifies one.
func (Dialect) RenderUpsertDoUpdate(target, updates []string) (string, error) {
	if len(target) == 0 {
		return "", errors.New("postgres: ON CONFLICT ... DO UPDATE has to know which " +
			"columns conflict; pass them to OnConflict, or use DoNothing, which does not")
	}
	sets := make([]string, len(updates))
	for i, col := range updates {
		sets[i] = col + " = EXCLUDED." + col
	}
	return "ON CONFLICT (" + strings.Join(target, ", ") + ") DO UPDATE SET " +
		strings.Join(sets, ", "), nil
}

// MaxBindParams reports Postgres's limit of 65535 parameters per
// statement.
//
// The number is not a configurable server setting but a consequence of the
// wire protocol, which counts a Bind message's parameters in an Int16. That
// makes it the same on every Postgres a driver can reach, and worth
// splitting a statement to stay under rather than discovering from the
// error.
func (Dialect) MaxBindParams() int { return 65535 }
