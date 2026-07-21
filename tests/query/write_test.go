package query_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

func ptr[T any](v T) *T { return &v }

// A generated key is never written to, so an insert never names it and
// reads it back instead.
func TestInsert_OmitsTheGeneratedKey(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{7}) // the RETURNING row
	db := orm.NewDB(c, postgres.Dialect{})

	u := &User{Username: "alice", Email: ptr("a@example.com"), Age: 30}
	if err := Users.With(db).Insert(context.Background(), u); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	got := c.QueryCalls()[0]
	want := `INSERT INTO "users" ("username", "email", "age", "prefs", "created_at")` +
		` VALUES ($1, $2, $3, $4, $5) RETURNING "id"`
	if got != want {
		t.Errorf("Insert ran  %s\nwant        %s", got, want)
	}
	if u.ID != 7 {
		t.Errorf("ID = %d, want 7 read back from RETURNING", u.ID)
	}
}

// A document column is encoded on the way in, the same way it is decoded
// on the way out.
func TestInsert_EncodesDocumentColumns(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1})
	db := orm.NewDB(c, postgres.Dialect{})

	u := &User{Username: "alice", Prefs: Prefs{Theme: "dark"}}
	if err := Users.With(db).Insert(context.Background(), u); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	args := c.QueryArgs(0)
	// username, email, age, prefs, created_at
	b, ok := args[3].([]byte)
	if !ok {
		t.Fatalf("prefs bound as %T, want []byte", args[3])
	}
	if string(b) != `{"theme":"dark"}` {
		t.Errorf("prefs bound as %s, want the encoded document", b)
	}
}

// Without RETURNING the key arrives as a plain integer instead.
func TestInsert_WithoutReturning(t *testing.T) {
	c := fakedriver.NewConn()
	c.LastInsertID = 99
	db := orm.NewDB(c, fakedriver.NewDialect()) // reports no RETURNING support

	u := &User{Username: "alice"}
	if err := Users.With(db).Insert(context.Background(), u); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if u.ID != 99 {
		t.Errorf("ID = %d, want 99 from the last insert id", u.ID)
	}
	if len(c.ExecCalls()) != 1 {
		t.Errorf("Insert ran %d statements, want one Exec", len(c.ExecCalls()))
	}
	if strings.Contains(c.ExecCalls()[0], "RETURNING") {
		t.Errorf("Insert ran %s, want no RETURNING on a dialect without it", c.ExecCalls()[0])
	}
}

type defaulted struct {
	ID      int
	Name    string
	Created time.Time
}

type defaultedModel struct {
	orm.Table[defaulted]
	ID      *orm.IntColumn
	Name    *orm.StringColumn
	Created *orm.TimeColumn
}

var Defaulted = orm.DefineTable[defaulted]("defaulted", func(t *orm.TableBuilder[defaulted]) *defaultedModel {
	return &defaultedModel{
		Table:   t.Table(),
		ID:      t.Int("id").PrimaryKey(),
		Name:    t.String("name").NotNull(),
		Created: t.Time("created").NotNull().ServerDefault("now()"),
	}
})

// A column with a server default is left out while its field is zero, so
// the database supplies it, and read back so the row in memory matches.
func TestInsert_ServerDefaultOmittedWhenZero(t *testing.T) {
	c := fakedriver.NewConn()
	at := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	c.QueueRows([]any{5, at})
	db := orm.NewDB(c, postgres.Dialect{})

	d := &defaulted{Name: "x"}
	if err := Defaulted.With(db).Insert(context.Background(), d); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	got := c.QueryCalls()[0]
	want := `INSERT INTO "defaulted" ("name") VALUES ($1) RETURNING "id", "created"`
	if got != want {
		t.Errorf("Insert ran  %s\nwant        %s", got, want)
	}
	if !d.Created.Equal(at) {
		t.Errorf("Created = %v, want the default read back", d.Created)
	}
}

// Setting the field explicitly means the caller's value is written, not
// the default.
func TestInsert_ServerDefaultIncludedWhenSet(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{5})
	db := orm.NewDB(c, postgres.Dialect{})

	at := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	d := &defaulted{Name: "x", Created: at}
	if err := Defaulted.With(db).Insert(context.Background(), d); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if !strings.Contains(c.QueryCalls()[0], `"created"`) {
		t.Errorf("Insert ran %s, want the explicit value written", c.QueryCalls()[0])
	}
}

type keyed struct {
	ID   uuid.UUID
	Name string
}

type keyedModel struct {
	orm.Table[keyed]
	ID   *orm.UUIDColumn
	Name *orm.StringColumn
}

var Keyed = orm.DefineTable[keyed]("keyed", func(t *orm.TableBuilder[keyed]) *keyedModel {
	return &keyedModel{
		Table: t.Table(),
		ID:    t.UUID("id").PrimaryKey().GeneratedByClient(uuid.New),
		Name:  t.String("name").NotNull(),
	}
})

// A key generated in Go is filled in before the statement runs, and lands
// in the caller's row as well as in the database.
func TestInsert_ClientGeneratedKey(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	k := &keyed{Name: "x"}
	if err := Keyed.With(db).Insert(context.Background(), k); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if k.ID == uuid.Nil {
		t.Fatal("ID is still the nil UUID, want the generated key")
	}
	args := c.ExecArgs(0)
	if len(args) != 2 || args[0] != k.ID {
		t.Errorf("Insert bound %v, want the generated key first", args)
	}
	// A client generated key is supplied, so there is nothing to read back
	// and the insert is a plain Exec.
	if len(c.QueryCalls()) != 0 {
		t.Errorf("Insert ran a query, want a plain Exec with nothing to return")
	}
}

func TestUpdate(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	u := &User{ID: 3, Username: "hasan", Age: 15}
	if err := Users.With(db).Update(context.Background(), u); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	got := c.ExecCalls()[0]
	want := `UPDATE "users" SET "username" = $1, "email" = $2, "age" = $3, ` +
		`"prefs" = $4, "created_at" = $5 WHERE "id" = $6`
	if got != want {
		t.Errorf("Update ran  %s\nwant        %s", got, want)
	}
	args := c.ExecArgs(0)
	if args[0] != "hasan" || args[2] != 15 || args[5] != 3 {
		t.Errorf("Update bound %v, want the new values then the key", args)
	}
}

// The row may not be there, and a write that changed nothing has to say so
// rather than report success.
func TestUpdate_NoSuchRow(t *testing.T) {
	c := fakedriver.NewConn() // RowsAffected stays zero
	db := orm.NewDB(c, postgres.Dialect{})
	err := Users.With(db).Update(context.Background(), &User{ID: 3})
	if !errors.Is(err, orm.ErrNoRows) {
		t.Errorf("Update() error = %v, want ErrNoRows", err)
	}
}

func TestDelete(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if err := Users.With(db).Delete(context.Background(), &User{ID: 3}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	got := c.ExecCalls()[0]
	want := `DELETE FROM "users" WHERE "id" = $1`
	if got != want {
		t.Errorf("Delete ran %s, want %s", got, want)
	}
	if args := c.ExecArgs(0); len(args) != 1 || args[0] != 3 {
		t.Errorf("Delete bound %v, want [3]", args)
	}
}

func TestDelete_NoSuchRow(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	err := Users.With(db).Delete(context.Background(), &User{ID: 3})
	if !errors.Is(err, orm.ErrNoRows) {
		t.Errorf("Delete() error = %v, want ErrNoRows", err)
	}
}

// Save tells a new row from a stored one by whether the generated key is
// still zero.
func TestSave_InsertsThenUpdates(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{11})
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	u := &User{Username: "alice"}
	if err := Users.With(db).Save(context.Background(), u); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if u.ID != 11 {
		t.Fatalf("ID = %d, want 11: Save should have inserted", u.ID)
	}
	if len(c.ExecCalls()) != 0 {
		t.Errorf("Save ran %v, want an insert", c.ExecCalls())
	}

	u.Username = "hasan"
	if err := Users.With(db).Save(context.Background(), u); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if len(c.ExecCalls()) != 1 || !strings.HasPrefix(c.ExecCalls()[0], "UPDATE") {
		t.Errorf("Save ran %v, want an update once the key was set", c.ExecCalls())
	}
}

// Without a generated key there is no signal for Save to read.
func TestSave_NeedsAGeneratedKey(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	err := Keyed.With(db).Save(context.Background(), &keyed{Name: "x"})
	if err == nil {
		t.Fatal("Save() error = nil, want it to report the missing signal")
	}
	for _, want := range []string{"generated key", "Insert or Update"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
