package orm

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// The Many operations write a batch of rows the caller is already holding,
// as against the All operations, which write every row a filter matches
// without loading any of them. See query_setops.go.
//
// The distinction is not stylistic. These have rows, so hooks fire on each
// one and values the database generates are read back into them; the set
// operations have only a condition, so neither is possible there. Coming
// from Prisma, updateMany and deleteMany are the set operations, spelled
// UpdateAll and DeleteAll here.
//
// A batch of no rows does nothing and reports no error. A slice filtered
// down to nothing is ordinary caller code rather than a mistake, and making
// it one would put an `if len(xs) > 0` in front of every call.

// InsertMany writes es as new rows.
//
//	err := Users.With(db).InsertMany(ctx, users...)
//
//	INSERT INTO "users" ("username", "age") VALUES ($1, $2), ($3, $4)
//	    RETURNING "id"
//
// Values the database produces are read back into each row, exactly as
// Insert does for one: a generated key, and any column left to a server
// default.
//
// Rows are not required to write the same columns. A column with a server
// default is left out while its field is still zero, so two rows of the same
// type can want different column lists, and the batch is split into one
// statement per list rather than forcing a value on a row that did not have
// one.
func (q *Query[E]) InsertMany(ctx context.Context, es ...*E) error {
	if len(es) == 0 {
		return nil
	}
	// The count is not returned, for the reason Insert gives: only an upsert
	// can write fewer rows than it was given without failing.
	_, err := q.insertMany(ctx, es)
	return err
}

// insertMany is the body both spellings share, reporting how many rows the
// statements wrote.
func (q *Query[E]) insertMany(ctx context.Context, es []*E) (int64, error) {
	ws, err := q.bulkWriters(es)
	if err != nil {
		return 0, err
	}

	// Every BeforeCreate runs before any column list is worked out. A hook
	// that fills in a field changes whether that field is still zero, and so
	// whether its column is written at all or left to the database, which
	// would make a list computed earlier wrong.
	for _, w := range ws {
		if err := runHook(ctx, w.st.name, "BeforeCreate", any(w.e), BeforeCreater.BeforeCreate); err != nil {
			return 0, err
		}
	}

	var written int64
	chunks := insertChunks(ws, q.db.d)
	if err := q.db.atomically(ctx, len(chunks) > 1, func(db *DB) error {
		for _, ch := range chunks {
			n, err := ch.run(ctx, db)
			if err != nil {
				return err
			}
			written += n
		}
		return nil
	}); err != nil {
		return 0, err
	}

	// AfterCreate runs once the rows are committed, matching Insert, where
	// the row is written whether or not the hook then fails. Running these
	// inside the transaction would let a hook roll the batch back, which is
	// a different contract from the one row case and not one to introduce
	// only for batches.
	for _, w := range ws {
		if err := runHook(ctx, w.st.name, "AfterCreate", any(w.e), AfterCreater.AfterCreate); err != nil {
			return written, err
		}
	}
	return written, nil
}

// UpdateMany writes every writable column of each row to the row its primary
// key identifies, and returns how many rows it changed.
//
//	n, err := Users.With(db).UpdateMany(ctx, users...)
//
// It runs one statement per row, inside one transaction. Rows carry
// different values, and no statement portable across the databases Tork
// targets writes different values to different rows: Postgres could with an
// UPDATE ... FROM (VALUES ...), which every other driver would then have to
// find its own answer to. Doing it as many statements and saying so is the
// honest version.
//
// To write one set of values to many rows, that is UpdateAll, which is one
// statement however many rows it matches.
func (q *Query[E]) UpdateMany(ctx context.Context, es ...*E) (int64, error) {
	if len(es) == 0 {
		return 0, nil
	}
	ws, err := q.bulkWriters(es)
	if err != nil {
		return 0, err
	}

	for _, w := range ws {
		if err := runHook(ctx, w.st.name, "BeforeUpdate", any(w.e), BeforeUpdater.BeforeUpdate); err != nil {
			return 0, err
		}
	}

	var affected int64
	if err := q.db.atomically(ctx, len(ws) > 1, func(db *DB) error {
		for i, w := range ws {
			n, err := w.withDB(db).update(ctx, nil)
			if err != nil {
				// The error already names the table, so only the row's
				// position is added, and as a suffix rather than a prefix
				// that would say orm twice.
				return fmt.Errorf("%w (row %d)", err, i)
			}
			affected += n
		}
		return nil
	}); err != nil {
		return 0, err
	}

	for _, w := range ws {
		if err := runHook(ctx, w.st.name, "AfterUpdate", any(w.e), AfterUpdater.AfterUpdate); err != nil {
			return affected, err
		}
	}
	return affected, partialWrite(q.st.name, "UpdateMany", "updated", affected, int64(len(ws)))
}

// DeleteMany removes the rows the given rows' primary keys identify, and
// returns how many rows it removed.
//
//	n, err := Users.With(db).DeleteMany(ctx, users...)
//
//	DELETE FROM "users" WHERE "id" IN ($1, $2, $3)
//
// A composite key cannot be listed that way, so it becomes a comparison per
// row instead:
//
//	DELETE FROM "memberships"
//	    WHERE (("org_id" = $1 AND "user_id" = $2) OR ("org_id" = $3 AND "user_id" = $4))
//
// A table with a soft-delete column is updated rather than deleted; see
// SoftDelete. ForceDeleteMany always removes the rows.
//
// To remove every row matching a condition, that is DeleteAll.
func (q *Query[E]) DeleteMany(ctx context.Context, es ...*E) (int64, error) {
	return q.deleteMany(ctx, es, false)
}

// ForceDeleteMany is DeleteMany, but always issues a physical DELETE even
// when the table has a soft-delete column.
func (q *Query[E]) ForceDeleteMany(ctx context.Context, es ...*E) (int64, error) {
	return q.deleteMany(ctx, es, true)
}

func (q *Query[E]) deleteMany(ctx context.Context, es []*E, force bool) (int64, error) {
	op := "DeleteMany"
	if force {
		op = "ForceDeleteMany"
	}
	if len(es) == 0 {
		return 0, nil
	}
	ws, err := q.bulkWriters(es)
	if err != nil {
		return 0, err
	}
	pk := q.st.pk
	if len(pk) == 0 {
		return 0, fmt.Errorf("orm: table %q: %s needs a primary key to say which "+
			"rows it means, and the table declares none; use Where(...).DeleteAll instead",
			q.st.name, op)
	}

	for _, w := range ws {
		if err := runHook(ctx, w.st.name, "BeforeDelete", any(w.e), BeforeDeleter.BeforeDelete); err != nil {
			return 0, err
		}
	}

	// One value per key column per row, so a composite key fits fewer rows
	// into a statement than a single column one does.
	per := rowsPerStatement(q.db.d.MaxBindParams(), len(pk), len(ws))

	var affected int64
	if err := q.db.atomically(ctx, per < len(ws), func(db *DB) error {
		for start := 0; start < len(ws); start += per {
			n, err := deleteBatch(ctx, db, q.st, ws[start:min(start+per, len(ws))], force)
			if err != nil {
				return err
			}
			affected += n
		}
		return nil
	}); err != nil {
		return 0, err
	}

	for _, w := range ws {
		if err := runHook(ctx, w.st.name, "AfterDelete", any(w.e), AfterDeleter.AfterDelete); err != nil {
			return affected, err
		}
	}
	return affected, partialWrite(q.st.name, op, "deleted", affected, int64(len(ws)))
}

// bulkWriters resolves every row, reporting the query's own problems once
// and any row's by its position in the batch.
//
// An index is what makes a bad row findable: "nil row" says nothing about
// which of five hundred it was.
func (q *Query[E]) bulkWriters(es []*E) ([]*writer[E], error) {
	if err := q.readyToWrite(); err != nil {
		return nil, err
	}
	ws := make([]*writer[E], len(es))
	for i, e := range es {
		if e == nil {
			return nil, fmt.Errorf("orm: table %q: row %d is nil", q.st.name, i)
		}
		ws[i] = q.newWriter(e)
	}
	return ws, nil
}

// insertChunk is one statement's worth of an insert: the columns being
// written and the rows writing them.
type insertChunk[E any] struct {
	cols []ColumnMeta
	ws   []*writer[E]
}

// insertChunks splits rows into the statements that will write them: first
// by the columns each row writes, then by how many rows a statement may
// carry.
func insertChunks[E any](ws []*writer[E], d QueryDialect) []insertChunk[E] {
	var (
		chunks []insertChunk[E]
		groups []insertChunk[E]
		index  = map[string]int{}
	)
	for _, w := range ws {
		// insertColumns also fills in any client side generated value, so
		// this is where a generated key lands in the caller's row.
		cols := w.insertColumns()
		key := columnsKey(cols)
		i, ok := index[key]
		if !ok {
			groups = append(groups, insertChunk[E]{cols: cols})
			i = len(groups) - 1
			index[key] = i
		}
		groups[i].ws = append(groups[i].ws, w)
	}

	for _, g := range groups {
		per := rowsPerStatement(d.MaxBindParams(), len(g.cols), len(g.ws))
		switch {
		case len(g.cols) == 0:
			// Every column is the database's to supply, which Postgres
			// spells DEFAULT VALUES. That takes no value list and so cannot
			// carry a second row.
			per = 1
		case len(g.ws[0].returningColumns(g.cols)) > 0 && !d.SupportsReturning():
			// There is something to read back and no way to read it back
			// from a statement covering several rows, since the only signal
			// such a dialect gives is one last insert id. One row at a time
			// is what makes that id belong to a known row.
			per = 1
		case len(g.ws[0].returningColumns(g.cols)) > 0 && g.ws[0].conflict != nil:
			// An upsert breaks the correlation run relies on: a row the
			// database skipped returns nothing, so the n-th row back is no
			// longer the n-th row written and there is nothing in it to match
			// them up by. One row per statement is what restores the answer,
			// and is the price of reading values back from an upsert.
			per = 1
		}
		for start := 0; start < len(g.ws); start += per {
			chunks = append(chunks, insertChunk[E]{
				cols: g.cols,
				ws:   g.ws[start:min(start+per, len(g.ws))],
			})
		}
	}
	return chunks
}

// columnsKey identifies a column list, so rows writing the same columns
// group together. The separator is a byte no column name can contain.
func columnsKey(cols []ColumnMeta) string {
	var b strings.Builder
	for _, c := range cols {
		b.WriteString(c.Name())
		b.WriteByte(0)
	}
	return b.String()
}

// run writes this chunk's rows, reads back whatever the database supplied for
// each, and reports how many rows it wrote.
func (ch insertChunk[E]) run(ctx context.Context, db *DB) (int64, error) {
	// A chunk of one is the statement Insert already writes, and it covers
	// the three cases a multi row statement cannot: an insert naming no
	// columns, a dialect that reports generated values through the result
	// rather than through RETURNING, and an upsert with something to read
	// back.
	if len(ch.ws) == 1 {
		return ch.ws[0].withDB(db).insert(ctx)
	}

	st := ch.ws[0].st
	c := &compiler{d: db.d, args: &argBuilder{d: db.d}, table: st.name}

	names := make([]string, len(ch.cols))
	for i, col := range ch.cols {
		names[i] = c.d.QuoteIdent(col.Name())
	}
	tuples := make([]string, len(ch.ws))
	for i, w := range ch.ws {
		marks := make([]string, len(ch.cols))
		for j, col := range ch.cols {
			v, err := c.value(col, w.field(col).Interface())
			if err != nil {
				return 0, err
			}
			marks[j] = c.args.bind(v)
		}
		tuples[i] = "(" + strings.Join(marks, ", ") + ")"
	}

	sql := "INSERT INTO " + c.d.QuoteIdent(st.name) +
		" (" + strings.Join(names, ", ") + ") VALUES " + strings.Join(tuples, ", ")
	clause, err := c.conflict(ch.ws[0].conflict, ch.cols)
	if err != nil {
		return 0, err
	}
	if clause != "" {
		sql += " " + clause
	}

	back := ch.ws[0].returningColumns(ch.cols)
	if len(back) == 0 {
		res, err := db.ex.Exec(ctx, sql, c.args.args...)
		if err != nil {
			return 0, fmt.Errorf("orm: table %q: inserting: %w", st.name, err)
		}
		// The driver's count is consulted only where it can differ from the
		// number of rows given, which is the skipping upsert alone; see
		// writer.insert.
		if ch.ws[0].skips() {
			return res.RowsAffected, nil
		}
		return int64(len(ch.ws)), nil
	}

	backNames := make([]string, len(back))
	for i, col := range back {
		backNames[i] = c.d.QuoteIdent(col.Name())
	}
	sql += " RETURNING " + strings.Join(backNames, ", ")

	rows, err := db.ex.Query(ctx, sql, c.args.args...)
	if err != nil {
		return 0, fmt.Errorf("orm: table %q: inserting: %w", st.name, err)
	}
	defer rows.Close()

	// The n-th row back is taken to be the n-th row written.
	//
	// That is how a plain multi row VALUES insert behaves everywhere Tork
	// runs, and it is the only correlation available: the values being read
	// back are exactly the ones the caller did not supply, so there is
	// nothing in a returned row to match it to an input row by. What the SQL
	// standard guarantees is weaker than what implementations do, so the
	// assumption is worth stating: it holds for the statement written above,
	// and stops holding for one whose rows the database is free to skip or
	// reorder, which is why an upsert with anything to read back is chunked
	// down to one row per statement and never reaches here.
	for i, w := range ch.ws {
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return 0, fmt.Errorf("orm: table %q: inserting: %w", st.name, err)
			}
			return 0, fmt.Errorf("orm: table %q: insert wrote %d rows but returned %d",
				st.name, len(ch.ws), i)
		}
		if err := w.scanInto(rows, back); err != nil {
			return 0, err
		}
	}
	return int64(len(ch.ws)), rows.Err()
}

// deleteBatch removes one statement's worth of rows, identified by key.
//
// A table with a soft-delete column is updated rather than deleted, unless
// force is set, the same choice writer.delete makes for a single row.
func deleteBatch[E any](ctx context.Context, db *DB, st *tableState, ws []*writer[E], force bool) (int64, error) {
	pk := st.pk

	// A single column key is a list; a composite one has no list form, so it
	// becomes a comparison per row joined by OR. Both are ordinary
	// predicates, so the existing compiler renders them and there is no
	// second place that knows how a WHERE is written.
	var pred Predicate
	if len(pk) == 1 {
		values := make([]any, len(ws))
		for i, w := range ws {
			values[i] = w.field(pk[0]).Interface()
		}
		pred = InList{Col: pk[0], Values: values}
	} else {
		preds := make([]Predicate, len(ws))
		for i, w := range ws {
			terms := make([]Predicate, len(pk))
			for j, col := range pk {
				terms[j] = Comparison{Col: col, Op: OpEq, Value: w.field(col).Interface()}
			}
			preds[i] = Group{Conj: ConjAnd, Preds: terms}
		}
		pred = Group{Conj: ConjOr, Preds: preds}
	}

	c := &compiler{d: db.d, args: &argBuilder{d: db.d}, table: st.name}

	if !force && st.softDelete != nil {
		// Neither error is reachable: softDelete is always a real column of
		// this table, and pred is built entirely from st.pk, columns of this
		// same table. Checked anyway, for the reason writer.delete's
		// identical c.set call documents.
		assignments, err := c.set([]Assignment{{Col: st.softDelete, Value: time.Now()}})
		if err != nil {
			return 0, err
		}
		where, err := c.where([]Predicate{pred})
		if err != nil {
			return 0, err
		}
		sql := "UPDATE " + c.d.QuoteIdent(st.name) + " SET " + assignments + where
		res, err := db.ex.Exec(ctx, sql, c.args.args...)
		if err != nil {
			return 0, fmt.Errorf("orm: table %q: deleting: %w", st.name, err)
		}
		return res.RowsAffected, nil
	}

	where, err := c.where([]Predicate{pred})
	if err != nil {
		return 0, err
	}
	sql := "DELETE FROM " + c.d.QuoteIdent(st.name) + where

	res, err := db.ex.Exec(ctx, sql, c.args.args...)
	if err != nil {
		return 0, fmt.Errorf("orm: table %q: deleting: %w", st.name, err)
	}
	return res.RowsAffected, nil
}

// partialWrite reports a batch that touched fewer rows than it was given.
//
// A batch that wrote eight of ten rows has found eight rows that were there
// and two that were not, which is the same thing Update and Delete return
// ErrNoRows for. Returning the count alone would leave the caller to compare
// it against the length of a slice they may no longer be holding, so the
// count comes back either way and the shortfall is also an error.
func partialWrite(table, op, verb string, affected, given int64) error {
	if affected >= given {
		return nil
	}
	return fmt.Errorf("orm: table %q: %s %s %d of %d rows; the rest matched no row: %w",
		table, op, verb, affected, given, ErrNoRows)
}
