package orm

// IndexDef declares one table-level index or unique constraint spanning
// one or more columns, for a compound (multi-column) index or unique
// constraint that a single column's Index/Unique builder can't express.
// Columns are referenced via ColumnMeta, the same vocabulary the rest of
// this package's reflection already uses. See Indexer.
type IndexDef struct {
	name        string
	unique      bool
	columns     []ColumnMeta
	expressions []string
	where       string
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

// On adds expression keys, such as lower(email), to the index.
//
// An index is over columns or over expressions, not a mix. Postgres allows
// interleaving them, but nothing in schema.Index records where each key
// sat, so an interleaved index could not be read back and compared. Use
// one or the other.
//
// The expressions are raw SQL and are rendered as written.
func (d IndexDef) On(expressions ...string) IndexDef {
	d.expressions = append(append([]string(nil), d.expressions...), expressions...)
	return d
}

// Where makes this a partial index, covering only rows matching predicate.
//
// The predicate is raw SQL and is rendered as written, the same way a
// CheckDef's expression is.
func (d IndexDef) Where(predicate string) IndexDef {
	d.where = predicate
	return d
}

// Expressions returns the expression keys added by On.
func (d IndexDef) Expressions() []string { return d.expressions }

// WherePredicate returns the predicate set by Where, empty for a full
// index.
func (d IndexDef) WherePredicate() string { return d.where }
