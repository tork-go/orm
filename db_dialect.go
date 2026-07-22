package orm

import "reflect"

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

	// RenderLock returns the clause that locks the rows a SELECT reads, so
	// another transaction cannot change them until this one ends.
	//
	// Both halves of it vary. Postgres and MySQL spell the strong mode FOR
	// UPDATE and the weak one differently from each other (FOR SHARE against
	// LOCK IN SHARE MODE before MySQL 8), and SQLite locks whole databases
	// rather than rows and so has no clause at all. A dialect that cannot
	// express a mode returns an error naming it, rather than reading rows it
	// has not locked and leaving the caller to find out later.
	//
	// of is the already-quoted tables the lock is narrowed to, and is empty
	// when it covers every table the statement reads. It is passed here
	// rather than appended by the caller because it belongs between the mode
	// and the wait — FOR UPDATE OF "books" NOWAIT — which only something
	// writing the whole clause can place.
	RenderLock(mode LockMode, wait LockWait, of []string) (string, error)

	// RenderJSONHasKey returns the test that an already-quoted JSON column has
	// the given top-level key, whose already-rendered placeholder binds it.
	//
	// JSON querying is where dialects diverge most: Postgres has operators
	// (?, @>, ->>), MySQL has functions (JSON_CONTAINS, JSON_EXTRACT), and a
	// database with no JSON type at all has neither. A dialect that cannot
	// express one returns an error naming the operation.
	RenderJSONHasKey(quotedColumn, keyPlaceholder string) (string, error)

	// RenderJSONContains returns the test that an already-quoted JSON column
	// contains the JSON value its already-rendered placeholder binds, as a
	// subtree.
	RenderJSONContains(quotedColumn, valuePlaceholder string) (string, error)

	// RenderJSONKey returns the comparison of the text at a top-level key of an
	// already-quoted JSON column against a value, both placeholders already
	// rendered and the operator one of the six comparisons.
	RenderJSONKey(quotedColumn, keyPlaceholder string, op Operator, valuePlaceholder string) (string, error)

	// RenderArrayContains returns the test that an already-quoted array column
	// holds every element of the array its already-rendered placeholder binds.
	// The bound value is a whole slice, so the placeholder is one, not one per
	// element: that is what lets the driver encode it as the column's array
	// type rather than leave an ARRAY[] constructor's element type to be
	// inferred as text.
	//
	// Arrays are a native type in Postgres and absent from MySQL, so a driver
	// without them returns an error naming the operation.
	RenderArrayContains(quotedColumn, placeholder string) (string, error)

	// RenderArrayOverlaps returns the test that an already-quoted array column
	// holds any element of the array its already-rendered placeholder binds.
	RenderArrayOverlaps(quotedColumn, placeholder string) (string, error)

	// RenderArrayLength returns the comparison of an already-quoted array
	// column's element count against a value, the placeholder already rendered
	// and the operator one of the six comparisons.
	RenderArrayLength(quotedColumn string, op Operator, placeholder string) (string, error)

	// RenderFullText returns a full-text match between an already-quoted text
	// column and the query its already-rendered placeholder binds.
	//
	// It is the one operator riding on ordinary string columns rather than a
	// kind of its own, so it is the one a dialect without full-text cannot
	// avoid being asked for. Postgres, MySQL and SQLite each have a full-text
	// facility spelled differently, and a driver with none returns an error
	// naming the operation.
	RenderFullText(quotedColumn, placeholder string) (string, error)

	// RenderNullsOrder returns an already-rendered ORDER BY term — the
	// column or expression and its direction — with the placement of NULLs
	// made explicit, first when asked and last otherwise.
	//
	// Where NULLs sort unasked is not agreed on: Postgres sorts them last
	// ascending and first descending, MySQL and SQLite the reverse. Saying
	// so explicitly is spelled NULLS FIRST by Postgres and SQLite, has to be
	// emulated with a leading `col IS NULL` term in MySQL, and cannot be
	// said at all in some others, so the whole term is the dialect's to
	// rewrite rather than a suffix to append. A dialect that cannot express
	// it returns an error naming the operation, rather than sorting NULLs
	// somewhere the caller did not ask for.
	RenderNullsOrder(term string, first bool) (string, error)

	// RenderDistinctOn returns the SELECT keyword that keeps one row per
	// distinct combination of the already-quoted columns given.
	//
	// It is Postgres's DISTINCT ON, which no other database Tork targets
	// has: the same result elsewhere is a window function filtered in a
	// derived table. A dialect without it returns an error naming the
	// operation, since the alternative is a different statement rather than
	// a different spelling of this one.
	RenderDistinctOn(columns []string) (string, error)

	// RenderTransactionOptions returns the statement that applies these
	// options to the transaction just opened, or "" when there is nothing to
	// apply — which the zero TxOptions always is.
	//
	// It is a statement rather than a clause on BEGIN because that is the
	// portable half: Postgres accepts both `BEGIN ISOLATION LEVEL ...` and
	// `SET TRANSACTION ISOLATION LEVEL ...` as the first statement, while a
	// driver that opens transactions through its own client API never writes
	// the BEGIN at all. A dialect that cannot express a level returns an
	// error naming it, rather than running at a level the caller did not
	// ask for and cannot see.
	RenderTransactionOptions(opts TxOptions) (string, error)

	// IsRetryable reports whether an error is the database refusing to
	// commit because of a conflict with another transaction, which running
	// the whole transaction again may resolve.
	//
	// Only the driver can answer it: the condition is a SQLSTATE, or a
	// vendor code, or a string, depending on the database, and recognising
	// it is what separates a transaction worth retrying from one that will
	// fail the same way forever. A dialect that cannot tell reports false,
	// which retries nothing.
	IsRetryable(err error) bool

	// RenderTypedPlaceholder returns an already-rendered placeholder with
	// its type made explicit, for the few positions where the database
	// cannot infer it from anything nearby.
	//
	// Almost every bound value sits beside a column that settles what it is:
	// `"age" > $1` tells Postgres $1 is an integer. A CASE arm has no such
	// neighbour — `SUM(CASE WHEN ... THEN $1 ELSE $2 END)` gives it nothing
	// to go on, and Postgres guesses text, then fails to find a sum of it.
	// Since this package binds every value rather than writing any of them
	// literally, saying the type is the only way left.
	//
	// A dialect that infers well enough, or has no cast syntax, returns the
	// placeholder unchanged; goType may be nil, which means the same.
	RenderTypedPlaceholder(placeholder string, goType reflect.Type) string

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
