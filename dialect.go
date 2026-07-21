package orm

// QueryDialect is the part of a database's SQL that a query compiler
// cannot write for itself.
//
// Most of a SELECT is the same everywhere, so the compiler builds it once
// and asks the dialect only about the pieces that genuinely differ. That
// split is deliberate in both directions: writing the whole statement in
// the compiler would bake Postgres's `$1` and double quotes into the model
// layer, while handing the whole tree to each dialect would make every
// future driver reimplement a SELECT compiler to change two tokens.
//
// It lives here rather than in driver for the same reason the execution
// interfaces do, and driver.Dialect embeds it, so a driver implements one
// interface and callers still have one to pass around.
type QueryDialect interface {
	// QuoteIdent quotes a table or column name, escaping whatever the
	// database's quoting character is.
	QuoteIdent(name string) string

	// Placeholder returns the parameter marker for the n-th bound
	// argument, counting from one. Postgres numbers them, `$1`; MySQL and
	// SQLite repeat a single `?`.
	Placeholder(n int) string

	// RenderLike returns a LIKE comparison between an already-quoted
	// column and an already-rendered placeholder, case insensitively when
	// asked. Postgres spells the insensitive form ILIKE, while other
	// databases fold case in LIKE itself or need an explicit collation, so
	// the whole comparison is the dialect's to write.
	RenderLike(quotedColumn, placeholder string, caseInsensitive bool) string

	// SupportsReturning reports whether an INSERT can return the row it
	// wrote. Where it can, generated values come back from the same
	// statement; where it cannot, they have to be fetched separately.
	SupportsReturning() bool
}
