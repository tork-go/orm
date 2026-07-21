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

	// RenderUpsertDoNothing returns the clause that makes an INSERT skip a
	// row already present, given the already-quoted columns whose duplication
	// is what "already present" means. No columns means any conflict at all,
	// which some databases can express and others cannot.
	//
	// The whole clause is the dialect's to write, not a token or two of it:
	// Postgres and SQLite spell it ON CONFLICT, MySQL has no target at all
	// and says ON DUPLICATE KEY, and SQL Server has neither and needs a
	// MERGE. A dialect that cannot express it returns an error naming the
	// operation rather than emitting something close.
	RenderUpsertDoNothing(target []string) (string, error)

	// RenderUpsertDoUpdate returns the clause that makes an INSERT overwrite
	// the given already-quoted columns of a row already present, with the
	// values the insert was carrying for it.
	RenderUpsertDoUpdate(target, updates []string) (string, error)

	// MaxBindParams reports how many parameters one statement may bind, or
	// 0 when the database sets no practical limit.
	//
	// It exists for the writes that bind a value per column per row, where
	// the caller decides how many rows and so how close the statement comes
	// to the ceiling. A bulk insert of a few thousand rows crosses it
	// easily, and the failure is a driver error naming a number rather than
	// anything a caller could act on, so the statement is split into as many
	// as it takes instead. See rowsPerStatement.
	MaxBindParams() int
}
