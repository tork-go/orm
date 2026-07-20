package orm

// ForeignKey is a Column that also references a column in another table.
// It embeds Column[T], so PrimaryKey, Unique, NotNull, MaxLen, and all
// read accessors apply to foreign key columns too.
type ForeignKey[T any] struct {
	Column[T]
	refTableName  string
	refColumnName string
}

// NewForeignKey declares a foreign key column named name that references
// refColumn in the table named refTableName. T is inferred from refColumn,
// so it is never written explicitly at the call site:
//
//	AuthorID := NewForeignKey("author_id", User.TableName(), User.ID) // T = int
func NewForeignKey[T any](name, refTableName string, refColumn *Column[T]) *ForeignKey[T] {
	return &ForeignKey[T]{
		Column:        newColumn[T](name),
		refTableName:  refTableName,
		refColumnName: refColumn.Name(),
	}
}

// ReferencedTable returns the name of the table this foreign key points to.
func (fk *ForeignKey[T]) ReferencedTable() string {
	return fk.refTableName
}

// ReferencedColumn returns the name of the column this foreign key points
// to.
func (fk *ForeignKey[T]) ReferencedColumn() string {
	return fk.refColumnName
}

// The methods below redeclare Column[T]'s builder methods on ForeignKey[T].
// Without them, fk.NotNull() would still compile through promotion, but
// return *Column[T] instead of *ForeignKey[T], breaking the chain. All
// four are overridden together so none of them silently changes the
// chain's type.

// PrimaryKey marks the foreign key column as (part of) the primary key.
func (fk *ForeignKey[T]) PrimaryKey() *ForeignKey[T] {
	fk.Column.PrimaryKey()
	return fk
}

// Unique marks the foreign key column as having a unique constraint.
func (fk *ForeignKey[T]) Unique() *ForeignKey[T] {
	fk.Column.Unique()
	return fk
}

// NotNull marks the foreign key column as disallowing SQL NULL.
func (fk *ForeignKey[T]) NotNull() *ForeignKey[T] {
	fk.Column.NotNull()
	return fk
}

// MaxLen sets the foreign key column's maximum length.
func (fk *ForeignKey[T]) MaxLen(n int) *ForeignKey[T] {
	fk.Column.MaxLen(n)
	return fk
}
