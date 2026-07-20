package schema

import (
	"fmt"
	"strings"
)

// PrimaryKeyConstraintName returns the deterministic name used for a
// table's primary key constraint when we create one.
func PrimaryKeyConstraintName(table string) string {
	return "pk_" + table
}

// UniqueConstraintName returns the deterministic name used for a unique
// constraint on the given columns when we create one.
func UniqueConstraintName(table string, columns []string) string {
	return "uq_" + table + "_" + strings.Join(columns, "_")
}

// ForeignKeyConstraintName returns the deterministic name used for a
// foreign key constraint on the given columns when we create one.
func ForeignKeyConstraintName(table string, columns []string) string {
	return "fk_" + table + "_" + strings.Join(columns, "_")
}

// IndexName returns the deterministic name used for a plain (non-unique)
// index on the given columns when we create one.
func IndexName(table string, columns []string) string {
	return "ix_" + table + "_" + strings.Join(columns, "_")
}

// CheckConstraintName returns the deterministic name used for the n-th
// (1-based) unnamed CHECK constraint on table, in Checks() declaration
// order. Unlike every other name function here, this is positional, not
// derived from a column set: a CHECK expression has no natural column
// list to name it from. See orm.Checker's doc comment.
func CheckConstraintName(table string, n int) string {
	return fmt.Sprintf("ck_%s_%d", table, n)
}
