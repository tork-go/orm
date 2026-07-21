package orm

import "reflect"

// IdentityColumn returns the column whose value the database generates,
// and whether the table has one.
//
// The rule is a sole primary key of an integer type with no client side
// generator. Every driver Tork targets renders such a key as an
// auto-incrementing identity, so a caller never supplies its value: a
// composite key names several columns and cannot be one, a non-integer key
// has nothing to increment, and a key with GeneratedByClient is generated
// in Go rather than by the database.
//
// The same rule is applied twice more, over the schema representation
// rather than over columns: driver/postgres decides which column to render
// as GENERATED ALWAYS AS IDENTITY, and schema rejects a server default on
// one, because Postgres allows a column one or the other and not both. The
// three cannot share an implementation, since the schema layer works from
// schema.Column and this works from ColumnMeta, so a test asserts they
// agree for the same model rather than leaving them to drift.
func IdentityColumn(m Model) (ColumnMeta, bool) {
	return identityOf(Columns(m))
}

func identityOf(cols []ColumnMeta) (ColumnMeta, bool) {
	var pk []ColumnMeta
	for _, c := range cols {
		if c.IsPrimaryKey() {
			pk = append(pk, c)
		}
	}
	if len(pk) != 1 {
		return nil, false
	}
	c := pk[0]
	if c.IsClientGenerated() || !isIntegerKind(c.GoType()) {
		return nil, false
	}
	return c, true
}

// isIntegerKind reports whether t is one of the Go types that reaches an
// integer column: int and int32 become INTEGER, int64 becomes BIGINT.
// A pointer is not one, since a generated key is never optional.
func isIntegerKind(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Int, reflect.Int32, reflect.Int64:
		return true
	}
	return false
}
