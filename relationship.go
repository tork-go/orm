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

// RelationInfo is a resolved relationship: the two columns a join matches
// on, and the tables on either side.
type RelationInfo struct {
	Kind RelationKind

	// LocalColumn is on the declaring table and ForeignColumn on the
	// related one. A BelongsTo holds the key locally; the other shapes
	// have it on the far side.
	LocalColumn   ColumnMeta
	ForeignColumn ColumnMeta

	LocalTable   string
	ForeignTable string
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

// info resolves the relationship once and gives every later caller the
// same answer, including the same failure.
func (r *relation) info() (RelationInfo, error) {
	if r == nil || r.owner == nil {
		return RelationInfo{}, fmt.Errorf("orm: relationship is not attached to a table; " +
			"declare the model with DefineTable rather than NewTable")
	}
	r.once.Do(func() { r.resolved, r.err = r.resolve() })
	return r.resolved, r.err
}

func (r *relation) resolve() (RelationInfo, error) {
	if r.kind == KindManyToMany {
		return RelationInfo{}, fmt.Errorf("orm: %s.ManyToMany -> %s: not supported yet; "+
			"a join table and both of its keys have to be named, and nothing in the "+
			"declaration says which table that is", r.owner.name, r.entity)
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
// It binds like the others but does not resolve. A many to many needs the
// join table and both of its keys, and nothing in the declaration says
// which table that is. Reporting that plainly beats inferring the wrong
// join, and the marker still documents the relationship's existence.
type ManyToMany[E any] struct{ rel *relation }

func (r *ManyToMany[E]) bindRelation(owner *tableState) {
	r.rel = &relation{kind: KindManyToMany, owner: owner, entity: reflect.TypeFor[E]()}
}
func (r *ManyToMany[E]) relationOf() *relation { return r.rel }

// Relation reports that many to many resolution is not built yet.
func (r *ManyToMany[E]) Relation() (RelationInfo, error) { return r.rel.info() }

// RelationDef names the foreign key a relationship uses.
type RelationDef struct {
	to  *relation
	key ColumnMeta
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
