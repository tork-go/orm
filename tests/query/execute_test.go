package query_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// row builds one queued result row in the fixture's column order.
func row(id int, username string, email *string, age int, prefs []byte, at time.Time) []any {
	return []any{id, username, email, age, prefs, at}
}

func TestAll_ScansEveryRow(t *testing.T) {
	c := fakedriver.NewConn()
	email := "alice@example.com"
	at := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	c.QueueRows(
		row(1, "alice", &email, 30, []byte(`{"theme":"dark"}`), at),
		row(2, "bob", nil, 41, nil, at),
	)
	db := orm.NewDB(c, postgres.Dialect{})

	users, err := Users.With(db).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("All() returned %d rows, want 2", len(users))
	}
	if users[0].Username != "alice" || users[0].Email == nil || *users[0].Email != email {
		t.Errorf("row 0 = %+v", users[0])
	}
	if users[0].Prefs.Theme != "dark" {
		t.Errorf("row 0 prefs = %+v, want the decoded document", users[0].Prefs)
	}
	if users[1].Email != nil {
		t.Errorf("row 1 email = %v, want nil", users[1].Email)
	}
	if !users[1].CreatedAt.Equal(at) {
		t.Errorf("row 1 created_at = %v, want %v", users[1].CreatedAt, at)
	}
}

// Every row gets its own allocation. Slicing them out of one backing array
// would invalidate earlier pointers as it grew, and those pointers are
// what hooks and eager loading hold on to.
func TestAll_RowsAreDistinctAllocations(t *testing.T) {
	c := fakedriver.NewConn()
	at := time.Time{}
	c.QueueRows(
		row(1, "a", nil, 1, nil, at),
		row(2, "b", nil, 2, nil, at),
		row(3, "c", nil, 3, nil, at),
	)
	db := orm.NewDB(c, postgres.Dialect{})

	users, err := Users.With(db).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	seen := map[*User]bool{}
	for _, u := range users {
		if seen[u] {
			t.Fatal("All() returned the same pointer twice")
		}
		seen[u] = true
	}
	if users[0].ID != 1 || users[1].ID != 2 || users[2].ID != 3 {
		t.Errorf("rows aliased each other: %d %d %d", users[0].ID, users[1].ID, users[2].ID)
	}
}

func TestAll_NoRows(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	users, err := Users.With(db).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(users) != 0 {
		t.Errorf("All() returned %d rows, want none", len(users))
	}
}

// First adds its own LIMIT rather than using QueryRow, which cannot report
// no-rows through this driver contract.
func TestFirst_LimitsToOne(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(row(7, "alice", nil, 30, nil, time.Time{}))
	db := orm.NewDB(c, postgres.Dialect{})

	u, err := Users.With(db).Where(Users.Age.Gt(18)).First(context.Background())
	if err != nil {
		t.Fatalf("First() error = %v", err)
	}
	if u.ID != 7 {
		t.Errorf("First().ID = %d, want 7", u.ID)
	}
	calls := c.QueryCalls()
	if len(calls) != 1 || !strings.HasSuffix(calls[0], "LIMIT 1") {
		t.Errorf("First() ran %q, want it to end in LIMIT 1", calls)
	}
}

func TestFirst_NoRows(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := Users.With(db).First(context.Background())
	if !errors.Is(err, orm.ErrNoRows) {
		t.Errorf("First() error = %v, want ErrNoRows", err)
	}
}

// Building a First must not disturb the query it was built from.
func TestFirst_DoesNotMutateTheQuery(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(row(1, "a", nil, 1, nil, time.Time{}))
	db := orm.NewDB(c, postgres.Dialect{})

	q := Users.With(db).Where(Users.Age.Gt(18)).Limit(50)
	if _, err := q.First(context.Background()); err != nil {
		t.Fatalf("First() error = %v", err)
	}
	sql, _, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, "LIMIT 50") {
		t.Errorf("after First the query reads %s, want its own LIMIT 50 intact", sql)
	}
}

func TestCount(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{int64(42)})
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := Users.With(db).Where(Users.Age.Gt(18)).Count(context.Background())
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if n != 42 {
		t.Errorf("Count() = %d, want 42", n)
	}
	got := c.QueryCalls()[0]
	want := `SELECT COUNT(*) FROM "users" WHERE "age" > $1`
	if got != want {
		t.Errorf("Count() ran  %s\nwant         %s", got, want)
	}
}

// Ordering and paging change which rows come back, not how many match, so
// a count drops them rather than paying for a sort it discards.
func TestCount_DropsOrderingAndPaging(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{int64(1)})
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).OrderBy(Users.ID.Desc()).Limit(5).Count(context.Background()); err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	got := c.QueryCalls()[0]
	if strings.Contains(got, "ORDER BY") || strings.Contains(got, "LIMIT") {
		t.Errorf("Count() ran %s, want no ordering or paging", got)
	}
}

func TestExists(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(row(1, "a", nil, 1, nil, time.Time{}))
	db := orm.NewDB(c, postgres.Dialect{})

	ok, err := Users.With(db).Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !ok {
		t.Error("Exists() = false, want true")
	}

	empty := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	ok, err = Users.With(empty).Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if ok {
		t.Error("Exists() = true on an empty result, want false")
	}
}

func TestFind_ByPrimaryKey(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(row(42, "alice", nil, 30, nil, time.Time{}))
	db := orm.NewDB(c, postgres.Dialect{})

	u, err := Users.With(db).Find(context.Background(), 42)
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if u.ID != 42 {
		t.Errorf("Find().ID = %d, want 42", u.ID)
	}
	got := c.QueryCalls()[0]
	if !strings.Contains(got, `WHERE "id" = $1`) {
		t.Errorf("Find() ran %s, want a filter on the primary key", got)
	}
	if args := c.QueryArgs(0); len(args) != 1 || args[0] != 42 {
		t.Errorf("Find() bound %v, want [42]", args)
	}
}

func TestFind_NoRows(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := Users.With(db).Find(context.Background(), 42)
	if !errors.Is(err, orm.ErrNoRows) {
		t.Errorf("Find() error = %v, want ErrNoRows", err)
	}
}

// The key is checked before the statement is built, so a mismatch reads as
// one rather than as whatever the database says about a bad parameter.
func TestFind_WrongKeyType(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := Users.With(db).Find(context.Background(), "42")
	if err == nil {
		t.Fatal("Find() error = nil, want a key type error")
	}
	for _, want := range []string{"string", `"id"`, "int"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestFind_NilKey(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	if _, err := Users.With(db).Find(context.Background(), nil); err == nil {
		t.Error("Find(nil) produced no error")
	}
}

// A composite key has no single value to look up by.
func TestFind_CompositeKey(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := Memberships.With(db).Find(context.Background(), 1)
	if err == nil {
		t.Fatal("Find() error = nil, want a composite key error")
	}
	if !strings.Contains(err.Error(), "Where") {
		t.Errorf("error %q does not point at Where", err)
	}
}

func TestFind_NoPrimaryKey(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := Events.With(db).Find(context.Background(), 1)
	if err == nil {
		t.Fatal("Find() error = nil, want a missing primary key error")
	}
	if !strings.Contains(err.Error(), "declares none") {
		t.Errorf("error %q does not say the table has no primary key", err)
	}
}

// A model built with NewTable has no entity mapping to scan into.
func TestQuery_WithoutEntityMapping(t *testing.T) {
	type model struct {
		orm.Table[orm.NoEntity]
		ID *orm.IntColumn
	}
	m := &model{Table: orm.NewTable[orm.NoEntity]("legacy"), ID: orm.NewIntColumn("id")}
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	_, err := m.Table.With(db).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want a missing mapping error")
	}
	if !strings.Contains(err.Error(), "DefineTable") {
		t.Errorf("error %q does not point at DefineTable", err)
	}
}

func TestQuery_NilHandle(t *testing.T) {
	_, err := Users.With(nil).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want a missing handle error")
	}
	if !strings.Contains(err.Error(), "With") {
		t.Errorf("error %q does not point at With", err)
	}
}

// A builder error is kept and surfaces from the terminal, since a builder
// method cannot return one without breaking the chain.
func TestQuery_BuilderErrorSurfacesFromTerminal(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := Users.With(db).Limit(-1).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want the recorded builder error")
	}
	if !strings.Contains(err.Error(), "negative") {
		t.Errorf("error %q is not the builder error", err)
	}
}

func TestQuery_ReportsDriverFailure(t *testing.T) {
	c := fakedriver.NewConn()
	sql := `SELECT ` + userCols + ` FROM "users"`
	c.FailOn(sql)
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).All(context.Background()); err == nil {
		t.Error("All() error = nil, want the driver failure")
	}
}
