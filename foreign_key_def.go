package orm

// ForeignKeyDef declares a foreign key over one or more columns.
//
// A single column key is better written with References on the column
// itself, which reads closer to where it applies. This exists for the
// composite case, where a key spans several columns and no one column owns
// it, the same way IndexDef exists for an index over several columns.
//
// Column order is significant and must line up on both sides: the first
// column references the first referenced column, and so on.
//
//	func (m *OrderLineModel) ForeignKeys() []orm.ForeignKeyDef {
//	    return []orm.ForeignKeyDef{
//	        orm.NewForeignKeyDef(m.OrgID, m.OrderID).
//	            References(Orders.OrgID, Orders.ID).
//	            OnDelete(orm.ActionCascade),
//	    }
//	}
//
// Like IndexDef and CheckDef it is a value type with value receivers,
// since it is only ever returned from a method rather than stored as a
// struct field.
type ForeignKeyDef struct {
	name       string
	columns    []ColumnMeta
	refTable   string
	refColumns []string
	onDelete   ForeignKeyAction
	onUpdate   ForeignKeyAction
}

// NewForeignKeyDef declares a foreign key over columns, in order.
func NewForeignKeyDef(columns ...ColumnMeta) ForeignKeyDef {
	return ForeignKeyDef{columns: columns}
}

// References names the columns this key points at, in the same order as
// the key's own columns.
//
// The referenced table is read from the columns themselves, so it cannot
// disagree with them. They must therefore come from a model declared with
// DefineTable, which is what binds a column to its table; use
// ReferencesTable for a target that has none.
func (d ForeignKeyDef) References(columns ...ColumnMeta) ForeignKeyDef {
	names := make([]string, len(columns))
	for i, c := range columns {
		names[i] = c.Name()
		if i == 0 {
			d.refTable = c.OwnerTable()
		}
	}
	d.refColumns = names
	return d
}

// ReferencesTable names the referenced table and columns directly, for a
// target References cannot name: a table managed outside this program, or
// a model built with NewTable, whose columns are never bound to a table.
func (d ForeignKeyDef) ReferencesTable(table string, columns ...string) ForeignKeyDef {
	d.refTable = table
	d.refColumns = columns
	return d
}

// OnDelete sets the referential action for a deleted referenced row.
func (d ForeignKeyDef) OnDelete(action ForeignKeyAction) ForeignKeyDef {
	d.onDelete = action
	return d
}

// OnUpdate sets the referential action for an updated referenced row.
func (d ForeignKeyDef) OnUpdate(action ForeignKeyAction) ForeignKeyDef {
	d.onUpdate = action
	return d
}

// Named overrides the derived constraint name. Leave it alone unless the
// database already has the constraint under a different name: the derived
// name is deterministic, so two people declaring the same key get the same
// one.
func (d ForeignKeyDef) Named(name string) ForeignKeyDef {
	d.name = name
	return d
}

// Name returns the overriding name set by Named, or "" when the name is to
// be derived.
func (d ForeignKeyDef) Name() string { return d.name }

// Columns returns the key's own columns, in order.
func (d ForeignKeyDef) Columns() []ColumnMeta { return d.columns }

// ReferencedTable returns the table this key points at.
func (d ForeignKeyDef) ReferencedTable() string { return d.refTable }

// ReferencedColumns returns the columns this key points at, in order.
func (d ForeignKeyDef) ReferencedColumns() []string { return d.refColumns }

// OnDeleteAction returns the action set by OnDelete.
func (d ForeignKeyDef) OnDeleteAction() ForeignKeyAction { return d.onDelete }

// OnUpdateAction returns the action set by OnUpdate.
func (d ForeignKeyDef) OnUpdateAction() ForeignKeyAction { return d.onUpdate }

// ForeignKeyer is the optional interface a model implements to declare
// foreign keys that span more than one column.
//
// Single column keys need nothing here: References on the column says the
// same thing closer to where it applies.
type ForeignKeyer interface {
	ForeignKeys() []ForeignKeyDef
}
