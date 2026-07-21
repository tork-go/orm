package orm

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// The relationship markers say that rows of one table relate to rows of
// another. They carry no data of their own and stay legal left
// uninitialised in a model literal, which is what lets a model mention a
// table declared after it.
//
// A marker names only the related row type, so working out which foreign
// key joins the two means looking that type up in the registry and reading
// the columns already declared on both sides. That happens on first use
// rather than while the tables are being declared, because pointing at the
// other table directly would make the two models depend on each other and
// Go rejects a cycle between package level variables outright.
//
// Loading related rows belongs to the query API and is not built yet. What
// is settled here is which columns a relationship joins on, which is the
// part the schema alone can answer.

// RelationKind distinguishes the four shapes a relationship can take.
type RelationKind int

const (
	KindHasMany RelationKind = iota
	KindHasOne
	KindBelongsTo
	KindManyToMany
)

// String returns the marker's name, which is what error messages use to
// point at the field a caller wrote.
func (k RelationKind) String() string {
	switch k {
	case KindHasMany:
		return "HasMany"
	case KindHasOne:
		return "HasOne"
	case KindBelongsTo:
		return "BelongsTo"
	case KindManyToMany:
		return "ManyToMany"
	}
	return "unknown"
}

// RelationInfo is a resolved relationship: the columns a join matches on,
// and the tables on either side.
type RelationInfo struct {
	Kind RelationKind

	// LocalColumn is on the declaring table and ForeignColumn on the
	// related one. A BelongsTo holds the key locally; HasMany and HasOne
	// have it on the far side.
	//
	// For a many to many neither table holds a key, so these name the two
	// columns the join table points at, and the join is made in two hops
	// through JoinTable.
	LocalColumn   ColumnMeta
	ForeignColumn ColumnMeta

	LocalTable   string
	ForeignTable string

	// JoinTable is set only for a many to many. LocalJoinColumn is the
	// join table's key into the declaring table and ForeignJoinColumn its
	// key into the related one.
	JoinTable         string
	LocalJoinColumn   ColumnMeta
	ForeignJoinColumn ColumnMeta
}

// relation is the state behind every marker, so the four differ only in
// their kind and in what they are called.
type relation struct {
	kind   RelationKind
	owner  *tableState
	entity reflect.Type

	once     sync.Once
	resolved RelationInfo
	err      error

	// localJoin and foreignJoin are the join table's two keys, set by
	// Through for a many to many.
	localJoin   ColumnMeta
	foreignJoin ColumnMeta
}

// namedKey returns the key the owning model's Relations method names for
// this relationship, or nil if it names none.
//
// Relations is read here rather than while the table was being declared
// because it routinely mentions another table, which may not have been
// initialised at that point. Defs are matched by identity against this
// relation, which is what Via captured.
func (r *relation) namedKey() (ColumnMeta, error) {
	if r.owner.relater == nil {
		return nil, nil
	}
	for _, def := range r.owner.relater.Relations() {
		if def.to != r {
			continue
		}
		if def.key == nil {
			return nil, fmt.Errorf("orm: %s.%s: Relations named a nil column",
				r.owner.name, r.kind)
		}
		return def.key, nil
	}
	return nil, nil
}

// readJoinKeys copies the join table keys named by Through onto the
// relation, for the same reason namedKey reads its key late.
func (r *relation) readJoinKeys() {
	if r.owner.relater == nil {
		return
	}
	for _, def := range r.owner.relater.Relations() {
		if def.to == r && def.joinLocal != nil && def.joinForeign != nil {
			r.localJoin, r.foreignJoin = def.joinLocal, def.joinForeign
			return
		}
	}
}

// info resolves the relationship once and gives every later caller the
// same answer, including the same failure.
func (r *relation) info() (RelationInfo, error) {
	if r == nil || r.owner == nil {
		return RelationInfo{}, fmt.Errorf("orm: relationship is not attached to a table; " +
			"declare the model with DefineTable rather than NewTable")
	}
	r.once.Do(func() {
		r.readJoinKeys()
		r.resolved, r.err = r.resolve()
	})
	return r.resolved, r.err
}

func (r *relation) resolve() (RelationInfo, error) {
	if r.kind == KindManyToMany {
		return r.resolveThrough()
	}

	related, ok := lookupTable(r.entity)
	if !ok {
		return RelationInfo{}, fmt.Errorf("orm: %s.%s: no table is declared for %s; "+
			"declare it with DefineTable before the relationship is used",
			r.owner.name, r.kind, r.entity)
	}

	// A BelongsTo owns its key, so the referencing column sits on the
	// declaring table. The other shapes are the mirror of that.
	holder, target := related, r.owner
	if r.kind == KindBelongsTo {
		holder, target = r.owner, related
	}

	key, err := r.namedKey()
	if err != nil {
		return RelationInfo{}, err
	}
	if key == nil {
		found, err := soleForeignKeyInto(holder, target.name)
		if err != nil {
			return RelationInfo{}, fmt.Errorf("orm: %s.%s -> %s: %w",
				r.owner.name, r.kind, related.name, err)
		}
		key = found
	}

	referenced := columnNamed(target, referencedColumnOf(key))
	if referenced == nil {
		return RelationInfo{}, fmt.Errorf("orm: %s.%s -> %s: the key %q references column %q, "+
			"which is not declared on %s",
			r.owner.name, r.kind, related.name, key.Name(), referencedColumnOf(key), target.name)
	}

	local, foreign := referenced, key
	if r.kind == KindBelongsTo {
		local, foreign = key, referenced
	}

	return RelationInfo{
		Kind:          r.kind,
		LocalColumn:   local,
		ForeignColumn: foreign,
		LocalTable:    r.owner.name,
		ForeignTable:  related.name,
	}, nil
}

// resolveThrough resolves a many to many from the two join table keys the
// model named with Through.
//
// Nothing in a ManyToMany[E] declaration says which table joins the two,
// so unlike the other shapes there is nothing to infer from. Naming the
// two keys settles the join table as well, since a column knows the table
// it belongs to.
func (r *relation) resolveThrough() (RelationInfo, error) {
	related, ok := lookupTable(r.entity)
	if !ok {
		return RelationInfo{}, fmt.Errorf("orm: %s.ManyToMany: no table is declared for %s; "+
			"declare it with DefineTable before the relationship is used",
			r.owner.name, r.entity)
	}
	if r.localJoin == nil || r.foreignJoin == nil {
		return RelationInfo{}, fmt.Errorf("orm: %s.ManyToMany -> %s: no join table named; "+
			"nothing in the declaration says which table joins the two, so name the "+
			"join table's two keys with Through in a Relations method",
			r.owner.name, related.name)
	}

	joinTable := r.localJoin.OwnerTable()
	if joinTable == "" || joinTable != r.foreignJoin.OwnerTable() {
		return RelationInfo{}, fmt.Errorf("orm: %s.ManyToMany -> %s: the two keys given to "+
			"Through belong to different tables (%q and %q); both must be columns of "+
			"the one join table",
			r.owner.name, related.name, joinTable, r.foreignJoin.OwnerTable())
	}

	local := columnNamed(r.owner, referencedColumnOf(r.localJoin))
	foreign := columnNamed(related, referencedColumnOf(r.foreignJoin))
	if local == nil || foreign == nil {
		return RelationInfo{}, fmt.Errorf("orm: %s.ManyToMany -> %s: the keys on %s must "+
			"reference %s and %s respectively; declare them with References",
			r.owner.name, related.name, joinTable, r.owner.name, related.name)
	}

	return RelationInfo{
		Kind:              KindManyToMany,
		LocalColumn:       local,
		ForeignColumn:     foreign,
		LocalTable:        r.owner.name,
		ForeignTable:      related.name,
		JoinTable:         joinTable,
		LocalJoinColumn:   r.localJoin,
		ForeignJoinColumn: r.foreignJoin,
	}, nil
}

func referencedColumnOf(c ColumnMeta) string {
	if fk, ok := c.(ForeignKeyMeta); ok {
		return fk.ReferencedColumn()
	}
	return ""
}

// soleForeignKeyInto returns the one column of holder referencing table,
// or explains why there is not exactly one.
func soleForeignKeyInto(holder *tableState, table string) (ColumnMeta, error) {
	var found []ColumnMeta
	for _, c := range holder.cols {
		if fk, ok := c.(ForeignKeyMeta); ok && fk.ReferencedTable() == table {
			found = append(found, c)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return nil, fmt.Errorf("no column on %s references %s; declare one with "+
			"References, or name the key with a Relations method", holder.name, table)
	default:
		names := make([]string, len(found))
		for i, c := range found {
			names[i] = `"` + c.Name() + `"`
		}
		return nil, fmt.Errorf("%s has %d columns referencing %s (%s); name the one "+
			"this relationship uses with a Relations method",
			holder.name, len(found), table, strings.Join(names, ", "))
	}
}

func columnNamed(st *tableState, name string) ColumnMeta {
	for _, c := range st.cols {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// relationBinder is how DefineTable attaches a marker to its table without
// knowing which of the four it holds.
type relationBinder interface {
	bindRelation(owner *tableState)
	relationOf() *relation
}

// HasMany is the many side of a one to many. The foreign key lives on the
// related table.
//
// It is legal left uninitialised in a model literal, which is what lets a
// model mention a table declared after it.
type HasMany[E any] struct{ rel *relation }

func (r *HasMany[E]) bindRelation(owner *tableState) {
	r.rel = &relation{kind: KindHasMany, owner: owner, entity: reflect.TypeFor[E]()}
}
func (r *HasMany[E]) relationOf() *relation { return r.rel }

// Relation resolves the relationship and reports the columns a join
// matches on.
func (r *HasMany[E]) Relation() (RelationInfo, error) { return r.rel.info() }

// HasOne is the non-owning side of a one to one. The foreign key lives on
// the related table.
type HasOne[E any] struct{ rel *relation }

func (r *HasOne[E]) bindRelation(owner *tableState) {
	r.rel = &relation{kind: KindHasOne, owner: owner, entity: reflect.TypeFor[E]()}
}
func (r *HasOne[E]) relationOf() *relation { return r.rel }

// Relation resolves the relationship and reports the columns a join
// matches on.
func (r *HasOne[E]) Relation() (RelationInfo, error) { return r.rel.info() }

// BelongsTo is the owning side of a one to many or one to one. The foreign
// key lives on the declaring table.
type BelongsTo[E any] struct{ rel *relation }

func (r *BelongsTo[E]) bindRelation(owner *tableState) {
	r.rel = &relation{kind: KindBelongsTo, owner: owner, entity: reflect.TypeFor[E]()}
}
func (r *BelongsTo[E]) relationOf() *relation { return r.rel }

// Relation resolves the relationship and reports the columns a join
// matches on.
func (r *BelongsTo[E]) Relation() (RelationInfo, error) { return r.rel.info() }

// ManyToMany is either side of a many to many, through a join table.
//
// Unlike the other shapes there is nothing to infer: a ManyToMany[E] names
// the far side but not the table joining the two, and no two tables in a
// schema imply a particular join table. Name the join table's two keys
// with Through in a Relations method, which settles the join table as
// well, since a column knows the table it belongs to.
type ManyToMany[E any] struct{ rel *relation }

func (r *ManyToMany[E]) bindRelation(owner *tableState) {
	r.rel = &relation{kind: KindManyToMany, owner: owner, entity: reflect.TypeFor[E]()}
}
func (r *ManyToMany[E]) relationOf() *relation { return r.rel }

// Relation reports that many to many resolution is not built yet.
func (r *ManyToMany[E]) Relation() (RelationInfo, error) { return r.rel.info() }

// RelationDef names the keys a relationship uses.
type RelationDef struct {
	to  *relation
	key ColumnMeta

	// joinLocal and joinForeign are set by Through, for a many to many.
	joinLocal   ColumnMeta
	joinForeign ColumnMeta
}

// Relater is the optional interface a model implements to name the key
// behind a relationship, for the cases inference cannot settle: two tables
// joined by more than one key, where nothing in the schema says which one
// a given field means.
//
//	func (m *PostModel) Relations() []orm.RelationDef {
//	    return []orm.RelationDef{orm.Via(&m.Author, m.AuthorID)}
//	}
//
// It is a method rather than something declared beside the columns for the
// same reason relationships resolve late: a method body is not part of any
// variable's initialiser, so it can mention another table without closing
// an initialisation cycle.
type Relater interface {
	Relations() []RelationDef
}

// Via names key as the foreign key behind the relationship r.
func Via(r relationBinder, key ColumnMeta) RelationDef {
	return RelationDef{to: r.relationOf(), key: key}
}

// Through names the join table's two keys behind a many to many: local
// references the declaring table, foreign the related one. Both must be
// columns of the same join table.
//
//	func (m *UserModel) Relations() []orm.RelationDef {
//	    return []orm.RelationDef{
//	        orm.Through(&m.Roles, UserRoles.UserID, UserRoles.RoleID),
//	    }
//	}
func Through(r relationBinder, local, foreign ColumnMeta) RelationDef {
	return RelationDef{to: r.relationOf(), joinLocal: local, joinForeign: foreign}
}
