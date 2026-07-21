package orm

import "reflect"

// ColumnMeta is the read-only view of a column that Column[T] and
// the typed column types satisfy for every T, since none of their methods
// mention T
// in a parameter or return type. It lets code outside this package walk a
// model's fields without knowing each field's concrete T.
type ColumnMeta interface {
	Name() string
	OwnerTable() string
	IsPrimaryKey() bool
	IsUnique() bool
	HasNotNull() bool
	MaxLength() (int, bool)
	GoType() reflect.Type
	IsNullable() bool
	IsIndexed() bool
	ServerDefaultExpr() (string, bool)
	IsClientGenerated() bool
	NumericPrecisionScale() (precision, scale int, ok bool)
	IsJSON() bool
	IsJSONB() bool
	IsSerialized() bool
	EnumSpec() (typeName string, values []string, ok bool)
}

// ForeignKeyMeta is the read-only view of a foreign key column.
type ForeignKeyMeta interface {
	ColumnMeta
	ReferencedTable() string
	ReferencedColumn() string
	OnDeleteAction() ForeignKeyAction
	OnUpdateAction() ForeignKeyAction
}

// Model is any type with a table name, satisfied by embedding Table.
type Model interface {
	TableName() string
}

// Columns returns every field of m that is a column, in struct field
// order. Foreign keys appear here too, since a key is an ordinary column
// carrying a reference rather than a kind of its own, so a referencing
// column shows up in both this and ForeignKeys.
func Columns(m Model) []ColumnMeta {
	return walkFields[ColumnMeta](m)
}

// ForeignKeys returns every field of m that references a column in
// another table, in struct field order.
//
// Every column satisfies ForeignKeyMeta, because any column may carry a
// reference, so satisfying the interface is not what makes a field a
// foreign key. Having a referenced table is. Columns that reference
// nothing report "" there and are filtered out here.
func ForeignKeys(m Model) []ForeignKeyMeta {
	all := walkFields[ForeignKeyMeta](m)
	out := make([]ForeignKeyMeta, 0, len(all))
	for _, fk := range all {
		if fk.ReferencedTable() != "" {
			out = append(out, fk)
		}
	}
	return out
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
