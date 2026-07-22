package query_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// A table whose every writable column is the database's to supply, so a row
// with nothing set writes no columns at all. Postgres spells that
// DEFAULT VALUES, which cannot carry a second row, so a batch of these has
// to fall back to one statement each.
type allDefaults struct {
	ID      int
	Created time.Time
}

type allDefaultsModel struct {
	orm.Table[allDefaults]
	ID      *orm.IntColumn
	Created *orm.TimeColumn
}

var AllDefaulted = orm.DefineTable[allDefaults]("all_defaults",
	func(t *orm.TableBuilder[allDefaults]) *allDefaultsModel {
		return &allDefaultsModel{
			Table:   t.Table(),
			ID:      t.Int("id").PrimaryKey(),
			Created: t.Time("created").NotNull().ServerDefault("now()"),
		}
	})

// A table whose hook fills in a column that would otherwise be left to a
// server default. Which columns a row writes therefore depends on the hook
// having run, which is what forces BeforeCreate to run before any column
// list is worked out.
type stamped struct {
	ID      int
	Name    string
	Created time.Time
}

type stampedModel struct {
	orm.Table[stamped]
	ID      *orm.IntColumn
	Name    *orm.StringColumn
	Created *orm.TimeColumn
}

var Stamped = orm.DefineTable[stamped]("stamped", func(t *orm.TableBuilder[stamped]) *stampedModel {
	return &stampedModel{
		Table:   t.Table(),
		ID:      t.Int("id").PrimaryKey(),
		Name:    t.String("name").NotNull(),
		Created: t.Time("created").NotNull().ServerDefault("now()"),
	}
})

// stampedAt is what the hook writes, fixed so a test can assert on it.
var stampedAt = time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

func (s *stamped) BeforeCreate(context.Context) error {
	s.Created = stampedAt
	return nil
}

// returning builds a handle whose dialect both supports RETURNING and has a
// bind ceiling, which no real dialect does: Postgres supports RETURNING but
// its ceiling of 65535 is too high to cross with a fixture anyone can read.
func returning(c *fakedriver.Conn, bindLimit int) *orm.DB {
	d := fakedriver.NewDialect()
	d.CanReturn = true
	d.BindLimit = bindLimit
	return orm.NewDB(c, d)
}

// Rows that write the same columns share one statement, with a tuple each.
func TestInsertMany_OneStatementForManyRows(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	err := Memberships.With(db).InsertMany(context.Background(),
		&Membership{OrgID: 1, UserID: 10},
		&Membership{OrgID: 1, UserID: 11},
		&Membership{OrgID: 2, UserID: 12},
	)
	if err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}

	calls := c.ExecCalls()
	if len(calls) != 1 {
		t.Fatalf("ran %d statements, want 1:\n%v", len(calls), calls)
	}
	want := `INSERT INTO "memberships" ("org_id", "user_id") VALUES ($1, $2), ($3, $4), ($5, $6)`
	if calls[0] != want {
		t.Errorf("InsertMany ran  %s\nwant            %s", calls[0], want)
	}
	args := c.ExecArgs(0)
	if len(args) != 6 || args[0] != 1 || args[1] != 10 || args[4] != 2 || args[5] != 12 {
		t.Errorf("InsertMany bound %v, want each row's values in order", args)
	}
	// One statement is already atomic, so there is nothing to wrap.
	if len(c.Txs()) != 0 {
		t.Errorf("opened %d transactions for a single statement, want none", len(c.Txs()))
	}
}

// Generated values come back into the row they belong to.
func TestInsertMany_ReadsGeneratedValuesBack(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{7}, []any{8}, []any{9})
	db := orm.NewDB(c, postgres.Dialect{})

	a := &User{Username: "a", Age: 1}
	b := &User{Username: "b", Age: 2}
	d := &User{Username: "c", Age: 3}
	if err := Users.With(db).InsertMany(context.Background(), a, b, d); err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}

	got := c.QueryCalls()[0]
	want := `INSERT INTO "users" ("username", "email", "age", "prefs", "created_at") VALUES ` +
		`($1, $2, $3, $4, $5), ($6, $7, $8, $9, $10), ($11, $12, $13, $14, $15) RETURNING "id"`
	if got != want {
		t.Errorf("InsertMany ran  %s\nwant            %s", got, want)
	}
	for i, u := range []*User{a, b, d} {
		if wantID := 7 + i; u.ID != wantID {
			t.Errorf("row %d got id %d, want %d: rows come back in the order they were written",
				i, u.ID, wantID)
		}
	}
}

// A column with a server default is left out while its field is zero, so
// rows of one type can want different columns. Each list gets its own
// statement rather than a value being forced on a row that had none.
func TestInsertMany_SplitsRowsByTheColumnsTheyWrite(t *testing.T) {
	c := fakedriver.NewConn()
	at := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	// The first statement writes the two rows leaving created to the
	// database, and reads both columns back; the second writes the one row
	// that supplied it, and reads back only the key.
	c.QueueRows([]any{1, at}, []any{3, at})
	c.QueueRows([]any{2})
	db := orm.NewDB(c, postgres.Dialect{})

	first := &defaulted{Name: "first"}
	middle := &defaulted{Name: "middle", Created: at}
	last := &defaulted{Name: "last"}
	if err := Defaulted.With(db).InsertMany(context.Background(), first, middle, last); err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}

	calls := c.QueryCalls()
	if len(calls) != 2 {
		t.Fatalf("ran %d statements, want one per column list:\n%v", len(calls), calls)
	}
	want := []string{
		`INSERT INTO "defaulted" ("name") VALUES ($1), ($2) RETURNING "id", "created"`,
		`INSERT INTO "defaulted" ("name", "created") VALUES ($1, $2) RETURNING "id"`,
	}
	for i, w := range want {
		if calls[i] != w {
			t.Errorf("statement %d was  %s\nwant              %s", i, calls[i], w)
		}
	}

	// Each row got its own key back, across the group boundary.
	if first.ID != 1 || middle.ID != 2 || last.ID != 3 {
		t.Errorf("ids = %d, %d, %d; want 1, 2, 3", first.ID, middle.ID, last.ID)
	}
	if !first.Created.Equal(at) || !last.Created.Equal(at) {
		t.Error("the rows that left created to the database did not read it back")
	}
	// Groups keep the order they first appeared in, so output is stable.
	if len(c.Txs()) != 1 {
		t.Errorf("opened %d transactions, want 1 covering both statements", len(c.Txs()))
	}
}

// A statement cannot bind more parameters than the driver allows, so a big
// batch becomes as many statements as it takes.
func TestInsertMany_ChunksUnderTheBindCeiling(t *testing.T) {
	c := fakedriver.NewConn()
	// Two columns a row, four parameters a statement: two rows each.
	db := orm.NewDB(c, func() orm.QueryDialect {
		d := fakedriver.NewDialect()
		d.BindLimit = 4
		return d
	}())

	rows := make([]*Membership, 5)
	for i := range rows {
		rows[i] = &Membership{OrgID: 1, UserID: i}
	}
	if err := Memberships.With(db).InsertMany(context.Background(), rows...); err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}

	calls := c.ExecCalls()
	if len(calls) != 3 {
		t.Fatalf("ran %d statements for 5 rows at 2 a statement, want 3:\n%v", len(calls), calls)
	}
	wantTuples := []int{2, 2, 1}
	for i, n := range wantTuples {
		if got := strings.Count(calls[i], "("); got != n+1 { // +1 for the column list
			t.Errorf("statement %d has %d tuples, want %d: %s", i, got-1, n, calls[i])
		}
	}
	// Several statements are not atomic on their own, so they are wrapped.
	if len(c.Txs()) != 1 {
		t.Fatalf("opened %d transactions, want 1", len(c.Txs()))
	}
	if !c.Txs()[0].Committed {
		t.Error("the transaction covering the chunks was not committed")
	}
}

// Chunking must not lose track of which returned row belongs to which
// written row, across the statement boundary.
func TestInsertMany_ReadsValuesBackAcrossChunks(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1}, []any{2})
	c.QueueRows([]any{3}, []any{4})
	c.QueueRows([]any{5})
	// Five columns a row, ten parameters a statement: two rows each.
	db := returning(c, 10)

	rows := make([]*User, 5)
	for i := range rows {
		rows[i] = &User{Username: "u", Age: i}
	}
	if err := Users.With(db).InsertMany(context.Background(), rows...); err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}
	if len(c.QueryCalls()) != 3 {
		t.Fatalf("ran %d statements, want 3", len(c.QueryCalls()))
	}
	for i, u := range rows {
		if u.ID != i+1 {
			t.Errorf("row %d got id %d, want %d", i, u.ID, i+1)
		}
	}
}

// Without RETURNING there is no way to tell which generated value belongs to
// which row of a statement covering several, so each row gets its own.
func TestInsertMany_FallsBackToOneRowAtATimeWithoutReturning(t *testing.T) {
	c := fakedriver.NewConn()
	c.LastInsertID = 42
	db := orm.NewDB(c, fakedriver.NewDialect()) // reports no RETURNING

	rows := []*User{{Username: "a"}, {Username: "b"}, {Username: "c"}}
	if err := Users.With(db).InsertMany(context.Background(), rows...); err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}

	calls := c.ExecCalls()
	if len(calls) != 3 {
		t.Fatalf("ran %d statements, want one per row:\n%v", len(calls), calls)
	}
	for i, s := range calls {
		if strings.Contains(s, "), (") {
			t.Errorf("statement %d covers several rows, which cannot report their keys: %s", i, s)
		}
		if strings.Contains(s, "RETURNING") {
			t.Errorf("statement %d used RETURNING on a dialect without it: %s", i, s)
		}
	}
	for i, u := range rows {
		if u.ID != 42 {
			t.Errorf("row %d got id %d, want the last insert id", i, u.ID)
		}
	}
}

// A dialect without RETURNING can still write many rows at once when there
// is nothing to read back.
func TestInsertMany_BatchesWithoutReturningWhenNothingComesBack(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, fakedriver.NewDialect())

	err := Memberships.With(db).InsertMany(context.Background(),
		&Membership{OrgID: 1, UserID: 1}, &Membership{OrgID: 1, UserID: 2})
	if err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}
	if len(c.ExecCalls()) != 1 {
		t.Errorf("ran %d statements, want 1: nothing needs reading back", len(c.ExecCalls()))
	}
}

// A row whose every column is the database's to supply writes no columns at
// all, which Postgres spells DEFAULT VALUES. That form carries no value list
// and so cannot hold a second row.
func TestInsertMany_RowsWritingNoColumns(t *testing.T) {
	c := fakedriver.NewConn()
	at := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	c.QueueRows([]any{1, at})
	c.QueueRows([]any{2, at})
	db := orm.NewDB(c, postgres.Dialect{})

	first, second := &allDefaults{}, &allDefaults{}
	if err := AllDefaulted.With(db).InsertMany(context.Background(), first, second); err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}

	calls := c.QueryCalls()
	if len(calls) != 2 {
		t.Fatalf("ran %d statements, want one per row:\n%v", len(calls), calls)
	}
	want := `INSERT INTO "all_defaults" DEFAULT VALUES RETURNING "id", "created"`
	for i, got := range calls {
		if got != want {
			t.Errorf("statement %d was %s, want %s", i, got, want)
		}
	}
	if first.ID != 1 || second.ID != 2 {
		t.Errorf("ids = %d, %d; want 1, 2", first.ID, second.ID)
	}
}

// A batch of one writes exactly the statement Insert would.
func TestInsertMany_OfOneMatchesInsert(t *testing.T) {
	batch := fakedriver.NewConn()
	batch.QueueRows([]any{7})
	if err := Users.With(orm.NewDB(batch, postgres.Dialect{})).
		InsertMany(context.Background(), &User{Username: "alice", Age: 30}); err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}

	single := fakedriver.NewConn()
	single.QueueRows([]any{7})
	if err := Users.With(orm.NewDB(single, postgres.Dialect{})).
		Insert(context.Background(), &User{Username: "alice", Age: 30}); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	if batch.QueryCalls()[0] != single.QueryCalls()[0] {
		t.Errorf("a batch of one differs from Insert:\n  %s\n  %s",
			batch.QueryCalls()[0], single.QueryCalls()[0])
	}
}

// A batch of nothing is ordinary caller code, not a mistake.
func TestBulk_EmptyBatchesDoNothing(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})
	ctx := context.Background()

	if err := Users.With(db).InsertMany(ctx); err != nil {
		t.Errorf("InsertMany() error = %v, want nil", err)
	}
	if n, err := Users.With(db).UpdateMany(ctx); err != nil || n != 0 {
		t.Errorf("UpdateMany() = %d, %v; want 0, nil", n, err)
	}
	if n, err := Users.With(db).DeleteMany(ctx); err != nil || n != 0 {
		t.Errorf("DeleteMany() = %d, %v; want 0, nil", n, err)
	}
	if len(c.ExecCalls())+len(c.QueryCalls()) != 0 {
		t.Errorf("an empty batch ran %v / %v", c.ExecCalls(), c.QueryCalls())
	}
}

// Hooks belong to the rows, so every row gets its own, on both sides of the
// statement.
func TestInsertMany_FiresHooksPerRow(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1}, []any{2})
	db := orm.NewDB(c, postgres.Dialect{})

	first := &Post{Title: "Hello World"}
	second := &Post{Title: "Second Post"}
	if err := Posts.With(db).InsertMany(context.Background(), first, second); err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}
	for _, p := range []*Post{first, second} {
		if p.Slug != slugify(p.Title) {
			t.Errorf("%q got slug %q, want BeforeCreate to have set it", p.Title, p.Slug)
		}
		if got := p.fired; len(got) != 2 || got[0] != "BeforeCreate" || got[1] != "AfterCreate" {
			t.Errorf("%q fired %v, want BeforeCreate then AfterCreate", p.Title, got)
		}
	}
}

// A hook that fills in a field changes whether that field is still zero, and
// so whether its column is written at all. Working out the column list first
// would write the value the hook replaced, or leave it to the database
// having just been given one.
func TestInsertMany_RunsHooksBeforeChoosingColumns(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1}, []any{2})
	db := orm.NewDB(c, postgres.Dialect{})

	rows := []*stamped{{Name: "a"}, {Name: "b"}}
	if err := Stamped.With(db).InsertMany(context.Background(), rows...); err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}

	got := c.QueryCalls()[0]
	want := `INSERT INTO "stamped" ("name", "created") VALUES ($1, $2), ($3, $4) RETURNING "id"`
	if got != want {
		t.Errorf("InsertMany ran  %s\nwant            %s\n"+
			"the hook set created, so its column must be written rather than defaulted", got, want)
	}
	for i, r := range rows {
		if !r.Created.Equal(stampedAt) {
			t.Errorf("row %d created = %v, want the hook's value", i, r.Created)
		}
	}
}

// A key generated in Go is filled in per row before the statement runs.
func TestInsertMany_ClientGeneratedKeys(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	rows := []*keyed{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	if err := Keyed.With(db).InsertMany(context.Background(), rows...); err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}

	seen := map[uuid.UUID]bool{}
	for i, k := range rows {
		if k.ID == uuid.Nil {
			t.Errorf("row %d has the nil UUID, want a generated key", i)
		}
		if seen[k.ID] {
			t.Errorf("row %d reused key %s", i, k.ID)
		}
		seen[k.ID] = true
	}
	if len(c.ExecCalls()) != 1 {
		t.Errorf("ran %d statements, want 1: nothing needs reading back", len(c.ExecCalls()))
	}
	args := c.ExecArgs(0)
	if len(args) != 6 || args[0] != rows[0].ID || args[2] != rows[1].ID {
		t.Errorf("bound %v, want each row's generated key", args)
	}
}

// A document column is encoded for every row, the same way one row is.
func TestInsertMany_EncodesDocumentColumns(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1}, []any{2})
	db := orm.NewDB(c, postgres.Dialect{})

	err := Users.With(db).InsertMany(context.Background(),
		&User{Username: "a", Prefs: Prefs{Theme: "dark"}},
		&User{Username: "b", Prefs: Prefs{Theme: "light"}},
	)
	if err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}
	args := c.QueryArgs(0)
	// Five columns a row, prefs fourth.
	for i, want := range []string{`{"theme":"dark"}`, `{"theme":"light"}`} {
		b, ok := args[i*5+3].([]byte)
		if !ok {
			t.Fatalf("row %d prefs bound as %T, want []byte", i, args[i*5+3])
		}
		if string(b) != want {
			t.Errorf("row %d prefs bound as %s, want %s", i, b, want)
		}
	}
}

func TestUpdateMany_OneStatementPerRow(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := Users.With(db).UpdateMany(context.Background(),
		&User{ID: 1, Username: "a", Age: 10},
		&User{ID: 2, Username: "b", Age: 20},
	)
	if err != nil {
		t.Fatalf("UpdateMany() error = %v", err)
	}
	if n != 2 {
		t.Errorf("UpdateMany() = %d, want 2", n)
	}

	calls := c.ExecCalls()
	if len(calls) != 2 {
		t.Fatalf("ran %d statements, want one per row:\n%v", len(calls), calls)
	}
	want := `UPDATE "users" SET "username" = $1, "email" = $2, "age" = $3, ` +
		`"prefs" = $4, "created_at" = $5 WHERE "id" = $6`
	for i, got := range calls {
		if got != want {
			t.Errorf("statement %d was  %s\nwant              %s", i, got, want)
		}
	}
	if first, second := c.ExecArgs(0), c.ExecArgs(1); first[5] != 1 || second[5] != 2 {
		t.Errorf("keys bound %v then %v, want each row's own", first[5], second[5])
	}
	// Several statements have to be all or nothing.
	if len(c.Txs()) != 1 || !c.Txs()[0].Committed {
		t.Errorf("opened %d transactions, want 1 committed", len(c.Txs()))
	}
}

func TestUpdateMany_FiresHooksPerRow(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	first := &Post{ID: 1, Title: "Hello World"}
	second := &Post{ID: 2, Title: "Second Post"}
	if _, err := Posts.With(db).UpdateMany(context.Background(), first, second); err != nil {
		t.Fatalf("UpdateMany() error = %v", err)
	}
	for _, p := range []*Post{first, second} {
		if p.Slug != slugify(p.Title) {
			t.Errorf("%q got slug %q, want BeforeUpdate to have set it", p.Title, p.Slug)
		}
		if got := p.fired; len(got) != 2 || got[0] != "BeforeUpdate" || got[1] != "AfterUpdate" {
			t.Errorf("%q fired %v, want BeforeUpdate then AfterUpdate", p.Title, got)
		}
	}
}

// One row is already atomic.
func TestUpdateMany_OfOneOpensNoTransaction(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).UpdateMany(context.Background(), &User{ID: 1}); err != nil {
		t.Fatalf("UpdateMany() error = %v", err)
	}
	if len(c.Txs()) != 0 {
		t.Errorf("opened %d transactions for one statement, want none", len(c.Txs()))
	}
}

func TestDeleteMany_ListsTheKeys(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 3
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := Users.With(db).DeleteMany(context.Background(),
		&User{ID: 1}, &User{ID: 2}, &User{ID: 3})
	if err != nil {
		t.Fatalf("DeleteMany() error = %v", err)
	}
	if n != 3 {
		t.Errorf("DeleteMany() = %d, want 3", n)
	}

	calls := c.ExecCalls()
	if len(calls) != 1 {
		t.Fatalf("ran %d statements, want 1:\n%v", len(calls), calls)
	}
	want := `DELETE FROM "users" WHERE "id" IN ($1, $2, $3)`
	if calls[0] != want {
		t.Errorf("DeleteMany ran  %s\nwant            %s", calls[0], want)
	}
	if args := c.ExecArgs(0); len(args) != 3 || args[0] != 1 || args[2] != 3 {
		t.Errorf("bound %v, want the three keys", args)
	}
}

// A composite key has no list form, so it becomes a comparison per row.
func TestDeleteMany_CompositeKey(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 2
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := Memberships.With(db).DeleteMany(context.Background(),
		&Membership{OrgID: 1, UserID: 10},
		&Membership{OrgID: 2, UserID: 20},
	)
	if err != nil {
		t.Fatalf("DeleteMany() error = %v", err)
	}
	if n != 2 {
		t.Errorf("DeleteMany() = %d, want 2", n)
	}
	want := `DELETE FROM "memberships" WHERE (("org_id" = $1 AND "user_id" = $2) ` +
		`OR ("org_id" = $3 AND "user_id" = $4))`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("DeleteMany ran  %s\nwant            %s", got, want)
	}
	if args := c.ExecArgs(0); len(args) != 4 || args[0] != 1 || args[3] != 20 {
		t.Errorf("bound %v, want each row's key pair", args)
	}
}

// One value per key column per row, so a composite key fits fewer rows into
// a statement than a single column one.
func TestDeleteMany_ChunksUnderTheBindCeiling(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 2
	d := fakedriver.NewDialect()
	d.BindLimit = 4
	db := orm.NewDB(c, d)

	rows := make([]*Membership, 4)
	for i := range rows {
		rows[i] = &Membership{OrgID: 1, UserID: i}
	}
	n, err := Memberships.With(db).DeleteMany(context.Background(), rows...)
	if err != nil {
		t.Fatalf("DeleteMany() error = %v", err)
	}

	calls := c.ExecCalls()
	if len(calls) != 2 {
		t.Fatalf("ran %d statements for 4 rows at 2 a statement, want 2:\n%v", len(calls), calls)
	}
	// The counts each statement reported are added up.
	if n != 4 {
		t.Errorf("DeleteMany() = %d, want the statements' counts added up", n)
	}
	for i, s := range calls {
		if got := strings.Count(s, " OR "); got != 1 {
			t.Errorf("statement %d joins %d pairs with OR, want 2 rows: %s", i, got+1, s)
		}
	}
	if len(c.Txs()) != 1 {
		t.Errorf("opened %d transactions, want 1", len(c.Txs()))
	}
}

// A batch inside a transaction the caller opened joins it rather than
// starting a second one, so the whole block is one unit.
func TestBulk_JoinsAnOpenTransaction(t *testing.T) {
	ctx := context.Background()
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	d := fakedriver.NewDialect()
	d.BindLimit = 4 // two rows a statement, so the insert would open its own
	db := orm.NewDB(c, d)

	rows := make([]*Membership, 5)
	for i := range rows {
		rows[i] = &Membership{OrgID: 1, UserID: i}
	}

	err := db.Transaction(ctx, func(tx *orm.DB) error {
		if err := Memberships.With(tx).InsertMany(ctx, rows...); err != nil {
			return err
		}
		_, err := Memberships.With(tx).Where(Memberships.OrgID.Equals(1)).DeleteAll(ctx)
		return err
	})
	if err != nil {
		t.Fatalf("Transaction() error = %v", err)
	}
	if len(c.Txs()) != 1 {
		t.Errorf("started %d transactions, want 1: the batch should have joined", len(c.Txs()))
	}
	if !c.Txs()[0].Committed {
		t.Error("the transaction was not committed")
	}
}

func TestDeleteMany_FiresHooksPerRow(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 2
	db := orm.NewDB(c, postgres.Dialect{})

	first := &Post{ID: 1, Title: "a"}
	second := &Post{ID: 2, Title: "b"}
	if _, err := Posts.With(db).DeleteMany(context.Background(), first, second); err != nil {
		t.Fatalf("DeleteMany() error = %v", err)
	}
	for _, p := range []*Post{first, second} {
		if got := p.fired; len(got) != 2 || got[0] != "BeforeDelete" || got[1] != "AfterDelete" {
			t.Errorf("row %d fired %v, want BeforeDelete then AfterDelete", p.ID, got)
		}
	}
}
