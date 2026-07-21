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
	return w.insert(ctx)
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
	return w.update(ctx)
}

// Delete removes the row e's primary key identifies.
func (q *Query[E]) Delete(ctx context.Context, e *E) error {
	w, err := q.writer(e)
	if err != nil {
		return err
	}
	return w.delete(ctx)
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
}

func (q *Query[E]) writer(e *E) (*writer[E], error) {
	if q.st == nil {
		return nil, errNoEntityMapping("")
	}
	if q.st.fieldIdx == nil {
		return nil, errNoEntityMapping(q.st.name)
	}
	if q.db == nil {
		return nil, fmt.Errorf("orm: table %q: no database handle; pass one to With", q.st.name)
	}
	if e == nil {
		return nil, fmt.Errorf("orm: table %q: nil row", q.st.name)
	}
	return &writer[E]{st: q.st, db: q.db, e: e, val: reflect.ValueOf(e).Elem()}, nil
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

func (w *writer[E]) insert(ctx context.Context) error {
	c := &compiler{d: w.db.d, args: &argBuilder{d: w.db.d}, table: w.st.name}

	cols := w.insertColumns()
	names := make([]string, len(cols))
	marks := make([]string, len(cols))
	for i, col := range cols {
		v, err := w.bindable(c, col)
		if err != nil {
			return err
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

	back := w.returningColumns(cols)
	if w.db.d.SupportsReturning() && len(back) > 0 {
		return w.insertReturning(ctx, sql, c.args.args, back)
	}
	res, err := w.db.ex.Exec(ctx, sql, c.args.args...)
	if err != nil {
		return fmt.Errorf("orm: table %q: inserting: %w", w.st.name, err)
	}
	// Without RETURNING only the key comes back, and only as an integer.
	// A server default on any other column stays whatever the database
	// chose, unread; there is no second statement that could recover it
	// without guessing which row was just written.
	if w.st.identity != nil && res.LastInsertID != 0 {
		// An identity column is an integer by the rule that chose it, so
		// there is nothing to check before setting it.
		w.field(w.st.identity).SetInt(res.LastInsertID)
	}
	return nil
}

// insertReturning runs an insert that reads its generated values back in
// the same statement.
func (w *writer[E]) insertReturning(ctx context.Context, sql string, args []any, back []ColumnMeta) error {
	names := make([]string, len(back))
	for i, c := range back {
		names[i] = w.db.d.QuoteIdent(c.Name())
	}
	sql += " RETURNING " + strings.Join(names, ", ")

	rows, err := w.db.ex.Query(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("orm: table %q: inserting: %w", w.st.name, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return fmt.Errorf("orm: table %q: inserting: %w", w.st.name, err)
		}
		return fmt.Errorf("orm: table %q: insert returned no row", w.st.name)
	}
	if err := w.scanInto(rows, back); err != nil {
		return err
	}
	return rows.Err()
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

// keyFilter builds the WHERE that identifies this entity's row.
func (w *writer[E]) keyFilter(c *compiler) (string, error) {
	if len(w.st.pk) == 0 {
		return "", fmt.Errorf("orm: table %q: this operation needs a primary key to say "+
			"which row it means, and the table declares none", w.st.name)
	}
	preds := make([]Predicate, len(w.st.pk))
	for i, col := range w.st.pk {
		preds[i] = Comparison{Col: col, Op: OpEq, Value: w.field(col).Interface()}
	}
	return c.where(preds)
}

func (w *writer[E]) update(ctx context.Context) error {
	c := &compiler{d: w.db.d, args: &argBuilder{d: w.db.d}, table: w.st.name}

	var sets []string
	for _, col := range w.st.cols {
		if col.IsPrimaryKey() || col == w.st.identity {
			continue
		}
		v, err := w.bindable(c, col)
		if err != nil {
			return err
		}
		// Unqualified on purpose: Postgres rejects a table-qualified
		// column on the left of a SET.
		sets = append(sets, c.d.QuoteIdent(col.Name())+" = "+c.args.bind(v))
	}
	if len(sets) == 0 {
		return fmt.Errorf("orm: table %q: Update has nothing to write; every column is "+
			"part of the primary key", w.st.name)
	}

	where, err := w.keyFilter(c)
	if err != nil {
		return err
	}
	sql := "UPDATE " + c.d.QuoteIdent(w.st.name) + " SET " + strings.Join(sets, ", ") + where

	res, err := w.db.ex.Exec(ctx, sql, c.args.args...)
	if err != nil {
		return fmt.Errorf("orm: table %q: updating: %w", w.st.name, err)
	}
	if res.RowsAffected == 0 {
		return ErrNoRows
	}
	return nil
}

func (w *writer[E]) delete(ctx context.Context) error {
	c := &compiler{d: w.db.d, args: &argBuilder{d: w.db.d}, table: w.st.name}
	where, err := w.keyFilter(c)
	if err != nil {
		return err
	}
	sql := "DELETE FROM " + c.d.QuoteIdent(w.st.name) + where

	res, err := w.db.ex.Exec(ctx, sql, c.args.args...)
	if err != nil {
		return fmt.Errorf("orm: table %q: deleting: %w", w.st.name, err)
	}
	if res.RowsAffected == 0 {
		return ErrNoRows
	}
	return nil
}
