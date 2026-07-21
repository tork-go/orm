// Package fakedriver provides in-memory fakes for driver.Conn and
// driver.Dialect, so migrate's runner and CLI can be tested without a
// live database.
package fakedriver

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/tork-go/orm/driver"
	"github.com/tork-go/orm/schema"
)

// Conn is an in-memory fake driver.Conn.
type Conn struct {
	mu         sync.Mutex
	execCalls  []string
	queryCalls []string
	queryArgs  [][]any
	queued     [][][]any
	failOn     map[string]bool
	FailBegin  bool // if true, Begin returns an error, simulating a dropped connection

	// RowsAffected is what Exec reports back. Zero unless a test sets it.
	RowsAffected int64

	// RowsErr is what a result set reports from Err, so a test can
	// simulate a connection dropping partway through one. Real drivers
	// report a mid-iteration failure that way rather than from Next.
	RowsErr error
}

// NewConn returns a ready-to-use fake connection.
func NewConn() *Conn { return &Conn{failOn: map[string]bool{}} }

// FailOn makes Exec return an error whenever called with exactly this SQL
// string, so a test can simulate a migration failing partway through.
func (c *Conn) FailOn(sql string) { c.failOn[sql] = true }

// ExecCalls returns every SQL string passed to Exec (on the connection or
// any transaction from it), in call order.
func (c *Conn) ExecCalls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.execCalls))
	copy(out, c.execCalls)
	return out
}

// QueueRows sets the result the next Query returns. Each element is one
// row, and its values are handed to Scan in order, so they have to line up
// with the SELECT list under test.
//
// Without this a query layer could only be tested against a live database,
// since Query otherwise reports no rows at all.
func (c *Conn) QueueRows(rows ...[]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queued = append(c.queued, rows)
}

// QueryCalls returns every SQL string passed to Query, in call order.
func (c *Conn) QueryCalls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.queryCalls))
	copy(out, c.queryCalls)
	return out
}

// QueryArgs returns the bound arguments of the nth Query call.
func (c *Conn) QueryArgs(n int) []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n < 0 || n >= len(c.queryArgs) {
		return nil
	}
	return c.queryArgs[n]
}

func (c *Conn) Query(_ context.Context, sql string, args ...any) (driver.Rows, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queryCalls = append(c.queryCalls, sql)
	c.queryArgs = append(c.queryArgs, args)
	if c.failOn[sql] {
		return nil, errors.New("fakedriver: simulated Query failure")
	}
	if len(c.queued) == 0 {
		return &Rows{err: c.RowsErr}, nil
	}
	next := c.queued[0]
	c.queued = c.queued[1:]
	return &Rows{rows: next, err: c.RowsErr}, nil
}

func (c *Conn) QueryRow(_ context.Context, sql string, args ...any) driver.Row {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queryCalls = append(c.queryCalls, sql)
	c.queryArgs = append(c.queryArgs, args)
	if len(c.queued) == 0 || len(c.queued[0]) == 0 {
		return &Row{}
	}
	next := c.queued[0][0]
	c.queued = c.queued[1:]
	return &Row{values: next}
}

func (c *Conn) Exec(_ context.Context, sql string, _ ...any) (driver.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.execCalls = append(c.execCalls, sql)
	if c.failOn[sql] {
		return driver.Result{}, errors.New("fakedriver: simulated Exec failure")
	}
	return driver.Result{RowsAffected: c.RowsAffected}, nil
}

func (c *Conn) Begin(context.Context) (driver.Tx, error) {
	if c.FailBegin {
		return nil, errors.New("fakedriver: simulated Begin failure")
	}
	return &Tx{conn: c}, nil
}
func (c *Conn) Close(context.Context) error { return nil }

// Tx is an in-memory fake driver.Tx backed by its parent Conn.
type Tx struct {
	conn       *Conn
	Committed  bool
	RolledBack bool
}

func (t *Tx) Query(ctx context.Context, sql string, args ...any) (driver.Rows, error) {
	return t.conn.Query(ctx, sql, args...)
}
func (t *Tx) QueryRow(ctx context.Context, sql string, args ...any) driver.Row {
	return t.conn.QueryRow(ctx, sql, args...)
}
func (t *Tx) Exec(ctx context.Context, sql string, args ...any) (driver.Result, error) {
	return t.conn.Exec(ctx, sql, args...)
}
func (t *Tx) Commit(context.Context) error   { t.Committed = true; return nil }
func (t *Tx) Rollback(context.Context) error { t.RolledBack = true; return nil }

// Rows is a fake driver.Rows over a queued result set, empty unless the
// test queued one with Conn.QueueRows.
type Rows struct {
	rows   [][]any
	cursor int
	closed bool
	err    error
}

func (r *Rows) Next() bool {
	if r.cursor >= len(r.rows) {
		return false
	}
	r.cursor++
	return true
}

// Scan copies the current row into dest, which is what a real driver does
// with the pointers a caller hands it.
func (r *Rows) Scan(dest ...any) error {
	if r.cursor == 0 || r.cursor > len(r.rows) {
		return errors.New("fakedriver: Scan called outside a row")
	}
	return assign(r.rows[r.cursor-1], dest)
}

func (r *Rows) Err() error { return r.err }
func (r *Rows) Close()     { r.closed = true }

// Closed reports whether Close was called, so a test can check a query
// releases its cursor.
func (r *Rows) Closed() bool { return r.closed }

// Row is a fake driver.Row over a single queued row, empty unless the test
// queued one.
type Row struct {
	values []any
}

func (r *Row) Scan(dest ...any) error {
	if r.values == nil {
		return errors.New("fakedriver: no row")
	}
	return assign(r.values, dest)
}

// assign copies values into the pointers in dest, the same way a driver
// fills in the destinations a caller passed to Scan. Types have to match
// exactly; a mismatch is reported rather than coerced, since silently
// converting would hide the very bug a scan test is looking for.
func assign(values, dest []any) error {
	if len(values) != len(dest) {
		return fmt.Errorf("fakedriver: scanning %d values into %d destinations",
			len(values), len(dest))
	}
	for i, v := range values {
		d := reflect.ValueOf(dest[i])
		if d.Kind() != reflect.Pointer || d.IsNil() {
			return fmt.Errorf("fakedriver: destination %d is %T, want a non-nil pointer", i, dest[i])
		}
		target := d.Elem()
		if v == nil {
			target.SetZero()
			continue
		}
		rv := reflect.ValueOf(v)
		if !rv.Type().AssignableTo(target.Type()) {
			return fmt.Errorf("fakedriver: cannot scan %T into %s at position %d",
				v, target.Type(), i)
		}
		target.Set(rv)
	}
	return nil
}

// The four below are the fake's answers to orm.QueryDialect. They are
// deliberately unlike Postgres's: square brackets instead of double
// quotes, and a repeated question mark instead of a numbered parameter.
// A compiler test written against this fake therefore cannot accidentally
// pass by hard-coding Postgres's spelling, and the difference between the
// two dialects is visible in the expected SQL rather than implied.

// QuoteIdent wraps an identifier in square brackets.
func (*Dialect) QuoteIdent(name string) string { return "[" + name + "]" }

// Placeholder returns a positional question mark, ignoring n.
func (*Dialect) Placeholder(int) string { return "?" }

// RenderLike renders a LIKE comparison, spelling the case insensitive
// form with an explicit lower() rather than a dedicated operator.
func (*Dialect) RenderLike(quotedColumn, placeholder string, caseInsensitive bool) string {
	if caseInsensitive {
		return "lower(" + quotedColumn + ") LIKE lower(" + placeholder + ")"
	}
	return quotedColumn + " LIKE " + placeholder
}

// SupportsReturning reports false, so the no-RETURNING path is reachable
// from a test without a second driver.
func (*Dialect) SupportsReturning() bool { return false }

// Dialect is an in-memory fake driver.Dialect. Its history methods
// (InsertHistoryRow, DeleteHistoryRow, AppliedRevisions) are fully
// functional, backed by an in-memory map, for testing migrate's runner.
// Its Render* methods return short, recognizable strings identifying
// which method was called and with what, for testing Generate's
// dispatch. Connect returns a fresh Conn and Introspect returns
// IntrospectResult (empty by default); both can be made to fail, for
// testing the cli package's error handling.
type Dialect struct {
	mu               sync.Mutex
	rows             map[string]driver.AppliedRevision
	FailRender       bool // if true, the three error-returning Render* methods fail
	FailHistory      bool // if true, InsertHistoryRow and DeleteHistoryRow fail
	ConnectErr       error
	IntrospectErr    error
	IntrospectResult schema.Schema
}

// NewDialect returns a ready-to-use fake dialect with no applied revisions.
func NewDialect() *Dialect { return &Dialect{rows: map[string]driver.AppliedRevision{}} }

func (d *Dialect) Name() string { return "fake" }

func (d *Dialect) Connect(context.Context, string) (driver.Conn, error) {
	if d.ConnectErr != nil {
		return nil, d.ConnectErr
	}
	return NewConn(), nil
}

func (d *Dialect) Introspect(context.Context, driver.Conn, []string) (schema.Schema, error) {
	if d.IntrospectErr != nil {
		return schema.Schema{}, d.IntrospectErr
	}
	return d.IntrospectResult, nil
}

func (d *Dialect) EnsureHistoryTable(context.Context, driver.Conn) error { return nil }

func (d *Dialect) InsertHistoryRow(_ context.Context, _ driver.Execer, revision, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.FailHistory {
		return errors.New("fakedriver: simulated InsertHistoryRow failure")
	}
	d.rows[revision] = driver.AppliedRevision{Revision: revision, AppliedAt: time.Now()}
	return nil
}

func (d *Dialect) DeleteHistoryRow(_ context.Context, _ driver.Execer, revision string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.FailHistory {
		return errors.New("fakedriver: simulated DeleteHistoryRow failure")
	}
	delete(d.rows, revision)
	return nil
}

func (d *Dialect) AppliedRevisions(context.Context, driver.Execer) ([]driver.AppliedRevision, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]driver.AppliedRevision, 0, len(d.rows))
	for _, r := range d.rows {
		out = append(out, r)
	}
	return out, nil
}

// SeedApplied marks revision as already applied, without going through
// InsertHistoryRow, for setting up a test's starting state directly.
func (d *Dialect) SeedApplied(revision string, at time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rows[revision] = driver.AppliedRevision{Revision: revision, AppliedAt: at}
}

func (d *Dialect) RenderCreateTable(t schema.Table) ([]string, error) {
	if d.FailRender {
		return nil, errors.New("fakedriver: simulated render failure")
	}
	return []string{"CREATE TABLE " + t.Name}, nil
}
func (d *Dialect) RenderDropTable(table string) []string {
	return []string{"DROP TABLE " + table}
}
func (d *Dialect) RenderAddColumn(table string, col schema.Column) ([]string, error) {
	if d.FailRender {
		return nil, errors.New("fakedriver: simulated render failure")
	}
	return []string{"ADD COLUMN " + table + "." + col.Name}, nil
}
func (d *Dialect) RenderDropColumn(table, column string) []string {
	return []string{"DROP COLUMN " + table + "." + column}
}
func (d *Dialect) RenderAlterColumnType(table string, col schema.Column) ([]string, error) {
	if d.FailRender {
		return nil, errors.New("fakedriver: simulated render failure")
	}
	return []string{"ALTER COLUMN TYPE " + table + "." + col.Name}, nil
}
func (d *Dialect) RenderAlterColumnDefault(table, column, def string) []string {
	if def == "" {
		return []string{"ALTER " + table + " DROP DEFAULT " + column}
	}
	return []string{"ALTER " + table + " SET DEFAULT " + column + " " + def}
}

func (d *Dialect) RenderAlterColumnNullability(table, column string, notNull bool) []string {
	return []string{fmt.Sprintf("ALTER COLUMN NULLABILITY %s.%s %v", table, column, notNull)}
}
func (d *Dialect) RenderAddPrimaryKey(table string, pk schema.PrimaryKey) []string {
	return []string{"ADD PRIMARY KEY " + table + " " + pk.Name}
}
func (d *Dialect) RenderDropPrimaryKey(table, name string) []string {
	return []string{"DROP PRIMARY KEY " + name}
}
func (d *Dialect) RenderAddUnique(table string, u schema.UniqueConstraint) []string {
	return []string{"ADD UNIQUE " + u.Name}
}
func (d *Dialect) RenderDropUnique(table, name string) []string {
	return []string{"DROP UNIQUE " + name}
}
func (d *Dialect) RenderAddIndex(table string, idx schema.Index) []string {
	return []string{"ADD INDEX " + idx.Name}
}
func (d *Dialect) RenderDropIndex(table, name string) []string {
	return []string{"DROP INDEX " + name}
}
func (d *Dialect) RenderAddCheck(table string, c schema.Check) []string {
	return []string{"ADD CHECK " + c.Name}
}
func (d *Dialect) RenderDropCheck(table, name string) []string {
	return []string{"DROP CHECK " + name}
}
func (d *Dialect) RenderAddForeignKey(table string, fk schema.ForeignKey) []string {
	return []string{"ADD FOREIGN KEY " + fk.Name}
}
func (d *Dialect) RenderDropForeignKey(table, name string) []string {
	return []string{"DROP FOREIGN KEY " + name}
}
func (d *Dialect) RenderCreateEnumType(e schema.EnumType) []string {
	return []string{"CREATE ENUM TYPE " + e.Name}
}
func (d *Dialect) RenderDropEnumType(name string) []string {
	return []string{"DROP ENUM TYPE " + name}
}
func (d *Dialect) RenderAddEnumValue(name, value, before, after string) []string {
	return []string{"ADD ENUM VALUE " + name + "." + value}
}
