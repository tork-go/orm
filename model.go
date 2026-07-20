package orm

import "reflect"

// ColumnMeta is the read-only view of a column that Column[T] and
// ForeignKey[T] satisfy for every T, since none of their methods mention T
// in a parameter or return type. It lets code outside this package walk a
// model's fields without knowing each field's concrete T.
type ColumnMeta interface {
	Name() string
	IsPrimaryKey() bool
	IsUnique() bool
	IsNotNull() bool
	MaxLength() (int, bool)
	GoType() reflect.Type
	IsNullable() bool
	IsIndexed() bool
	ServerDefaultExpr() (string, bool)
	IsClientGenerated() bool
}

// ForeignKeyMeta is the read-only view of a foreign key column.
type ForeignKeyMeta interface {
	ColumnMeta
	ReferencedTable() string
	ReferencedColumn() string
}

// Model is any type with a table name, satisfied by embedding Table.
type Model interface {
	TableName() string
}

// Columns returns every field of m that is a column, in struct field
// order. Foreign key fields are columns too (ForeignKey[T] embeds
// Column[T]) and appear here as well as in ForeignKeys.
func Columns(m Model) []ColumnMeta {
	return walkFields[ColumnMeta](m)
}

// ForeignKeys returns every field of m that is a foreign key column.
func ForeignKeys(m Model) []ForeignKeyMeta {
	return walkFields[ForeignKeyMeta](m)
}

// walkFields reflects over m's exported struct fields and collects the
// ones satisfying I. Non-column fields (the embedded Table, HasMany,
// BelongsTo) are skipped naturally: they don't satisfy ColumnMeta or
// ForeignKeyMeta, so the type assertion below just fails for them.
func walkFields[I any](m Model) []I {
	v := reflect.ValueOf(m)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}

	var out []I
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if !t.Field(i).IsExported() {
			continue
		}
		fv := v.Field(i)
		if fv.Kind() == reflect.Pointer && fv.IsNil() {
			continue
		}
		if item, ok := fv.Interface().(I); ok {
			out = append(out, item)
		}
	}
	return out
}
