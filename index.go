package orm

// IndexDef declares one table-level index or unique constraint spanning
// one or more columns, for a compound (multi-column) index or unique
// constraint that a single column's Index/Unique builder can't express.
// Columns are referenced via ColumnMeta, the same vocabulary the rest of
// this package's reflection already uses. See Indexer.
type IndexDef struct {
	name    string
	unique  bool
	columns []ColumnMeta
}

// NewIndexDef declares a plain (non-unique) index over columns, in the
// given order. Chain Unique to declare a compound unique constraint
// instead. Name is optional: leave it unset, or call Named to override,
// to have schema.ExtractSchema auto-generate one using the same naming
// convention as every other constraint.
func NewIndexDef(columns ...ColumnMeta) IndexDef {
	return IndexDef{columns: columns}
}

// Unique marks the definition as a compound unique constraint rather than
// a plain index.
func (d IndexDef) Unique() IndexDef {
	d.unique = true
	return d
}

// Named overrides the auto-generated name. IndexDef is returned by value,
// not a pointer: unlike Column[T] and the typed column types, it's never a
// struct field
// walked by reflection, it's returned from an ordinary method call, so
// there's no shared-identity requirement forcing pointer semantics here.
func (d IndexDef) Named(name string) IndexDef {
	d.name = name
	return d
}

// Name returns the name passed to Named, or "" if it was never called.
func (d IndexDef) Name() string { return d.name }

// IsUnique reports whether Unique was called.
func (d IndexDef) IsUnique() bool { return d.unique }

// Columns returns the columns passed to NewIndexDef, in order.
func (d IndexDef) Columns() []ColumnMeta { return d.columns }

// Indexer is implemented by a model that declares table-level, multi-
// column index or unique-constraint definitions beyond what individual
// columns' Index/Unique builders express. Implementing it is optional;
// most models won't:
//
//	func (m *OrgMemberModel) Indexes() []orm.IndexDef {
//	    return []orm.IndexDef{
//	        orm.NewIndexDef(m.OrgID, m.CreatedAt),
//	        orm.NewIndexDef(m.OrgID, m.UserID).Unique().Named("uq_org_members_org_user"),
//	    }
//	}
type Indexer interface {
	Indexes() []IndexDef
}
