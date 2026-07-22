package postgres

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/tork-go/orm"
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

// RenderLock returns Postgres's row locking clause.
//
// Postgres has four strengths, of which these are the two worth naming: FOR
// UPDATE, which blocks everything, and FOR SHARE, which blocks writers. The
// other two, FOR NO KEY UPDATE and FOR KEY SHARE, differ only in how they
// interact with foreign key checks, and are what Postgres itself takes on a
// caller's behalf rather than something to reach for.
func (Dialect) RenderLock(mode orm.LockMode, wait orm.LockWait) (string, error) {
	var b string
	switch mode {
	case orm.LockUpdate:
		b = "FOR UPDATE"
	case orm.LockShare:
		b = "FOR SHARE"
	default:
		return "", fmt.Errorf("postgres: unknown lock mode %d", mode)
	}
	switch wait {
	case orm.LockBlock:
		return b, nil
	case orm.LockSkip:
		return b + " SKIP LOCKED", nil
	case orm.LockNoWait:
		return b + " NOWAIT", nil
	}
	return "", fmt.Errorf("postgres: unknown lock wait %d", wait)
}

// RenderJSONHasKey returns Postgres's jsonb key-existence operator. The `?`
// is the operator, not a parameter marker: pgx numbers its parameters `$1`
// and passes `?` through untouched, so the two do not collide.
func (Dialect) RenderJSONHasKey(quotedColumn, keyPlaceholder string) (string, error) {
	return quotedColumn + " ? " + keyPlaceholder, nil
}

// RenderJSONContains returns Postgres's jsonb containment operator. The value
// is cast to jsonb because it arrives as text: `@>` wants jsonb on both sides,
// and text casts to it where the driver's binary type would not.
func (Dialect) RenderJSONContains(quotedColumn, valuePlaceholder string) (string, error) {
	return quotedColumn + " @> " + valuePlaceholder + "::jsonb", nil
}

// RenderJSONKey returns the comparison of a top-level key's text against a
// value, using ->> to extract text so the value compares as text.
func (Dialect) RenderJSONKey(quotedColumn, keyPlaceholder string, op orm.Operator, valuePlaceholder string) (string, error) {
	return "(" + quotedColumn + " ->> " + keyPlaceholder + ") " + op.String() + " " + valuePlaceholder, nil
}

// RenderArrayContains returns Postgres's array containment. The placeholder
// binds a whole array, so it is the operator's right operand directly, with no
// ARRAY constructor whose element type Postgres would infer as text.
func (Dialect) RenderArrayContains(quotedColumn, placeholder string) (string, error) {
	return quotedColumn + " @> " + placeholder, nil
}

// RenderArrayOverlaps returns Postgres's array overlap operator.
func (Dialect) RenderArrayOverlaps(quotedColumn, placeholder string) (string, error) {
	return quotedColumn + " && " + placeholder, nil
}

// RenderArrayLength returns the comparison of an array's element count,
// counted with cardinality rather than array_length.
//
// cardinality is 0 for an empty array where array_length(col, 1) is NULL, so
// Len().Eq(0) means what it says and Len().Gt(0) is "non-empty" rather than a
// three-valued unknown.
func (Dialect) RenderArrayLength(quotedColumn string, op orm.Operator, placeholder string) (string, error) {
	return "cardinality(" + quotedColumn + ") " + op.String() + " " + placeholder, nil
}

// RenderFullText returns Postgres's full-text match, parsing the query with
// websearch_to_tsquery.
//
// That parser is the one that accepts what a search box contains — quoted
// "phrases", -exclusions, and or — and returns an empty query rather than an
// error on malformed input, where to_tsquery raises a syntax error on a stray
// operator. Both it and to_tsvector use the database's default text search
// configuration, so the language is one the DBA sets rather than one fixed
// here. For the full to_tsquery operator vocabulary, reach for orm.Raw.
func (Dialect) RenderFullText(quotedColumn, placeholder string) (string, error) {
	return "to_tsvector(" + quotedColumn + ") @@ websearch_to_tsquery(" + placeholder + ")", nil
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
