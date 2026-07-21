package orm_test

import (
	"context"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver"
	"github.com/tork-go/orm/tests/fakedriver"
)

// The aliases are what let orm name a connection without importing driver.
// If driver ever redeclared these instead, a driver.Conn would stop being
// an orm.Conn and query building could not accept one. Assigning across
// the two names only compiles while they are the same type.
func TestDriverTypesAreAliasesOfOrmTypes(t *testing.T) {
	var (
		_ orm.Conn    = (driver.Conn)(nil)
		_ driver.Conn = (orm.Conn)(nil)
		_ orm.Rows    = (driver.Rows)(nil)
		_ driver.Rows = (orm.Rows)(nil)
		_ orm.Execer  = (driver.Execer)(nil)
		_ orm.Tx      = (driver.Tx)(nil)
		_ orm.Row     = (driver.Row)(nil)
	)
	var r orm.Result = driver.Result{RowsAffected: 1}
	if r.RowsAffected != 1 {
		t.Errorf("RowsAffected = %d, want 1", r.RowsAffected)
	}
}

// A fake connection has to satisfy the contract for the same reason a real
// one does, so the compile time assertion is worth stating.
var _ orm.Conn = (*fakedriver.Conn)(nil)

func TestExec_ReportsRowsAffected(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 3

	res, err := c.Exec(context.Background(), "UPDATE users SET x = 1")
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if res.RowsAffected != 3 {
		t.Errorf("RowsAffected = %d, want 3", res.RowsAffected)
	}
}

// Queued rows are what make a query layer testable without a database.
func TestQueuedRows_ScanIntoDestinations(t *testing.T) {
	c := fakedriver.NewConn()
	email := "alice@example.com"
	c.QueueRows(
		[]any{1, "alice", &email},
		[]any{2, "bob", (*string)(nil)},
	)

	rows, err := c.Query(context.Background(), "SELECT id, username, email FROM users")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	defer rows.Close()

	type row struct {
		id    int
		name  string
		email *string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.name, &r.email); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("scanned %d rows, want 2", len(got))
	}
	if got[0].id != 1 || got[0].name != "alice" || got[0].email == nil || *got[0].email != email {
		t.Errorf("row 0 = %+v, want id 1, alice, %q", got[0], email)
	}
	if got[1].email != nil {
		t.Errorf("row 1 email = %v, want nil", got[1].email)
	}
}

func TestQueuedRows_RecordsSQLAndArgs(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1})

	const sql = `SELECT "id" FROM "users" WHERE "id" > $1`
	if _, err := c.Query(context.Background(), sql, 100); err != nil {
		t.Fatalf("Query() error = %v", err)
	}

	calls := c.QueryCalls()
	if len(calls) != 1 || calls[0] != sql {
		t.Errorf("QueryCalls() = %v, want the one statement", calls)
	}
	args := c.QueryArgs(0)
	if len(args) != 1 || args[0] != 100 {
		t.Errorf("QueryArgs(0) = %v, want [100]", args)
	}
}

// A type mismatch has to be reported rather than coerced, since silently
// converting would hide the very bug a scan test exists to catch.
func TestQueuedRows_TypeMismatchIsReported(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"not an int"})

	rows, _ := c.Query(context.Background(), "SELECT id FROM users")
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("Next() = false, want a queued row")
	}
	var id int
	if err := rows.Scan(&id); err == nil {
		t.Error("Scan() error = nil, want a type mismatch")
	}
}

func TestQueuedRows_ArityMismatchIsReported(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "alice"})

	rows, _ := c.Query(context.Background(), "SELECT id FROM users")
	defer rows.Close()
	rows.Next()
	var id int
	if err := rows.Scan(&id); err == nil {
		t.Error("Scan() error = nil, want an arity mismatch")
	}
}

func TestQuery_WithNothingQueuedReportsNoRows(t *testing.T) {
	c := fakedriver.NewConn()
	rows, err := c.Query(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Error("Next() = true on a connection with nothing queued")
	}
}
