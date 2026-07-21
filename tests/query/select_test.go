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

func TestSelect_NarrowsTheColumns(t *testing.T) {
	sql, _, err := Users.With(pg()).Select(Users.ID, Users.Username).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "username" FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// The columns come out in the order they were given, not in the model's, since
// that order is what the scanner reads back.
func TestSelect_KeepsTheOrderGiven(t *testing.T) {
	sql, _, err := Users.With(pg()).Select(Users.Username, Users.ID).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT "username", "id" FROM "users"`; sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

func TestSelect_Accumulates(t *testing.T) {
	sql, _, err := Users.With(pg()).Select(Users.ID).Select(Users.Username).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT "id", "username" FROM "users"`; sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// A field whose column was not read keeps its zero value. That is the cost of
// a projection returning the row type, and is worth pinning down.
func TestSelect_UnreadFieldsAreZero(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{7, "alice"})
	db := orm.NewDB(c, postgres.Dialect{})

	users, err := Users.With(db).Select(Users.ID, Users.Username).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("All() returned %d rows, want 1", len(users))
	}
	if users[0].ID != 7 || users[0].Username != "alice" {
		t.Errorf("read %+v, want the selected columns filled", users[0])
	}
	if users[0].Age != 0 || users[0].Email != nil || !users[0].CreatedAt.IsZero() {
		t.Errorf("read %+v, want everything unselected left zero", users[0])
	}
}

func TestSelect_Rejected(t *testing.T) {
	tests := map[string]struct {
		build func() *orm.Filtered[User]
		want  string
	}{
		"no columns": {
			build: func() *orm.Filtered[User] { return Users.With(pg()).Select() },
			want:  "was given no columns",
		},
		"a nil column": {
			build: func() *orm.Filtered[User] { return Users.With(pg()).Select(Users.ID, nil) },
			want:  "column 1 is nil",
		},
		"another table's column": {
			build: func() *orm.Filtered[User] { return Users.With(pg()).Select(Posts.Title) },
			want:  `belongs to table "posts"`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := tt.build().SQL()
			if err == nil {
				t.Fatal("SQL() error = nil, want the projection rejected")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

func TestDistinct(t *testing.T) {
	sql, _, err := Users.With(pg()).Distinct().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasPrefix(sql, "SELECT DISTINCT "+userCols) {
		t.Errorf("SQL() = %s, want a DISTINCT over every column", sql)
	}

	sql, _, err = Users.With(pg()).Select(Users.Username).Distinct().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT DISTINCT "username" FROM "users"`; sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// Counting a distinct query counts the rows that query returns, which is a
// count over the read rather than over the table.
func TestCount_Distinct(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{int64(3)})
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := Users.With(db).Select(Users.Username).Distinct().
		Where(Users.Age.Gt(18)).Count(context.Background())
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if n != 3 {
		t.Errorf("Count() = %d, want 3", n)
	}
	want := `SELECT COUNT(*) FROM (SELECT DISTINCT "username" FROM "users" ` +
		`WHERE "age" > $1) AS "t"`
	if got := c.QueryCalls()[0]; got != want {
		t.Errorf("Count ran  %s\nwant       %s", got, want)
	}
}

// Without Distinct it stays the plain count it always was.
func TestCount_PlainIsUnchanged(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{int64(9)})
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).Where(Users.Age.Gt(18)).Count(context.Background()); err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if want := `SELECT COUNT(*) FROM "users" WHERE "age" > $1`; c.QueryCalls()[0] != want {
		t.Errorf("Count ran %s, want %s", c.QueryCalls()[0], want)
	}
}

// A projection is a read concept; a write is told what to do by its
// assignments, so carrying one into a set operation is a mistake.
func TestSetOps_RejectProjectionAndDistinct(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	ctx := context.Background()

	tests := map[string]struct {
		query func() *orm.Filtered[User]
		want  string
	}{
		"Select":   {func() *orm.Filtered[User] { return Users.With(db).Select(Users.ID) }, "a Select"},
		"Distinct": {func() *orm.Filtered[User] { return Users.With(db).Distinct() }, "a Distinct"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := tt.query().Where(Users.ID.Eq(1)).DeleteAll(ctx); err == nil {
				t.Error("DeleteAll() error = nil, want the clause rejected")
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not name %s", err, tt.want)
			}
		})
	}
}

// ── orm.Select, the typed single-column read ────────────────────────────

func TestScalars_Typed(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice"}, []any{"bob"})
	db := orm.NewDB(c, postgres.Dialect{})

	// The result is []string, not []*User with one field filled.
	names, err := orm.Select(Users.With(db), Users.Username).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	var _ []string = names
	if len(names) != 2 || names[0] != "alice" || names[1] != "bob" {
		t.Errorf("All() = %v, want [alice bob]", names)
	}
	if want := `SELECT "username" FROM "users"`; c.QueryCalls()[0] != want {
		t.Errorf("ran %s, want %s", c.QueryCalls()[0], want)
	}
}

// A nullable column is a Column[*T], so its values come back as pointers and a
// NULL is a nil rather than an empty string.
func TestScalars_NullableGivesPointers(t *testing.T) {
	c := fakedriver.NewConn()
	email := "alice@example.com"
	c.QueueRows([]any{&email}, []any{nil})
	db := orm.NewDB(c, postgres.Dialect{})

	emails, err := orm.Select(Users.With(db), Users.Email).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	var _ []*string = emails
	if len(emails) != 2 {
		t.Fatalf("All() returned %d values, want 2", len(emails))
	}
	if emails[0] == nil || *emails[0] != email {
		t.Errorf("first = %v, want the address", emails[0])
	}
	if emails[1] != nil {
		t.Errorf("second = %v, want nil for the NULL", emails[1])
	}
}

// The whole query still applies: conditions, ordering and paging.
func TestScalars_CarriesTheQuery(t *testing.T) {
	sql, args, err := orm.Select(
		Users.With(pg()).Where(Users.Age.Gt(18)).OrderBy(Users.Username.Asc()).Limit(5),
		Users.Username,
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "username" FROM "users" WHERE "age" > $1 ORDER BY "username" ASC LIMIT 5`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != 18 {
		t.Errorf("args = %v, want [18]", args)
	}
}

// Narrowing before a Where reads the same as narrowing after one, which is
// what taking a QuerySource buys.
func TestScalars_AcceptsQueryAndFiltered(t *testing.T) {
	fromQuery, _, err := orm.Select(Users.With(pg()), Users.Username).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	fromFiltered, _, err := orm.Select(Users.With(pg()).Where(), Users.Username).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if fromQuery != fromFiltered {
		t.Errorf("a Query and a Filtered compiled differently:\n  %s\n  %s",
			fromQuery, fromFiltered)
	}
}

func TestScalars_Distinct(t *testing.T) {
	sql, _, err := orm.Select(Users.With(pg()), Users.Username).Distinct().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT DISTINCT "username" FROM "users"`; sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

func TestScalars_First(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice"})
	db := orm.NewDB(c, postgres.Dialect{})

	name, err := orm.Select(Users.With(db), Users.Username).First(context.Background())
	if err != nil {
		t.Fatalf("First() error = %v", err)
	}
	if name != "alice" {
		t.Errorf("First() = %q, want alice", name)
	}
	if !strings.HasSuffix(c.QueryCalls()[0], "LIMIT 1") {
		t.Errorf("ran %s, want it narrowed to one row", c.QueryCalls()[0])
	}
}

func TestScalars_FirstOfNothing(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := orm.Select(Users.With(db), Users.Username).First(context.Background())
	if !errors.Is(err, orm.ErrNoRows) {
		t.Errorf("First() error = %v, want ErrNoRows", err)
	}
}

// First narrows a copy, so the value it was called on keeps its own limit.
func TestScalars_FirstLeavesTheQueryAlone(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice"})
	db := orm.NewDB(c, postgres.Dialect{})

	s := orm.Select(Users.With(db).Limit(50), Users.Username)
	if _, err := s.First(context.Background()); err != nil {
		t.Fatalf("First() error = %v", err)
	}
	sql, _, err := s.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, "LIMIT 50") {
		t.Errorf("First changed the query's limit: %s", sql)
	}
}

// Distinct returns a new value, so the one it was called on is unchanged.
func TestScalars_DistinctLeavesTheQueryAlone(t *testing.T) {
	base := orm.Select(Users.With(pg()), Users.Username)
	if _, _, err := base.Distinct().SQL(); err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	sql, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(sql, "DISTINCT") {
		t.Errorf("Distinct changed the value it was called on: %s", sql)
	}
}

func TestScalars_Count(t *testing.T) {
	tests := []struct {
		name  string
		build func(db *orm.DB) *orm.Scalars[string]
		want  string
	}{
		{
			name:  "counts the column, so a NULL does not count",
			build: func(db *orm.DB) *orm.Scalars[string] { return orm.Select(Users.With(db), Users.Username) },
			want:  `SELECT COUNT("username") FROM "users"`,
		},
		{
			name: "distinct counts each value once",
			build: func(db *orm.DB) *orm.Scalars[string] {
				return orm.Select(Users.With(db), Users.Username).Distinct()
			},
			want: `SELECT COUNT(DISTINCT "username") FROM "users"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fakedriver.NewConn()
			c.QueueRows([]any{int64(4)})
			db := orm.NewDB(c, postgres.Dialect{})

			n, err := tt.build(db).Count(context.Background())
			if err != nil {
				t.Fatalf("Count() error = %v", err)
			}
			if n != 4 {
				t.Errorf("Count() = %d, want 4", n)
			}
			if c.QueryCalls()[0] != tt.want {
				t.Errorf("ran  %s\nwant %s", c.QueryCalls()[0], tt.want)
			}
		})
	}
}

// A document column travels as encoded bytes, so reading one column of them
// decodes through the column's own codec exactly as a row scan does.
func TestScalars_DocumentColumn(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{[]byte(`{"theme":"dark"}`)}, []any{nil})
	db := orm.NewDB(c, postgres.Dialect{})

	prefs, err := orm.Select(Users.With(db), Users.Prefs).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(prefs) != 2 {
		t.Fatalf("All() returned %d values, want 2", len(prefs))
	}
	if prefs[0].Theme != "dark" {
		t.Errorf("first = %+v, want the decoded document", prefs[0])
	}
	if prefs[1].Theme != "" {
		t.Errorf("second = %+v, want the zero value for a NULL", prefs[1])
	}
}

func TestScalars_DecodeFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{[]byte("not json")})
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := orm.Select(Users.With(db), Users.Prefs).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want the decode failure")
	}
	if !strings.Contains(err.Error(), `column "prefs"`) {
		t.Errorf("error %q does not name the column", err)
	}
}

func TestScalars_Rejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	ctx := context.Background()

	t.Run("no column", func(t *testing.T) {
		_, err := orm.Select[string](Users.With(db), nil).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the missing column reported")
		}
		if !strings.Contains(err.Error(), "given no column") {
			t.Errorf("error %q does not name the problem", err)
		}
	})

	t.Run("another table's column", func(t *testing.T) {
		_, err := orm.Select(Users.With(db), Posts.Title).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the foreign column rejected")
		}
	})

	t.Run("no handle", func(t *testing.T) {
		_, err := orm.Select(Users.With(nil), Users.Username).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the missing handle reported")
		}
		if !strings.Contains(err.Error(), "no database handle") {
			t.Errorf("error %q does not name the problem", err)
		}
	})

	t.Run("a builder error surfaces", func(t *testing.T) {
		_, err := orm.Select(Users.With(db).Limit(-1), Users.Username).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the negative limit reported")
		}
		if !strings.Contains(err.Error(), "negative") {
			t.Errorf("error %q is not the builder's own", err)
		}
	})

	t.Run("query failure", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.FailOn(`SELECT "username" FROM "users"`)
		_, err := orm.Select(Users.With(orm.NewDB(c, postgres.Dialect{})), Users.Username).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the driver failure")
		}
	})

	t.Run("count of another table's column", func(t *testing.T) {
		_, err := orm.Select(Users.With(db), Posts.Title).Count(ctx)
		if err == nil {
			t.Fatal("Count() error = nil, want the foreign column rejected")
		}
	})

	t.Run("count with a foreign column in the filter", func(t *testing.T) {
		_, err := orm.Select(Users.With(db).Where(Posts.Title.Eq("x")), Users.Username).Count(ctx)
		if err == nil {
			t.Fatal("Count() error = nil, want the foreign column rejected")
		}
	})

	t.Run("a foreign column in the filter", func(t *testing.T) {
		_, _, err := orm.Select(Users.With(db).Where(Posts.Title.Eq("x")), Users.Username).SQL()
		if err == nil {
			t.Fatal("SQL() error = nil, want the foreign column rejected")
		}
	})

	t.Run("a foreign column in the ordering", func(t *testing.T) {
		_, _, err := orm.Select(Users.With(db).OrderBy(Posts.Title.Asc()), Users.Username).SQL()
		if err == nil {
			t.Fatal("SQL() error = nil, want the foreign column rejected")
		}
	})

	t.Run("no table", func(t *testing.T) {
		var zero orm.Table[User]
		_, err := orm.Select(zero.With(db), Users.Username).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the missing table reported")
		}
		if !strings.Contains(err.Error(), "no entity mapping") {
			t.Errorf("error %q does not name the problem", err)
		}
	})

	t.Run("Count carries a failure through", func(t *testing.T) {
		_, err := orm.Select(Users.With(nil), Users.Username).Count(ctx)
		if err == nil {
			t.Fatal("Count() error = nil, want the missing handle reported")
		}
	})

	t.Run("Select on a query with no table", func(t *testing.T) {
		var zero orm.Table[User]
		_, _, err := zero.With(db).Select().SQL()
		if err == nil {
			t.Fatal("SQL() error = nil, want the empty projection rejected")
		}
		if !strings.Contains(err.Error(), "was given no columns") {
			t.Errorf("error %q does not name the problem", err)
		}
	})

	t.Run("First carries a failure through", func(t *testing.T) {
		_, err := orm.Select(Users.With(db).Limit(-1), Users.Username).First(ctx)
		if err == nil {
			t.Fatal("First() error = nil, want the negative limit reported")
		}
	})

	t.Run("a result set that fails partway", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.RowsErr = errors.New("connection lost")
		_, err := orm.Select(Users.With(orm.NewDB(c, postgres.Dialect{})), Users.Username).All(ctx)
		if err == nil || !strings.Contains(err.Error(), "connection lost") {
			t.Errorf("All() error = %v, want the driver's own", err)
		}
	})

	t.Run("a value of the wrong type", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{42}) // an int where a string is wanted
		_, err := orm.Select(Users.With(orm.NewDB(c, postgres.Dialect{})), Users.Username).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the scan failure")
		}
		if !strings.Contains(err.Error(), `"username"`) {
			t.Errorf("error %q does not name the column", err)
		}
	})

	t.Run("a document that will not scan", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{"not bytes"})
		_, err := orm.Select(Users.With(orm.NewDB(c, postgres.Dialect{})), Users.Prefs).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the scan failure")
		}
	})
}

// A projection is validated wherever it is used, including inside the derived
// table a distinct count builds.
func TestCount_DistinctRejectsAForeignColumn(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := Users.With(db).Select(Posts.Title).Distinct().Count(context.Background())
	if err == nil {
		t.Fatal("Count() error = nil, want the foreign column rejected")
	}
	if !strings.Contains(err.Error(), `table "posts"`) {
		t.Errorf("error %q does not name the other table", err)
	}
}

// A query that already failed keeps its first error rather than replacing it
// with whatever went wrong next.
func TestScalars_KeepsTheFirstError(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := orm.Select[string](Users.With(db).Limit(-1), nil).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil")
	}
	if !strings.Contains(err.Error(), "negative") {
		t.Errorf("error %q is not the first one hit", err)
	}
}

// A model with no row type: it describes a table but can never hand back a
// *E, which is what makes it worth reading one column from.
var (
	Legacy     = orm.NewTable[orm.NoEntity]("legacy")
	LegacyName = orm.NewStringColumn("name")
)

// Reading one column scans into a T rather than into a row, so a model with no
// row type can still be read from this way.
func TestScalars_WorksWithoutAnEntityMapping(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"legacy"})
	db := orm.NewDB(c, postgres.Dialect{})

	names, err := orm.Select(Legacy.With(db), LegacyName).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(names) != 1 || names[0] != "legacy" {
		t.Errorf("All() = %v, want [legacy]", names)
	}
}

// Nothing about a projection may assume Postgres's spelling.
func TestScalars_AsksTheDialect(t *testing.T) {
	sql, _, err := orm.Select(Users.With(fake()), Users.Username).Distinct().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT DISTINCT [username] FROM [users]`; sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// Reading rows still reads every column when nothing narrowed them, which is
// what every existing caller depends on.
func TestSelect_DefaultsToEveryColumn(t *testing.T) {
	c := fakedriver.NewConn()
	at := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	c.QueueRows(row(1, "alice", nil, 30, nil, at))
	db := orm.NewDB(c, postgres.Dialect{})

	users, err := Users.With(db).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if users[0].Age != 30 || !users[0].CreatedAt.Equal(at) {
		t.Errorf("read %+v, want every column", users[0])
	}
}
