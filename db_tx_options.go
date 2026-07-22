package orm

// Isolation is how much of other transactions' work a transaction may see.
//
// The levels are the ones SQL names, weakest first. A database's own default
// is whatever it says it is — Postgres reads committed, most others the same
// — and IsolationDefault asks for that rather than naming one, so a
// statement carries no isolation clause at all unless a caller wanted one.
type Isolation int

const (
	// IsolationDefault leaves the level to the database.
	IsolationDefault Isolation = iota

	// IsolationReadCommitted sees every other transaction's committed work,
	// including work committed after this one started. Two identical reads
	// in one transaction may therefore disagree.
	IsolationReadCommitted

	// IsolationRepeatableRead sees the database as it was when this
	// transaction started, so a row read twice reads the same both times.
	// A write over a row another transaction changed since then fails
	// rather than overwriting it.
	IsolationRepeatableRead

	// IsolationSerializable behaves as though the transactions ran one
	// after another. It is the level that makes read-then-write logic safe
	// without locking anything by hand, and the level that repays a retry:
	// the database detects the conflict and fails one of the pair rather
	// than letting both commit. See TxOptions.Retries.
	IsolationSerializable
)

// String returns the level as SQL spells it, which is what a dialect writes
// and what an error naming a level reads as.
func (i Isolation) String() string {
	switch i {
	case IsolationReadCommitted:
		return "READ COMMITTED"
	case IsolationRepeatableRead:
		return "REPEATABLE READ"
	case IsolationSerializable:
		return "SERIALIZABLE"
	}
	return "DEFAULT"
}

// TxOptions is what a transaction is opened with.
//
// The zero value asks for nothing: the database's own isolation level, a
// transaction that may write, and no retry. That is what Transaction runs
// with, so the two differ only in whether the caller had something to say.
type TxOptions struct {
	// Isolation is how much of other transactions' work this one may see.
	Isolation Isolation

	// ReadOnly declares that the transaction writes nothing, which lets the
	// database refuse a write rather than discover it, and lets it take
	// cheaper locks — a read-only transaction blocks nobody.
	ReadOnly bool

	// Retries is how many times to run fn again after the database refuses
	// to commit because of a conflict with another transaction it could not
	// resolve. Zero, the default, runs it once.
	//
	// It is worth setting only under IsolationSerializable, and there it is
	// close to required: serializable is defined by refusing to commit a
	// transaction that would break the illusion of running alone, so a
	// caller who never retries has only moved the problem. Each attempt
	// starts a fresh transaction, so fn must be safe to run more than once —
	// which is exactly the condition it already has to satisfy to be
	// retryable at all.
	Retries int
}
