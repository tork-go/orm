package orm

import (
	"context"
	"fmt"
	"reflect"
)

// Loading related rows runs a second query per relationship rather than a
// join: the parent rows come back, their keys are collected, and one
// statement fetches every related row for all of them at once.
//
// That is SQLAlchemy's selectinload rather than its joinedload, and it is the
// better default for the same reasons. A join multiplies the parent's columns
// by the number of related rows, so a user with fifty posts arrives fifty
// times and is assembled back into one; the parent's LIMIT stops meaning what
// it says, since it would cap joined rows rather than parents; and the whole
// thing needs a statement over two tables, which this one does not.
//
// The cost is one statement per relationship, which is the same constant
// whether there are ten parents or ten thousand.

// Loadable is a relationship to load: a marker, or a marker narrowed by
// Where, OrderBy, Limit or a nested Load.
type Loadable interface {
	loadSpec() loadSpec
}

// loadSpec is a relationship and the query that will fetch it.
type loadSpec struct {
	rel    *relation
	preds  []Predicate
	ords   []Ordering
	limit  *int
	nested []loadSpec
}

// Preload is a relationship narrowed by what to fetch for it.
//
//	Users.With(db).Load(
//	    Users.Posts.Where(Posts.Published.Eq(true)).
//	        OrderBy(Posts.CreatedAt.Desc()).
//	        Limit(5),
//	)
//
// It is a value, and every builder returns a new one, so a narrowed
// relationship is as safe to hold and branch from as a query is.
type Preload struct{ spec loadSpec }

func (p Preload) loadSpec() loadSpec { return p.spec }

// Where narrows which related rows are loaded. Conditions accumulate.
func (p Preload) Where(preds ...Predicate) Preload {
	out := p.clone()
	out.spec.preds = append(out.spec.preds, preds...)
	return out
}

// OrderBy sorts the related rows. Terms accumulate.
func (p Preload) OrderBy(ords ...Ordering) Preload {
	out := p.clone()
	out.spec.ords = append(out.spec.ords, ords...)
	return out
}

// Limit caps how many related rows each parent row gets.
//
// Per parent, not in total, which is the only reading that means anything:
// the five most recent posts of every user, rather than five posts spread
// over however many users they happen to belong to.
//
// It costs a statement per parent row. One statement can fetch every
// relationship for every parent at once precisely because it caps nothing,
// and no portable SQL caps rows per group; a database that has window
// functions could, which is worth revisiting when one is asked for.
func (p Preload) Limit(n int) Preload {
	out := p.clone()
	out.spec.limit = &n
	return out
}

// Load loads a relationship of the related rows in turn.
//
//	Users.With(db).Load(Users.Posts.Load(Posts.Comments))
//
// Each level is another statement, however many rows the level above it
// returned.
func (p Preload) Load(rels ...Loadable) Preload {
	out := p.clone()
	for _, r := range rels {
		if r == nil {
			continue
		}
		out.spec.nested = append(out.spec.nested, r.loadSpec())
	}
	return out
}

// clone copies the spec so a builder narrows the copy, slices and all, for
// the reason Filtered.clone gives: two branches appending into one backing
// array overwrite each other whenever the append has spare capacity.
func (p Preload) clone() Preload {
	out := p
	out.spec.preds = append([]Predicate(nil), p.spec.preds...)
	out.spec.ords = append([]Ordering(nil), p.spec.ords...)
	out.spec.nested = append([]loadSpec(nil), p.spec.nested...)
	return out
}

// Load fetches the given relationships alongside the rows.
func (q *Query[E]) Load(rels ...Loadable) *Filtered[E] { return q.filtered().Load(rels...) }

// Load fetches the given relationships alongside the rows, filling the field
// of the row type that each marker is named after.
//
//	users, err := Users.With(db).Load(Users.Posts).All(ctx)
//	// users[0].Posts is filled
//
// The field is found by the marker's own name on the model, the same
// convention that matches a column to a field: UserModel.Posts fills
// User.Posts. A HasMany or ManyToMany wants a slice, a HasOne or BelongsTo a
// single value or a pointer to one.
//
// Loading happens after the rows are read, so it applies to All and First and
// not to Count or Exists, which return no rows to fill.
func (f *Filtered[E]) Load(rels ...Loadable) *Filtered[E] {
	out := f.clone()
	for i, r := range rels {
		if r == nil {
			out.fail(fmt.Errorf("orm: table %q: Load relationship %d is nil",
				f.tableName(), i))
			return out
		}
		spec := r.loadSpec()
		if spec.rel == nil {
			out.fail(fmt.Errorf("orm: table %q: Load was given a relationship that is not "+
				"attached to a table; declare the model with DefineTable rather than NewTable",
				f.tableName()))
			return out
		}
		out.loads = append(out.loads, spec)
	}
	return out
}

// relationTarget is the field of a row type that a relationship's rows go
// into, and what shape it wants them in.
type relationTarget struct {
	index []int

	// many is set when the field is a slice, which a HasMany or ManyToMany
	// needs and the single-valued shapes reject.
	many bool

	// pointer is set when the field, or the slice's element, is a pointer to
	// the related row rather than the row itself.
	pointer bool
}

// targetField resolves where this relationship's rows go on the row type.
//
// Resolved once and remembered, like the relationship itself, and for a
// second reason: a marker is worth declaring for what Relation reports even
// when the row type has no field to load into, so a missing field can only be
// reported to a caller who actually asked for the rows.
func (r *relation) targetField(entity reflect.Type) (relationTarget, error) {
	r.targetOnce.Do(func() { r.target, r.targetErr = r.resolveTarget(entity) })
	return r.target, r.targetErr
}

func (r *relation) resolveTarget(entity reflect.Type) (relationTarget, error) {
	if entity == nil || entity.Kind() != reflect.Struct {
		return relationTarget{}, fmt.Errorf("orm: %s.%s: %s is not a struct, so it has "+
			"nowhere to load into", r.owner.name, r.field, entity)
	}
	sf, ok := entity.FieldByName(r.field)
	if !ok || !sf.IsExported() {
		return relationTarget{}, fmt.Errorf("orm: %s.%s: %s has no exported field named %q "+
			"to load into; a relationship fills the field of the row type it is named after",
			r.owner.name, r.field, entity, r.field)
	}

	related := r.entity
	want := related.String()
	ft := sf.Type

	many := r.kind == KindHasMany || r.kind == KindManyToMany
	if many {
		if ft.Kind() != reflect.Slice {
			return relationTarget{}, fmt.Errorf("orm: %s.%s: %s.%s is %s, but a %s loads "+
				"many rows and needs a []%s or []*%s",
				r.owner.name, r.field, entity, r.field, ft, r.kind, want, want)
		}
		ft = ft.Elem()
	}

	pointer := ft.Kind() == reflect.Pointer
	if pointer {
		ft = ft.Elem()
	}
	if ft != related {
		return relationTarget{}, fmt.Errorf("orm: %s.%s: %s.%s holds %s, but this "+
			"relationship loads %s",
			r.owner.name, r.field, entity, r.field, sf.Type, want)
	}
	return relationTarget{index: sf.Index, many: many, pointer: pointer}, nil
}

// runLoads fetches every relationship for parents, which are addressable
// values of st's row type.
//
// It is not generic because it recurses: the rows a nested load starts from
// are of the related type, which the type parameter of the query that began
// it cannot name.
func runLoads(ctx context.Context, db *DB, st *tableState, parents []reflect.Value, specs []loadSpec) error {
	if len(parents) == 0 {
		return nil
	}
	for _, spec := range specs {
		if err := runLoad(ctx, db, st, parents, spec); err != nil {
			return err
		}
	}
	return nil
}

func runLoad(ctx context.Context, db *DB, st *tableState, parents []reflect.Value, spec loadSpec) error {
	info, err := spec.rel.info()
	if err != nil {
		return err
	}
	target, err := spec.rel.targetField(st.entity)
	if err != nil {
		return err
	}
	related, ok := lookupTable(spec.rel.entity)
	if !ok {
		return fmt.Errorf("orm: %s.%s: no table is declared for %s",
			st.name, spec.rel.field, spec.rel.entity)
	}
	if related.fieldIdx == nil {
		return errNoEntityMapping(related.name)
	}

	// Which of the parent's columns the related rows are matched by, and
	// which of theirs carries the value. A many to many matches through the
	// join table instead, so it collects its pairs first.
	localIdx, ok := st.fieldIdx[info.LocalColumn.Name()]
	if !ok {
		return fmt.Errorf("orm: %s.%s: %q is not a column of %s",
			st.name, spec.rel.field, info.LocalColumn.Name(), st.name)
	}

	keys, byKey := parentKeys(parents, localIdx)
	if len(keys) == 0 {
		return nil
	}

	if info.Kind == KindManyToMany {
		return loadThrough(ctx, db, st, related, info, spec, target, keys, byKey)
	}

	rows, err := fetchRelated(ctx, db, related, info.ForeignColumn, keys, spec)
	if err != nil {
		return err
	}
	// The rows are filled before they are handed out, because assign copies
	// them: a nested load run afterwards would fill the originals and leave
	// every copy already in a parent empty.
	if err := loadNested(ctx, db, related, rows, spec.nested); err != nil {
		return err
	}
	for _, row := range rows {
		key, ok := fieldKey(row, related.fieldIdx[info.ForeignColumn.Name()])
		if !ok {
			continue // a NULL key matches no parent
		}
		assign(byKey[key], target, row)
	}
	return nil
}

// loadThrough fetches a many to many in two statements rather than a join:
// the join table's pairs, then the related rows those pairs name.
//
// Two statements rather than one join, so this needs no statement over more
// than one table, and the related rows come back once each however many
// parents point at them.
func loadThrough(ctx context.Context, db *DB, st, related *tableState, info RelationInfo,
	spec loadSpec, target relationTarget, keys []any, byKey map[any][]reflect.Value) error {

	pairs, err := fetchJoinPairs(ctx, db, info, keys)
	if err != nil {
		return err
	}
	if len(pairs) == 0 {
		return nil
	}

	// The far keys, deduplicated, so a row two parents share is fetched once.
	var farKeys []any
	seen := map[any]bool{}
	for _, p := range pairs {
		if !seen[p.far] {
			seen[p.far] = true
			farKeys = append(farKeys, p.far)
		}
	}

	rows, err := fetchRelated(ctx, db, related, info.ForeignColumn, farKeys, spec)
	if err != nil {
		return err
	}
	// Filled before they are handed out, for the reason runLoad gives.
	if err := loadNested(ctx, db, related, rows, spec.nested); err != nil {
		return err
	}

	byFar := map[any]reflect.Value{}
	for _, row := range rows {
		if key, ok := fieldKey(row, related.fieldIdx[info.ForeignColumn.Name()]); ok {
			byFar[key] = row
		}
	}
	for _, p := range pairs {
		row, ok := byFar[p.far]
		if !ok {
			continue // filtered out by the load's own conditions
		}
		assign(byKey[p.near], target, row)
	}
	return nil
}

// joinPair is one row of a join table: the key into the declaring table and
// the key into the related one.
type joinPair struct{ near, far any }

func fetchJoinPairs(ctx context.Context, db *DB, info RelationInfo, keys []any) ([]joinPair, error) {
	var pairs []joinPair
	err := inChunks(db, keys, 2, func(chunk []any) error {
		c := &compiler{d: db.d, args: &argBuilder{d: db.d}, table: info.JoinTable}
		where, err := c.where([]Predicate{InList{Col: info.LocalJoinColumn, Values: chunk}})
		if err != nil {
			return err
		}
		sql := "SELECT " + c.d.QuoteIdent(info.LocalJoinColumn.Name()) + ", " +
			c.d.QuoteIdent(info.ForeignJoinColumn.Name()) +
			" FROM " + c.d.QuoteIdent(info.JoinTable) + where

		rows, err := db.ex.Query(ctx, sql, c.args.args...)
		if err != nil {
			return fmt.Errorf("orm: table %q: %w", info.JoinTable, err)
		}
		defer rows.Close()

		for rows.Next() {
			near := reflect.New(info.LocalJoinColumn.GoType())
			far := reflect.New(info.ForeignJoinColumn.GoType())
			if err := rows.Scan(near.Interface(), far.Interface()); err != nil {
				return fmt.Errorf("orm: table %q: scanning: %w", info.JoinTable, err)
			}
			n, okNear := derefKey(near.Elem())
			f, okFar := derefKey(far.Elem())
			if okNear && okFar {
				pairs = append(pairs, joinPair{near: n, far: f})
			}
		}
		return rows.Err()
	})
	return pairs, err
}

// fetchRelated reads the related rows whose key column matches keys.
func fetchRelated(ctx context.Context, db *DB, related *tableState, key ColumnMeta,
	keys []any, spec loadSpec) ([]reflect.Value, error) {

	// A limit is per parent, which one statement cannot express, so a limited
	// load runs one statement for each key rather than one for all of them.
	if spec.limit != nil {
		var out []reflect.Value
		for _, k := range keys {
			rows, err := relatedQuery(ctx, db, related, key, []any{k}, spec)
			if err != nil {
				return nil, err
			}
			out = append(out, rows...)
		}
		return out, nil
	}

	var out []reflect.Value
	err := inChunks(db, keys, 1, func(chunk []any) error {
		rows, err := relatedQuery(ctx, db, related, key, chunk, spec)
		if err != nil {
			return err
		}
		out = append(out, rows...)
		return nil
	})
	return out, err
}

func relatedQuery(ctx context.Context, db *DB, related *tableState, key ColumnMeta,
	keys []any, spec loadSpec) ([]reflect.Value, error) {

	c := &compiler{d: db.d, args: &argBuilder{d: db.d}, table: related.name}
	list, err := c.selectList(related.cols)
	if err != nil {
		return nil, err
	}
	preds := append([]Predicate{InList{Col: key, Values: keys}}, spec.preds...)
	where, err := c.where(preds)
	if err != nil {
		return nil, err
	}
	order, err := c.orderBy(spec.ords)
	if err != nil {
		return nil, err
	}
	sql := "SELECT " + list + " FROM " + c.d.QuoteIdent(related.name) + where + order +
		limitOffset(spec.limit, nil)

	rows, err := db.ex.Query(ctx, sql, c.args.args...)
	if err != nil {
		return nil, fmt.Errorf("orm: table %q: %w", related.name, err)
	}
	defer rows.Close()

	var out []reflect.Value
	for rows.Next() {
		row := reflect.New(related.entity).Elem()
		if err := scanRowInto(related, rows, row, related.cols); err != nil {
			return nil, err
		}
		if err := runHook(ctx, related.name, "AfterLoad",
			row.Addr().Interface(), AfterLoader.AfterLoad); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("orm: table %q: reading rows: %w", related.name, err)
	}
	return out, nil
}

// loadNested runs a load's own loads, with the rows it fetched as the parents.
func loadNested(ctx context.Context, db *DB, related *tableState, rows []reflect.Value, nested []loadSpec) error {
	if len(nested) == 0 {
		return nil
	}
	return runLoads(ctx, db, related, rows, nested)
}

// parentKeys collects the distinct key values of the parent rows, and an
// index from each value back to the rows that hold it.
//
// Distinct because two parents sharing a key would otherwise ask the database
// for the same rows twice; the index is what puts one answer into both.
func parentKeys(parents []reflect.Value, index []int) ([]any, map[any][]reflect.Value) {
	keys := make([]any, 0, len(parents))
	byKey := make(map[any][]reflect.Value, len(parents))
	for _, p := range parents {
		key, ok := fieldKey(p, index)
		if !ok {
			continue
		}
		if _, seen := byKey[key]; !seen {
			keys = append(keys, key)
		}
		byKey[key] = append(byKey[key], p)
	}
	return keys, byKey
}

// fieldKey reads a row's key value in a form usable as a map key, reporting
// false for a NULL, which matches nothing on either side.
func fieldKey(row reflect.Value, index []int) (any, bool) {
	if len(index) == 0 {
		return nil, false
	}
	return derefKey(fieldByIndexAlloc(row, index))
}

// derefKey unwraps a nullable key to the value it holds.
//
// Comparing a *int against a *int compares addresses, so a pointer would
// index every row under a key of its own and match nothing.
func derefKey(v reflect.Value) (any, bool) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, false
		}
		v = v.Elem()
	}
	if !v.IsValid() || !v.Type().Comparable() {
		return nil, false
	}
	return v.Interface(), true
}

// assign puts a related row into every parent that asked for it.
func assign(parents []reflect.Value, target relationTarget, row reflect.Value) {
	for _, p := range parents {
		field := fieldByIndexAlloc(p, target.index)
		// The row is copied per parent, so two parents sharing a related row
		// do not share the value and cannot see each other's changes to it.
		v := row
		if target.pointer {
			held := reflect.New(row.Type())
			held.Elem().Set(row)
			v = held
		}
		if target.many {
			field.Set(reflect.Append(field, v))
			continue
		}
		// A single valued relationship keeps the first row it is given. A
		// second means the data does not match the shape the model declared,
		// which is the database's to answer for rather than something to
		// silently overwrite.
		if field.IsZero() {
			field.Set(v)
		}
	}
}

// inChunks calls fn with slices of keys small enough to bind, given how many
// parameters each key costs.
func inChunks(db *DB, keys []any, perKey int, fn func([]any) error) error {
	size := rowsPerStatement(db.d.MaxBindParams(), perKey, len(keys))
	for start := 0; start < len(keys); start += size {
		if err := fn(keys[start:min(start+size, len(keys))]); err != nil {
			return err
		}
	}
	return nil
}
