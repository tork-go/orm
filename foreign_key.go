package orm

// ForeignKey is a Column that also references a column in another table.
// It embeds Column[T], so all of Column's builder methods and read
// accessors apply to foreign key columns too.
type ForeignKey[T any] struct {
	Column[T]
	refTableName  string
	refColumnName string
	onDelete      ForeignKeyAction
	onUpdate      ForeignKeyAction
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

// The methods below redeclare Column[T]'s chain-returning builder methods
// on ForeignKey[T]. Without them, fk.NotNull() would still compile through
// promotion, but return *Column[T] instead of *ForeignKey[T], breaking the
// chain. Every chain-returning builder is overridden so none of them
// silently changes the chain's type. Read accessors that return something
// other than Self (MaxLength, IsIndexed, GoType, ...) need no override:
// promotion already returns the right type for those.

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

// Index marks the foreign key column as having a plain index.
func (fk *ForeignKey[T]) Index() *ForeignKey[T] {
	fk.Column.Index()
	return fk
}

// ServerDefault stores a raw SQL expression for the foreign key column's
// default value.
func (fk *ForeignKey[T]) ServerDefault(expr string) *ForeignKey[T] {
	fk.Column.ServerDefault(expr)
	return fk
}

// GeneratedByClient stores the foreign key column's Go-side value
// generator.
func (fk *ForeignKey[T]) GeneratedByClient(gen func() T) *ForeignKey[T] {
	fk.Column.GeneratedByClient(gen)
	return fk
}

// Numeric sets the foreign key column's explicit precision and scale.
func (fk *ForeignKey[T]) Numeric(precision, scale int) *ForeignKey[T] {
	fk.Column.Numeric(precision, scale)
	return fk
}

// JSON marks the foreign key column as stored as JSON.
func (fk *ForeignKey[T]) JSON() *ForeignKey[T] {
	fk.Column.JSON()
	return fk
}

// JSONB marks the foreign key column as stored as JSONB.
func (fk *ForeignKey[T]) JSONB() *ForeignKey[T] {
	fk.Column.JSONB()
	return fk
}

// Serialize overrides the foreign key column's default marshal/unmarshal
// pair.
func (fk *ForeignKey[T]) Serialize(marshal func(T) ([]byte, error), unmarshal func([]byte) (T, error)) *ForeignKey[T] {
	fk.Column.Serialize(marshal, unmarshal)
	return fk
}

// Enum declares the foreign key column as a Postgres native enum.
func (fk *ForeignKey[T]) Enum(typeName string, values ...string) *ForeignKey[T] {
	fk.Column.Enum(typeName, values...)
	return fk
}

// OnDelete and OnUpdate are new, FK-only builders, not promoted or
// overridden Column[T] methods, so they need no covariant-override
// counterpart there.

// OnDelete sets the referential action Postgres runs when the referenced
// row is deleted.
func (fk *ForeignKey[T]) OnDelete(action ForeignKeyAction) *ForeignKey[T] {
	fk.onDelete = action
	return fk
}

// OnUpdate sets the referential action Postgres runs when the referenced
// row is updated.
func (fk *ForeignKey[T]) OnUpdate(action ForeignKeyAction) *ForeignKey[T] {
	fk.onUpdate = action
	return fk
}

// OnDeleteAction returns the action set by OnDelete, ActionNoAction if
// never called.
func (fk *ForeignKey[T]) OnDeleteAction() ForeignKeyAction {
	return fk.onDelete
}

// OnUpdateAction returns the action set by OnUpdate, ActionNoAction if
// never called.
func (fk *ForeignKey[T]) OnUpdateAction() ForeignKeyAction {
	return fk.onUpdate
}
