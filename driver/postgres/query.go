package postgres

import (
	"strconv"
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
