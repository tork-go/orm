package orm

// DB is a database handle: somewhere to run statements, and the dialect
// that says how to write them.
//
// It pairs the two because neither is any use alone. A query has to be
// compiled before it can run, and compiling needs to know how the database
// quotes an identifier and spells a placeholder, which is exactly what
// QueryDialect answers.
//
// The statement surface is an Execer, which both a connection and an open
// transaction satisfy, so a DB can stand for either. That is what will let
// a transaction hand its callback a DB the rest of the API cannot tell
// apart from the outer one.
type DB struct {
	ex Execer
	d  QueryDialect

	// conn is the connection this handle was built from, and is nil when
	// the handle is bound to a transaction. Transactions are not built
	// yet; the field is here because Begin lives on Conn rather than on
	// Execer, so a handle that has forgotten its connection could never
	// start one.
	conn Conn
}

// NewDB pairs a connection with the dialect for the database it speaks to.
//
//	db := orm.NewDB(conn, postgres.Dialect{})
//
// The dialect is passed rather than asked for, because a Conn is only a
// way to run statements and says nothing about the SQL they are written
// in. A driver's Dialect satisfies QueryDialect, so the same value serves
// both migrations and queries.
func NewDB(conn Conn, d QueryDialect) *DB {
	return &DB{ex: conn, d: d, conn: conn}
}
