// Package fakedriver provides in-memory fakes for driver.Conn and
// driver.Dialect, so migrate's runner and CLI can be tested without a
// live database.
package fakedriver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/tork-go/orm/driver"
	"github.com/tork-go/orm/schema"
)

// Conn is an in-memory fake driver.Conn.
type Conn struct {
	mu        sync.Mutex
	execCalls []string
	failOn    map[string]bool
	FailBegin bool // if true, Begin returns an error, simulating a dropped connection
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

func (c *Conn) Query(context.Context, string, ...any) (driver.Rows, error) { return &Rows{}, nil }
func (c *Conn) QueryRow(context.Context, string, ...any) driver.Row        { return &Row{} }

func (c *Conn) Exec(_ context.Context, sql string, _ ...any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.execCalls = append(c.execCalls, sql)
	if c.failOn[sql] {
		return errors.New("fakedriver: simulated Exec failure")
	}
	return nil
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
func (t *Tx) Exec(ctx context.Context, sql string, args ...any) error {
	return t.conn.Exec(ctx, sql, args...)
}
func (t *Tx) Commit(context.Context) error   { t.Committed = true; return nil }
func (t *Tx) Rollback(context.Context) error { t.RolledBack = true; return nil }

// Rows is an always-empty fake driver.Rows.
type Rows struct{}

func (*Rows) Next() bool        { return false }
func (*Rows) Scan(...any) error { return errors.New("fakedriver: no rows") }
func (*Rows) Err() error        { return nil }
func (*Rows) Close()            {}

// Row is an always-empty fake driver.Row.
type Row struct{}

func (*Row) Scan(...any) error { return errors.New("fakedriver: no row") }

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
