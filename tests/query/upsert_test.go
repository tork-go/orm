package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// The columns a user insert writes, and the one it reads back, written once
// so a fixture column does not have to be threaded through every expectation.
const (
	userInsertCols = `("username", "email", "age", "prefs", "created_at")`
	userInsertVals = `($1, $2, $3, $4, $5)`
)

func TestUpsert_Shapes(t *testing.T) {
	tests := []struct {
		name  string
		build func(*orm.Query[User]) *orm.Upsert[User]
		want  string
	}{
		{
			name:  "do nothing",
			build: func(q *orm.Query[User]) *orm.Upsert[User] { return q.OnConflict(Users.Username).DoNothing() },
			want:  `ON CONFLICT ("username") DO NOTHING`,
		},
		{
			name:  "do nothing, any conflict",
			build: func(q *orm.Query[User]) *orm.Upsert[User] { return q.OnConflict().DoNothing() },
			want:  `ON CONFLICT DO NOTHING`,
		},
		{
			name: "do nothing, several target columns",
			build: func(q *orm.Query[User]) *orm.Upsert[User] {
				return q.OnConflict(Users.Username, Users.Email).DoNothing()
			},
			want: `ON CONFLICT ("username", "email") DO NOTHING`,
		},
		{
			name: "do update, named columns",
			build: func(q *orm.Query[User]) *orm.Upsert[User] {
				return q.OnConflict(Users.Username).DoUpdate(Users.Age, Users.Email)
			},
			want: `ON CONFLICT ("username") DO UPDATE SET ` +
				`"age" = EXCLUDED."age", "email" = EXCLUDED."email"`,
		},
		{
			// Every column the insert wrote, less the target. The identity is
			// not among them because an insert never writes one.
			name:  "do update all",
			build: func(q *orm.Query[User]) *orm.Upsert[User] { return q.OnConflict(Users.Username).DoUpdateAll() },
			want: `ON CONFLICT ("username") DO UPDATE SET ` +
				`"email" = EXCLUDED."email", "age" = EXCLUDED."age", ` +
				`"prefs" = EXCLUDED."prefs", "created_at" = EXCLUDED."created_at"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fakedriver.NewConn()
			c.QueueRows([]any{7})
			db := orm.NewDB(c, postgres.Dialect{})

			n, err := tt.build(Users.With(db)).Insert(context.Background(), &User{Username: "a"})
			if err != nil {
				t.Fatalf("Insert() error = %v", err)
			}
			if n != 1 {
				t.Errorf("Insert() = %d, want 1", n)
			}
			want := `INSERT INTO "users" ` + userInsertCols + ` VALUES ` + userInsertVals +
				` ` + tt.want + ` RETURNING "id"`
			if got := c.QueryCalls()[0]; got != want {
				t.Errorf("ran  %s\nwant %s", got, want)
			}
		})
	}
}

// The clause sits between the values and the RETURNING, and the generated key
// still comes back into the row.
func TestUpsert_ReadsGeneratedValuesBack(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{99})
	db := orm.NewDB(c, postgres.Dialect{})

	u := &User{Username: "alice"}
	n, err := Users.With(db).OnConflict(Users.Username).DoUpdateAll().Insert(context.Background(), u)
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if n != 1 || u.ID != 99 {
		t.Errorf("Insert() = %d with id %d, want 1 and 99", n, u.ID)
	}
}

// A skipped row returns nothing, which is the statement working rather than
// failing, and the count is the only thing that says so.
func TestUpsert_DoNothingReportsASkip(t *testing.T) {
	c := fakedriver.NewConn() // nothing queued, so the statement returns no row
	db := orm.NewDB(c, postgres.Dialect{})

	u := &User{Username: "alice"}
	n, err := Users.With(db).OnConflict(Users.Username).DoNothing().Insert(context.Background(), u)
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if n != 0 {
		t.Errorf("Insert() = %d, want 0 for a row already there", n)
	}
	if u.ID != 0 {
		t.Errorf("id = %d, want the row left as it arrived", u.ID)
	}
}

// Anywhere else an insert that returned no row is one that did not happen.
func TestUpsert_DoUpdateStillNeedsItsRowBack(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := Users.With(db).OnConflict(Users.Username).DoUpdateAll().
		Insert(context.Background(), &User{Username: "alice"})
	if err == nil {
		t.Fatal("Insert() error = nil, want the missing row reported")
	}
	if !strings.Contains(err.Error(), "returned no row") {
		t.Errorf("error %q does not say the row never came back", err)
	}
}

func TestUpsert_InsertMany(t *testing.T) {
	t.Run("counts what was written", func(t *testing.T) {
		c := fakedriver.NewConn()
		// Three rows, and the middle one is skipped: each runs its own
		// statement, so each has its own result set.
		c.QueueRows([]any{1})
		c.QueueRows()
		c.QueueRows([]any{3})
		db := orm.NewDB(c, postgres.Dialect{})

		rows := []*User{{Username: "a"}, {Username: "b"}, {Username: "c"}}
		n, err := Users.With(db).OnConflict(Users.Username).DoNothing().
			InsertMany(context.Background(), rows...)
		if err != nil {
			t.Fatalf("InsertMany() error = %v", err)
		}
		if n != 2 {
			t.Errorf("InsertMany() = %d, want the two rows that were new", n)
		}
		if rows[0].ID != 1 || rows[1].ID != 0 || rows[2].ID != 3 {
			t.Errorf("ids = %d, %d, %d; want the skipped row left alone",
				rows[0].ID, rows[1].ID, rows[2].ID)
		}
	})

	// A shortfall is what an upsert is for, so it is not the error UpdateMany
	// reports for one.
	t.Run("a skip is not an error", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows()
		c.QueueRows()
		db := orm.NewDB(c, postgres.Dialect{})

		n, err := Users.With(db).OnConflict(Users.Username).DoNothing().
			InsertMany(context.Background(), &User{Username: "a"}, &User{Username: "b"})
		if err != nil {
			t.Fatalf("InsertMany() error = %v", err)
		}
		if n != 0 {
			t.Errorf("InsertMany() = %d, want 0", n)
		}
	})

	t.Run("no rows does nothing", func(t *testing.T) {
		c := fakedriver.NewConn()
		db := orm.NewDB(c, postgres.Dialect{})

		n, err := Users.With(db).OnConflict(Users.Username).DoNothing().
			InsertMany(context.Background())
		if err != nil {
			t.Fatalf("InsertMany() error = %v", err)
		}
		if n != 0 || len(c.QueryCalls()) != 0 || len(c.ExecCalls()) != 0 {
			t.Errorf("InsertMany() = %d and ran statements, want neither", n)
		}
	})
}

// With nothing to read back there is nothing to correlate, so the rows share
// one statement as an ordinary insert does.
func TestUpsert_BatchesWhenNothingComesBack(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := Memberships.With(db).OnConflict(Memberships.OrgID, Memberships.UserID).
		DoNothing().InsertMany(context.Background(),
		&Membership{OrgID: 1, UserID: 10},
		&Membership{OrgID: 1, UserID: 11},
	)
	if err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}
	calls := c.ExecCalls()
	if len(calls) != 1 {
		t.Fatalf("ran %d statements, want 1:\n%v", len(calls), calls)
	}
	want := `INSERT INTO "memberships" ("org_id", "user_id") VALUES ($1, $2), ($3, $4) ` +
		`ON CONFLICT ("org_id", "user_id") DO NOTHING`
	if calls[0] != want {
		t.Errorf("ran  %s\nwant %s", calls[0], want)
	}
	// The driver's count is what says how many were new, since the statement
	// covers several rows and reads none of them back.
	if n != 1 {
		t.Errorf("InsertMany() = %d, want the driver's count", n)
	}
}

// A plain insert of the same rows still reports what it was given, since only
// a skipping upsert can write fewer without failing.
func TestUpsert_APlainBatchCountsItsRows(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	err := Memberships.With(db).InsertMany(context.Background(),
		&Membership{OrgID: 1, UserID: 10}, &Membership{OrgID: 1, UserID: 11})
	if err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}
	if got := c.ExecCalls()[0]; strings.Contains(got, "CONFLICT") {
		t.Errorf("ran %s, want no conflict clause on a plain insert", got)
	}
}

// An upsert with something to read back cannot correlate returned rows to
// written ones, so it drops to one row per statement.
func TestUpsert_OneStatementPerRowWhenReadingBack(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1})
	c.QueueRows([]any{2})
	c.QueueRows([]any{3})
	db := orm.NewDB(c, postgres.Dialect{})

	rows := []*User{{Username: "a"}, {Username: "b"}, {Username: "c"}}
	if _, err := Users.With(db).OnConflict(Users.Username).DoUpdateAll().
		InsertMany(context.Background(), rows...); err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}
	calls := c.QueryCalls()
	if len(calls) != 3 {
		t.Fatalf("ran %d statements, want one per row:\n%v", len(calls), calls)
	}
	for i, s := range calls {
		if strings.Contains(s, "), (") {
			t.Errorf("statement %d covers several rows, which an upsert cannot read back: %s", i, s)
		}
	}
	if rows[0].ID != 1 || rows[1].ID != 2 || rows[2].ID != 3 {
		t.Errorf("ids = %d, %d, %d; want each row's own", rows[0].ID, rows[1].ID, rows[2].ID)
	}
	// Several statements, so they are wrapped to be one write.
	if len(c.Txs()) != 1 {
		t.Errorf("opened %d transactions, want 1", len(c.Txs()))
	}
}

// The hooks an ordinary insert runs still run, and BeforeCreate still runs
// before the column list is worked out.
func TestUpsert_RunsTheCreateHooks(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{5})
	db := orm.NewDB(c, postgres.Dialect{})

	p := &Post{Title: "Hello There"}
	n, err := Posts.With(db).OnConflict(Posts.Slug).DoUpdateAll().Insert(context.Background(), p)
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if n != 1 {
		t.Errorf("Insert() = %d, want 1", n)
	}
	if p.Slug != "hello-there" {
		t.Errorf("slug = %q, want the one BeforeCreate derived", p.Slug)
	}
	if p.ID != 5 {
		t.Errorf("id = %d, want the generated key read back", p.ID)
	}
	// The hook's column is the target, so it is not among those overwritten —
	// but its value is what the statement bound and what it conflicts on.
	got := c.QueryCalls()[0]
	if !strings.Contains(got, `ON CONFLICT ("slug") DO UPDATE SET "title" = EXCLUDED."title"`) {
		t.Errorf("ran %s, want the target left out of the overwrite", got)
	}
	if args := c.QueryArgs(0); len(args) != 2 || args[1] != "hello-there" {
		t.Errorf("bound %v, want the slug the hook derived", args)
	}
}

// Nothing about it may assume Postgres's spelling: the fake spells the whole
// clause differently, down to what it calls the row that was proposed.
func TestUpsert_AsksTheDialect(t *testing.T) {
	t.Run("do nothing", func(t *testing.T) {
		c := fakedriver.NewConn()
		db := orm.NewDB(c, fakedriver.NewDialect())

		if _, err := Memberships.With(db).OnConflict(Memberships.OrgID).DoNothing().
			Insert(context.Background(), &Membership{OrgID: 1, UserID: 2}); err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
		want := `INSERT INTO [memberships] ([org_id], [user_id]) VALUES (?, ?) UPSERT [org_id] IGNORE`
		if got := c.ExecCalls()[0]; got != want {
			t.Errorf("ran  %s\nwant %s", got, want)
		}
	})

	t.Run("do nothing, any conflict", func(t *testing.T) {
		c := fakedriver.NewConn()
		db := orm.NewDB(c, fakedriver.NewDialect())

		if _, err := Memberships.With(db).OnConflict().DoNothing().
			Insert(context.Background(), &Membership{OrgID: 1, UserID: 2}); err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
		if got := c.ExecCalls()[0]; !strings.HasSuffix(got, "UPSERT ANY IGNORE") {
			t.Errorf("ran %s, want the fake's spelling of any conflict", got)
		}
	})

	t.Run("do update", func(t *testing.T) {
		c := fakedriver.NewConn()
		db := orm.NewDB(c, fakedriver.NewDialect())

		if _, err := Events.With(db).OnConflict(Events.Name).DoUpdate(Events.Name).
			Insert(context.Background(), &Event{Name: "x"}); err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
		want := `INSERT INTO [events] ([name]) VALUES (?) UPSERT [name] REPLACE [name] <- NEW.[name]`
		if got := c.ExecCalls()[0]; got != want {
			t.Errorf("ran  %s\nwant %s", got, want)
		}
	})
}

func TestUpsert_Rejected(t *testing.T) {
	tests := map[string]struct {
		build func(*orm.Query[User]) *orm.Upsert[User]
		want  string
	}{
		"a nil target column": {
			build: func(q *orm.Query[User]) *orm.Upsert[User] {
				return q.OnConflict(Users.Username, nil).DoNothing()
			},
			want: "OnConflict column 1 is nil",
		},
		"DoUpdate with no columns": {
			build: func(q *orm.Query[User]) *orm.Upsert[User] {
				return q.OnConflict(Users.Username).DoUpdate()
			},
			want: "DoUpdate was given no columns",
		},
		"a nil column to overwrite": {
			build: func(q *orm.Query[User]) *orm.Upsert[User] {
				return q.OnConflict(Users.Username).DoUpdate(Users.Age, nil)
			},
			want: "DoUpdate column 1 is nil",
		},
		"another table's target column": {
			build: func(q *orm.Query[User]) *orm.Upsert[User] {
				return q.OnConflict(Posts.Title).DoNothing()
			},
			want: `belongs to table "posts"`,
		},
		"another table's column to overwrite": {
			build: func(q *orm.Query[User]) *orm.Upsert[User] {
				return q.OnConflict(Users.Username).DoUpdate(Posts.Title)
			},
			want: `belongs to table "posts"`,
		},
		"a target the dialect cannot overwrite on": {
			build: func(q *orm.Query[User]) *orm.Upsert[User] {
				return q.OnConflict().DoUpdateAll()
			},
			want: "which columns conflict",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
			_, err := tt.build(Users.With(db)).Insert(context.Background(), &User{Username: "a"})
			if err == nil {
				t.Fatal("Insert() error = nil, want the upsert rejected")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

// Every column a memberships insert writes is part of its primary key, and a
// key is what says which row is being written rather than what to write.
func TestUpsert_DoUpdateAllWithNothingToOverwrite(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	_, err := Memberships.With(db).OnConflict(Memberships.OrgID).DoUpdateAll().
		Insert(context.Background(), &Membership{OrgID: 1, UserID: 2})
	if err == nil {
		t.Fatal("Insert() error = nil, want the empty update reported")
	}
	if !strings.Contains(err.Error(), "nothing to overwrite") {
		t.Errorf("error %q does not explain what is missing", err)
	}
}

// A driver whose SQL cannot express an upsert says so, naming the operation,
// rather than emitting something close.
func TestUpsert_UnsupportedByTheDialect(t *testing.T) {
	tests := map[string]func(*orm.Query[Event]) *orm.Upsert[Event]{
		"do nothing": func(q *orm.Query[Event]) *orm.Upsert[Event] {
			return q.OnConflict(Events.Name).DoNothing()
		},
		"do update": func(q *orm.Query[Event]) *orm.Upsert[Event] {
			return q.OnConflict(Events.Name).DoUpdate(Events.Name)
		},
	}
	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			d := fakedriver.NewDialect()
			d.NoUpsert = true
			db := orm.NewDB(fakedriver.NewConn(), d)

			_, err := build(Events.With(db)).Insert(context.Background(), &Event{Name: "x"})
			if err == nil {
				t.Fatal("Insert() error = nil, want the dialect's refusal")
			}
			if !strings.Contains(err.Error(), "this database cannot") {
				t.Errorf("error %q is not the dialect's own", err)
			}
		})
	}
}

// The query an upsert was built from is untouched, so the same handle still
// writes plain inserts.
func TestUpsert_DoesNotChangeTheQueryItCameFrom(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})
	q := Memberships.With(db)

	if _, err := q.OnConflict(Memberships.OrgID).DoNothing().
		Insert(context.Background(), &Membership{OrgID: 1, UserID: 1}); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if err := q.InsertMany(context.Background(), &Membership{OrgID: 2, UserID: 2}); err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}
	calls := c.ExecCalls()
	if len(calls) != 2 {
		t.Fatalf("ran %d statements, want 2:\n%v", len(calls), calls)
	}
	if !strings.Contains(calls[0], "ON CONFLICT") {
		t.Errorf("the upsert lost its clause: %s", calls[0])
	}
	if strings.Contains(calls[1], "ON CONFLICT") {
		t.Errorf("the plain insert picked up a clause it never asked for: %s", calls[1])
	}
}

// A bad clause is reported from the batched statement too, not only from the
// one-row form that renders it in a different place.
func TestUpsert_RejectedFromABatchedStatement(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	_, err := Memberships.With(db).OnConflict(Posts.Title).DoNothing().
		InsertMany(context.Background(),
			&Membership{OrgID: 1, UserID: 1}, &Membership{OrgID: 1, UserID: 2})
	if err == nil {
		t.Fatal("InsertMany() error = nil, want the foreign column rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "posts"`) {
		t.Errorf("error %q does not name the other table", err)
	}
}

// A zero Table has no name to put in an error, and the error still has to
// arrive rather than panicking on the way.
func TestUpsert_ZeroTable(t *testing.T) {
	var tbl orm.Table[User]
	_, err := tbl.With(nil).OnConflict(nil).DoUpdate().
		Insert(context.Background(), &User{Username: "a"})
	if err == nil {
		t.Fatal("Insert() error = nil, want the nil column reported")
	}
	if !strings.Contains(err.Error(), "is nil") {
		t.Errorf("error %q does not name the nil column", err)
	}
}

// The query's own problems are reported before any row is looked at.
func TestUpsert_ReportsTheQuerysOwnProblems(t *testing.T) {
	var db *orm.DB
	_, err := Users.With(db).OnConflict(Users.Username).DoNothing().
		Insert(context.Background(), &User{Username: "a"})
	if err == nil {
		t.Fatal("Insert() error = nil, want the missing handle reported")
	}
	if !strings.Contains(err.Error(), "no database handle") {
		t.Errorf("error %q does not name the missing handle", err)
	}
}
