// Package migrate diffs two schema.Schema values into an ordered list of
// Operations, renders them into SQL through a driver.Dialect, reads and
// writes migration files, and applies or rolls them back against a live
// database.
//
// This package is dialect-agnostic: it knows nothing about any specific
// database's SQL syntax. It talks to a database only through driver.Conn
// and driver.Dialect.
package migrate
