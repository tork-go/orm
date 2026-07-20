// Package orm provides Tork ORM's driver-agnostic model declaration
// primitives: Table, Column, ForeignKey, HasMany, and BelongsTo.
//
// This package has no knowledge of any specific database and imports no
// driver code. PostgreSQL is the first driver (see driver/postgres), with
// SQLite, MySQL, and others planned as sibling packages under driver/.
// Adding a new driver should never require changes here.
package orm
