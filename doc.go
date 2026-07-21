// Package orm provides Tork ORM's driver-agnostic model declaration
// primitives: Table, Column, the typed column types, and the
// relationship markers.
//
// Columns come in two forms. Column[T] is the generic carrier every column
// is built on, and accepts every builder for every T because Go cannot add
// a method to only some instantiations of a generic type. The typed column
// types (StringColumn, IntColumn, NullableStringColumn, ...) wrap it and
// expose only the builders and query operations their kind supports, so
// MaxLen on an integer column or Contains on a boolean one is a compile
// error rather than a rule enforced later. Both satisfy ColumnMeta, so
// schema extraction treats them identically.
//
// Predicates built from those operations (see Predicate) are pure data.
// Constructing one renders no SQL and cannot fail, which is what keeps
// this package free of dialect knowledge.
//
// This package has no knowledge of any specific database and imports no
// driver code. PostgreSQL is the first driver (see driver/postgres), with
// SQLite, MySQL, and others planned as sibling packages under driver/.
// Adding a new driver should never require changes here.
package orm
