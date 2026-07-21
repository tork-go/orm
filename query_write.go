package orm

import (
	"context"
	"fmt"
	"reflect"
	"strings"
)

// Insert writes e as a new row.
//
// Values the database produces are read back into e, so the row in memory
// matches the row that was stored. That covers a generated key and any
// column left to a server default.
//
// A column is left out of the statement when the database is to supply its
// value: the identity column always, since a generated key cannot be
// written to at all, and a column with a server default whose field is
// still zero. The second of those has a consequence worth knowing: a zero
// cannot be inserted explicitly into a defaulted column, because nothing
// distinguishes "left alone" from "set to zero" in a Go struct. Declare
// the field as a pointer if the difference matters.
func (q *Query[E]) Insert(ctx context.Context, e *E) error {
	w, err := q.writer(e)
	if err != nil {
		return err
	}
	if err := runHook(ctx, w.st.name, "BeforeCreate", any(e), BeforeCreater.BeforeCreate); err != nil {
		return err
	}
	// The count is not returned: a plain insert writes the row or fails, so
	// there is nothing for it to say. An upsert is where it means something,
	// and Upsert.Insert is where it is reported.
	if _, err := w.insert(ctx); err != nil {
		return err
	}
	return runHook(ctx, w.st.name, "AfterCreate", any(e), AfterCreater.AfterCreate)
}

// Update writes every writable column of e to the row its primary key
// identifies.
//
// All of them, not only the ones that changed: Go cannot intercept a field
// assignment, so nothing here can know which those were. The primary key
// and the identity column are excluded, since they say which row is being
// written rather than what to write.
func (q *Query[E]) Update(ctx context.Context, e *E) error {
	w, err := q.writer(e)
	if err != nil {
		return err
	}
	if err := runHook(ctx, w.st.name, "BeforeUpdate", any(e), BeforeUpdater.BeforeUpdate); err != nil {
		return err
	}
	n, err := w.update(ctx, nil)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNoRows
	}
	return runHook(ctx, w.st.name, "AfterUpdate", any(e), AfterUpdater.AfterUpdate)
}

// UpdateIf is Update, but only if the row still matches the conditions.
//
//	err := Users.With(db).UpdateIf(ctx, user, Users.Version.Eq(seen))
//
//	UPDATE "users" SET ... WHERE "id" = $9 AND "version" = $10
//
// This is optimistic locking: rather than holding a lock between reading a
// row and writing it, the write says what the row looked like when it was
// read and fails if that is no longer true. It costs nothing while nothing
// contends, which is the case it is for; ForUpdate is the pessimistic answer,
// and the better one when contention is the norm rather than the exception.
//
// The usual shape keeps the value that was read, since the row in hand has
// already been changed by the time it is written:
//
//	seen := user.Version
//	user.Version++
//	user.Name = newName
//	err := Users.With(db).UpdateIf(ctx, user, Users.Version.Eq(seen))
//
// It returns ErrNoRows when nothing was written, which means either that the
// row is gone or that the conditions no longer hold. Those are not
// distinguished: telling them apart would take a second statement, whose
// answer would be about a moment later than the one being asked about.
func (q *Query[E]) UpdateIf(ctx context.Context, e *E, conds ...Predicate) error {
	w, err := q.writer(e)
	if err != nil {
		return err
	}
	if len(conds) == 0 {
		return fmt.Errorf("orm: table %q: UpdateIf has no condition to check; "+
			"call Update, which writes the row its key identifies", w.st.name)
	}
	if err := runHook(ctx, w.st.name, "BeforeUpdate", any(e), BeforeUpdater.BeforeUpdate); err != nil {
		return err
	}
	n, err := w.update(ctx, conds)
	if err != nil {
		return err
	}
	if n == 0 {
		return conditionFailed(w.st.name, "UpdateIf", "written")
	}
	return runHook(ctx, w.st.name, "AfterUpdate", any(e), AfterUpdater.AfterUpdate)
}

// Delete removes the row e's primary key identifies.
func (q *Query[E]) Delete(ctx context.Context, e *E) error {
	w, err := q.writer(e)
	if err != nil {
		return err
	}
	if err := runHook(ctx, w.st.name, "BeforeDelete", any(e), BeforeDeleter.BeforeDelete); err != nil {
		return err
	}
	n, err := w.delete(ctx, nil)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNoRows
	}
	return runHook(ctx, w.st.name, "AfterDelete", any(e), AfterDeleter.AfterDelete)
}

// DeleteIf is Delete, but only if the row still matches the conditions.
//
//	err := Posts.With(db).DeleteIf(ctx, post, Posts.Draft.Eq(true))
//
// It is UpdateIf's other half, and for the same situation: removing a row on
// the strength of something read a moment ago, without holding a lock in
// between. It returns ErrNoRows for the same two reasons.
func (q *Query[E]) DeleteIf(ctx context.Context, e *E, conds ...Predicate) error {
	w, err := q.writer(e)
	if err != nil {
		return err
	}
	if len(conds) == 0 {
		return fmt.Errorf("orm: table %q: DeleteIf has no condition to check; "+
			"call Delete, which removes the row its key identifies", w.st.name)
	}
	if err := runHook(ctx, w.st.name, "BeforeDelete", any(e), BeforeDeleter.BeforeDelete); err != nil {
		return err
	}
	n, err := w.delete(ctx, conds)
	if err != nil {
		return err
	}
	if n == 0 {
		return conditionFailed(w.st.name, "DeleteIf", "removed")
	}
	return runHook(ctx, w.st.name, "AfterDelete", any(e), AfterDeleter.AfterDelete)
}

// conditionFailed reports a conditional write that touched no row.
//
// It says both things it could mean rather than picking one, and wraps
// ErrNoRows so errors.Is answers the same for it as for the unconditional
// Update and Delete.
func conditionFailed(table, op, verb string) error {
	return fmt.Errorf("orm: table %q: %s %s no row: either it is gone, or the "+
		"conditions no longer hold: %w", table, op, verb, ErrNoRows)
}

// Save inserts e when it has no key yet, and updates it otherwise.
//
// The test is whether the identity column is still zero, which is what a
// row that has never been stored looks like. A table whose key is not
// generated has no such signal, since any value there was supplied by the
// caller, so Save reports that rather than guessing which it meant.
func (q *Query[E]) Save(ctx context.Context, e *E) error {
	w, err := q.writer(e)
	if err != nil {
		return err
	}
	if w.st.identity == nil {
		return fmt.Errorf("orm: table %q: Save needs a generated key to tell a new row "+
			"from a stored one, and this table's key is supplied by the caller; "+
			"call Insert or Update", w.st.name)
	}
	if w.field(w.st.identity).IsZero() {
		return q.Insert(ctx, e)
	}
	return q.Update(ctx, e)
}

// writer holds what every write shares: the table, the handle, and the
// entity being written.
type writer[E any] struct {
	st  *tableState
	db  *DB
	e   *E
	val reflect.Value

	// conflict is the upsert clause the insert carries, nil for a plain one.
	conflict *conflictClause
}

// readyToWrite reports whether the query can write at all: the checks that
// are about the query rather than about any one row.
//
// It is separate from writer because a batch makes them once and then builds
// a writer per row, where repeating them would report the same problem as
// many times as the caller passed rows.
func (q *Query[E]) readyToWrite() error {
	if q.st == nil {
		return errNoEntityMapping("")
	}
	if q.st.fieldIdx == nil {
		return errNoEntityMapping(q.st.name)
	}
	if q.db == nil {
		return fmt.Errorf("orm: table %q: no database handle; pass one to With", q.st.name)
	}
	return nil
}

func (q *Query[E]) writer(e *E) (*writer[E], error) {
	if err := q.readyToWrite(); err != nil {
		return nil, err
	}
	if e == nil {
		return nil, fmt.Errorf("orm: table %q: nil row", q.st.name)
	}
	return q.newWriter(e), nil
}

// newWriter builds a writer for a row already established to be non-nil on
// a query already established to be able to write. It exists so a batch can
// report a bad row by its position without rebuilding the writer literal.
func (q *Query[E]) newWriter(e *E) *writer[E] {
	return &writer[E]{
		st:       q.st,
		db:       q.db,
		e:        e,
		val:      reflect.ValueOf(e).Elem(),
		conflict: q.conflict,
	}
}

// withDB returns a copy of the writer bound to db.
//
// A batch resolves its rows before it opens a transaction, since working out
// what to write is what decides how many statements it takes and therefore
// whether a transaction is needed at all. The writers it built are bound to
// the outer handle, so each is rebound to the transaction before it runs.
func (w *writer[E]) withDB(db *DB) *writer[E] {
	out := *w
	out.db = db
	return &out
}

// field returns the entity field a column maps to.
func (w *writer[E]) field(c ColumnMeta) reflect.Value {
	return fieldByIndexAlloc(w.val, w.st.fieldIdx[c.Name()])
}

// bindable prepares a column's current field value for binding, encoding
// it first when the column stores a document.
func (w *writer[E]) bindable(c *compiler, col ColumnMeta) (any, error) {
	return c.value(col, w.field(col).Interface())
}

// insertColumns decides which columns the statement writes, and fills in
// any client side generated value on the way.
//
// A generated value is written back into the entity as well as bound, so
// the caller's row carries the key it was stored under rather than the
// zero it arrived with.
func (w *writer[E]) insertColumns() []ColumnMeta {
	var cols []ColumnMeta
	for _, c := range w.st.cols {
		if c == w.st.identity {
			continue
		}
		f := w.field(c)
		if c.IsClientGenerated() && f.IsZero() {
			if codec, ok := c.(ValueCodec); ok {
				if v, generated := codec.GenerateAny(); generated {
					gv := reflect.ValueOf(v)
					if gv.Type().AssignableTo(f.Type()) {
						f.Set(gv)
					}
				}
			}
		}
		if _, hasDefault := c.ServerDefaultExpr(); hasDefault && f.IsZero() {
			continue
		}
		cols = append(cols, c)
	}
	return cols
}

// returningColumns are the ones the statement left to the database, and so
// the ones worth reading back.
func (w *writer[E]) returningColumns(written []ColumnMeta) []ColumnMeta {
	inserted := make(map[ColumnMeta]bool, len(written))
	for _, c := range written {
		inserted[c] = true
	}
	var out []ColumnMeta
	for _, c := range w.st.cols {
		if !inserted[c] {
			out = append(out, c)
		}
	}
	return out
}

// insert writes the row and reports how many rows it wrote, which is one
// unless an upsert declined to write it.
func (w *writer[E]) insert(ctx context.Context) (int64, error) {
	c := &compiler{d: w.db.d, args: &argBuilder{d: w.db.d}, table: w.st.name}

	cols := w.insertColumns()
	names := make([]string, len(cols))
	marks := make([]string, len(cols))
	for i, col := range cols {
		v, err := w.bindable(c, col)
		if err != nil {
			return 0, err
		}
		names[i] = c.d.QuoteIdent(col.Name())
		marks[i] = c.args.bind(v)
	}

	sql := "INSERT INTO " + c.d.QuoteIdent(w.st.name)
	if len(cols) == 0 {
		// Every column is the database's to supply. Postgres spells an
		// all-defaults insert this way; a dialect that cannot would have
		// to say so, which none Tork targets does.
		sql += " DEFAULT VALUES"
	} else {
		sql += " (" + strings.Join(names, ", ") + ") VALUES (" + strings.Join(marks, ", ") + ")"
	}
	clause, err := c.conflict(w.conflict, cols)
	if err != nil {
		return 0, err
	}
	if clause != "" {
		sql += " " + clause
	}

	back := w.returningColumns(cols)
	if w.db.d.SupportsReturning() && len(back) > 0 {
		return w.insertReturning(ctx, sql, c.args.args, back)
	}
	res, err := w.db.ex.Exec(ctx, sql, c.args.args...)
	if err != nil {
		return 0, fmt.Errorf("orm: table %q: inserting: %w", w.st.name, err)
	}
	// Only a skipping upsert can write fewer rows than it was given without
	// failing, so only there is the driver's count what decides. Everywhere
	// else the statement wrote its row or returned an error, whatever it
	// reports as a count, and not every driver reports a meaningful one.
	wrote := int64(1)
	if w.skips() {
		wrote = res.RowsAffected
	}

	// Without RETURNING only the key comes back, and only as an integer.
	// A server default on any other column stays whatever the database
	// chose, unread; there is no second statement that could recover it
	// without guessing which row was just written.
	//
	// A row that was skipped left no key behind, and whatever the driver
	// still reports as the last one belongs to an earlier statement, so
	// nothing is read back into a row that was not written.
	if w.st.identity != nil && wrote != 0 && res.LastInsertID != 0 {
		// An identity column is an integer by the rule that chose it, so
		// there is nothing to check before setting it.
		w.field(w.st.identity).SetInt(res.LastInsertID)
	}
	return wrote, nil
}

// insertReturning runs an insert that reads its generated values back in
// the same statement, and reports whether it wrote a row at all.
func (w *writer[E]) insertReturning(ctx context.Context, sql string, args []any, back []ColumnMeta) (int64, error) {
	names := make([]string, len(back))
	for i, c := range back {
		names[i] = w.db.d.QuoteIdent(c.Name())
	}
	sql += " RETURNING " + strings.Join(names, ", ")

	rows, err := w.db.ex.Query(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("orm: table %q: inserting: %w", w.st.name, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("orm: table %q: inserting: %w", w.st.name, err)
		}
		// An upsert told to skip a row already there returns nothing for it,
		// which is the statement working rather than failing. Anywhere else
		// an insert that returned no row is one that did not happen.
		if w.skips() {
			return 0, nil
		}
		return 0, fmt.Errorf("orm: table %q: insert returned no row", w.st.name)
	}
	if err := w.scanInto(rows, back); err != nil {
		return 0, err
	}
	return 1, rows.Err()
}

// skips reports whether this insert may write no row without that being a
// failure, which is exactly the upsert that was told to do nothing.
func (w *writer[E]) skips() bool {
	return w.conflict != nil && w.conflict.action == conflictNothing
}

// scanInto reads a subset of columns back into the entity, sharing the
// destination building with an ordinary row scan so a document column is
// decoded the same way whichever direction it travelled.
func (w *writer[E]) scanInto(rows Rows, cols []ColumnMeta) error {
	dests, finish := rowDests(w.st, w.val, cols)
	if err := rows.Scan(dests...); err != nil {
		return fmt.Errorf("orm: table %q: reading back the written row: %w", w.st.name, err)
	}
	return finish()
}

// keyFilter builds the WHERE that identifies this entity's row, plus any
// conditions a conditional write asked for.
//
// The two are joined with AND rather than kept apart, because they say one
// thing together: this row, and only while it still looks like this.
func (w *writer[E]) keyFilter(c *compiler, conds []Predicate) (string, error) {
	if len(w.st.pk) == 0 {
		return "", fmt.Errorf("orm: table %q: this operation needs a primary key to say "+
			"which row it means, and the table declares none", w.st.name)
	}
	preds := make([]Predicate, 0, len(w.st.pk)+len(conds))
	for _, col := range w.st.pk {
		preds = append(preds, Comparison{Col: col, Op: OpEq, Value: w.field(col).Interface()})
	}
	return c.where(append(preds, conds...))
}

// update writes the row and reports how many rows it changed.
//
// It returns the count rather than ErrNoRows for a row that was not there,
// because a batch needs to add the counts up and report once. Update itself
// maps a zero back to ErrNoRows, which is what a caller writing one row
// wants.
func (w *writer[E]) update(ctx context.Context, conds []Predicate) (int64, error) {
	c := &compiler{d: w.db.d, args: &argBuilder{d: w.db.d}, table: w.st.name}

	sets := make([]Assignment, 0, len(w.st.cols))
	for _, col := range w.st.cols {
		if col.IsPrimaryKey() || col == w.st.identity {
			continue
		}
		sets = append(sets, Assignment{Col: col, Value: w.field(col).Interface()})
	}
	if len(sets) == 0 {
		return 0, fmt.Errorf("orm: table %q: Update has nothing to write; every column is "+
			"part of the primary key", w.st.name)
	}

	// The same renderer UpdateAll uses, so a row written by key and a set of
	// rows written by filter cannot drift apart in how they spell a SET or
	// encode a value.
	assignments, err := c.set(sets)
	if err != nil {
		return 0, err
	}
	where, err := w.keyFilter(c, conds)
	if err != nil {
		return 0, err
	}
	sql := "UPDATE " + c.d.QuoteIdent(w.st.name) + " SET " + assignments + where

	res, err := w.db.ex.Exec(ctx, sql, c.args.args...)
	if err != nil {
		return 0, fmt.Errorf("orm: table %q: updating: %w", w.st.name, err)
	}
	return res.RowsAffected, nil
}

// delete removes the row and reports how many rows it removed, for the same
// reason update does.
func (w *writer[E]) delete(ctx context.Context, conds []Predicate) (int64, error) {
	c := &compiler{d: w.db.d, args: &argBuilder{d: w.db.d}, table: w.st.name}
	where, err := w.keyFilter(c, conds)
	if err != nil {
		return 0, err
	}
	sql := "DELETE FROM " + c.d.QuoteIdent(w.st.name) + where

	res, err := w.db.ex.Exec(ctx, sql, c.args.args...)
	if err != nil {
		return 0, fmt.Errorf("orm: table %q: deleting: %w", w.st.name, err)
	}
	return res.RowsAffected, nil
}
