package schema

import "strings"

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
