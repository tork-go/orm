package query_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

func TestGroup_Statements(t *testing.T) {
	tests := []struct {
		name string
		sql  func() (string, []any, error)
		want string
		args []any
	}{
		{
			name: "count",
			sql:  func() (string, []any, error) { return orm.CountBy(Users.With(pg()), Users.Username).SQL() },
			want: `SELECT "username", COUNT(*) FROM "users" GROUP BY "username"`,
		},
		{
			name: "sum",
			sql: func() (string, []any, error) {
				return orm.SumBy(Users.With(pg()), Users.Username, Users.Age).SQL()
			},
			want: `SELECT "username", SUM("age") FROM "users" GROUP BY "username"`,
		},
		{
			name: "avg",
			sql: func() (string, []any, error) {
				return orm.AvgBy(Users.With(pg()), Users.Username, Users.Age).SQL()
			},
			want: `SELECT "username", AVG("age") FROM "users" GROUP BY "username"`,
		},
		{
			name: "min and max",
			sql: func() (string, []any, error) {
				return orm.MinBy(Users.With(pg()), Users.Username, Users.Age).SQL()
			},
			want: `SELECT "username", MIN("age") FROM "users" GROUP BY "username"`,
		},
		{
			name: "max",
			sql: func() (string, []any, error) {
				return orm.MaxBy(Users.With(pg()), Users.Username, Users.CreatedAt).SQL()
			},
			want: `SELECT "username", MAX("created_at") FROM "users" GROUP BY "username"`,
		},
		{
			name: "carries the filter",
			sql: func() (string, []any, error) {
				return orm.CountBy(Users.With(pg()).Where(Users.Age.GreaterThan(18)), Users.Username).SQL()
			},
			want: `SELECT "username", COUNT(*) FROM "users" WHERE "age" > $1 GROUP BY "username"`,
			args: []any{18},
		},
		{
			name: "having",
			sql: func() (string, []any, error) {
				return orm.CountBy(Users.With(pg()), Users.Username).Having(orm.OpGreaterOrEqual, 5).SQL()
			},
			want: `SELECT "username", COUNT(*) FROM "users" GROUP BY "username" ` +
				`HAVING COUNT(*) >= $1`,
			args: []any{int64(5)},
		},
		{
			name: "having accumulates",
			sql: func() (string, []any, error) {
				return orm.CountBy(Users.With(pg()), Users.Username).
					Having(orm.OpGreaterOrEqual, 5).Having(orm.OpLessThan, 100).SQL()
			},
			want: `SELECT "username", COUNT(*) FROM "users" GROUP BY "username" ` +
				`HAVING COUNT(*) >= $1 AND COUNT(*) < $2`,
			args: []any{int64(5), int64(100)},
		},
		{
			name: "ordered by the aggregate, largest first, capped",
			sql: func() (string, []any, error) {
				return orm.CountBy(Users.With(pg()), Users.Username).
					OrderByValueDesc().Limit(10).SQL()
			},
			want: `SELECT "username", COUNT(*) FROM "users" GROUP BY "username" ` +
				`ORDER BY COUNT(*) DESC LIMIT 10`,
		},
		{
			name: "ordered by the aggregate, smallest first",
			sql: func() (string, []any, error) {
				return orm.SumBy(Users.With(pg()), Users.Username, Users.Age).OrderByValue().SQL()
			},
			want: `SELECT "username", SUM("age") FROM "users" GROUP BY "username" ` +
				`ORDER BY SUM("age") ASC`,
		},
		{
			name: "ordered by the key",
			sql: func() (string, []any, error) {
				return orm.CountBy(Users.With(pg()), Users.Username).
					OrderBy(Users.Username.Asc()).SQL()
			},
			want: `SELECT "username", COUNT(*) FROM "users" GROUP BY "username" ` +
				`ORDER BY "username" ASC`,
		},
		{
			name: "ordered by both, key first",
			sql: func() (string, []any, error) {
				return orm.CountBy(Users.With(pg()), Users.Username).
					OrderBy(Users.Username.Desc()).OrderByValueDesc().SQL()
			},
			want: `SELECT "username", COUNT(*) FROM "users" GROUP BY "username" ` +
				`ORDER BY "username" DESC, COUNT(*) DESC`,
		},
		{
			name: "distinct reaches the aggregate",
			sql: func() (string, []any, error) {
				return orm.SumBy(Users.With(pg()).Distinct(), Users.Username, Users.Age).SQL()
			},
			want: `SELECT "username", SUM(DISTINCT "age") FROM "users" GROUP BY "username"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.sql()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if sql != tt.want {
				t.Errorf("SQL()  = %s\nwant   = %s", sql, tt.want)
			}
			if len(args) != len(tt.args) {
				t.Fatalf("args = %v, want %v", args, tt.args)
			}
			for i := range args {
				if args[i] != tt.args[i] {
					t.Errorf("args[%d] = %v (%T), want %v (%T)",
						i, args[i], args[i], tt.args[i], tt.args[i])
				}
			}
		})
	}
}

// The filter's arguments bind before the aggregate's, matching where each
// appears in the statement.
func TestGroup_ArgumentOrder(t *testing.T) {
	_, args, err := orm.CountBy(Users.With(pg()).Where(Users.Age.GreaterThan(18)), Users.Username).
		Having(orm.OpGreaterOrEqual, 5).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if len(args) != 2 || args[0] != 18 || args[1] != int64(5) {
		t.Errorf("args = %v, want [18 5] in that order", args)
	}
}

func TestGroup_Results(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice", int64(3)}, []any{"bob", int64(1)})
	db := orm.NewDB(c, postgres.Dialect{})

	got, err := orm.CountBy(Users.With(db), Users.Username).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	var _ []orm.Bucket[string, int64] = got
	if len(got) != 2 {
		t.Fatalf("All() returned %d buckets, want 2", len(got))
	}
	if got[0].Key != "alice" || got[0].Value != 3 {
		t.Errorf("first = %+v, want {alice 3}", got[0])
	}
	if got[1].Key != "bob" || got[1].Value != 1 {
		t.Errorf("second = %+v, want {bob 1}", got[1])
	}
}

// The value's type comes from the column being aggregated, so nothing has to
// be converted at the call site.
func TestGroup_TypedValues(t *testing.T) {
	ctx := context.Background()

	t.Run("sum of an int column", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{"alice", 90})
		got, err := orm.SumBy(Users.With(orm.NewDB(c, postgres.Dialect{})),
			Users.Username, Users.Age).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		var _ []orm.Bucket[string, int] = got
		if got[0].Value != 90 {
			t.Errorf("Value = %d, want 90", got[0].Value)
		}
	})

	t.Run("avg is a float", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{"alice", 30.5})
		got, err := orm.AvgBy(Users.With(orm.NewDB(c, postgres.Dialect{})),
			Users.Username, Users.Age).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		var _ []orm.Bucket[string, float64] = got
		if got[0].Value != 30.5 {
			t.Errorf("Value = %v, want 30.5", got[0].Value)
		}
	})
}

// A nullable key groups its NULLs together, and K is a pointer so it can hold
// one; a group whose values are all NULL reports the same way an aggregate
// does.
func TestGroup_Nulls(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{nil, int64(2)}, []any{"a@example.com", int64(1)})
	db := orm.NewDB(c, postgres.Dialect{})

	got, err := orm.CountBy(Users.With(db), Users.Email).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	var _ []orm.Bucket[*string, int64] = got
	if got[0].Key != nil {
		t.Errorf("first key = %v, want nil for the NULL group", got[0].Key)
	}
	if got[0].Value != 2 {
		t.Errorf("first value = %d, want 2", got[0].Value)
	}
	if got[1].Key == nil || *got[1].Key != "a@example.com" {
		t.Errorf("second key = %v, want the address", got[1].Key)
	}
}

func TestGroup_NullAggregate(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice", nil})
	db := orm.NewDB(c, postgres.Dialect{})

	got, err := orm.SumBy(Users.With(db), Users.Username, Users.Age).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if got[0].Value != 0 {
		t.Errorf("Value = %d, want 0 where the aggregate was NULL", got[0].Value)
	}
}

func TestGroup_Keys(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice", int64(9)}, []any{"bob", int64(9)})
	db := orm.NewDB(c, postgres.Dialect{})

	keys, err := orm.CountBy(Users.With(db), Users.Username).
		Having(orm.OpGreaterOrEqual, 5).Keys(context.Background())
	if err != nil {
		t.Fatalf("Keys() error = %v", err)
	}
	var _ []string = keys
	if len(keys) != 2 || keys[0] != "alice" || keys[1] != "bob" {
		t.Errorf("Keys() = %v, want [alice bob]", keys)
	}
}

// Every builder returns a new value, so a grouped query is as safe to hold and
// branch from as a plain one.
func TestGroup_BuildersLeaveTheOriginalAlone(t *testing.T) {
	base := orm.CountBy(Users.With(pg()), Users.Username)
	want, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}

	branches := map[string]func() (string, []any, error){
		"Having":           func() (string, []any, error) { return base.Having(orm.OpGreaterThan, 1).SQL() },
		"OrderBy":          func() (string, []any, error) { return base.OrderBy(Users.Username.Asc()).SQL() },
		"OrderByValue":     func() (string, []any, error) { return base.OrderByValue().SQL() },
		"OrderByValueDesc": func() (string, []any, error) { return base.OrderByValueDesc().SQL() },
		"Limit":            func() (string, []any, error) { return base.Limit(5).SQL() },
	}
	for name, branch := range branches {
		t.Run(name, func(t *testing.T) {
			narrowed, _, err := branch()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if narrowed == want {
				t.Errorf("%s changed nothing", name)
			}
			got, _, err := base.SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if got != want {
				t.Errorf("%s changed the query it was called on:\n got %s\nwant %s",
					name, got, want)
			}
		})
	}
}

// Nothing about grouping may assume Postgres's spelling.
func TestGroup_AsksTheDialect(t *testing.T) {
	sql, _, err := orm.SumBy(Users.With(fake()), Users.Username, Users.Age).
		OrderByValueDesc().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT [username], SUM([age]) FROM [users] GROUP BY [username] ` +
		`ORDER BY SUM([age]) DESC`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

func TestGroup_Rejected(t *testing.T) {
	ctx := context.Background()
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	tests := map[string]struct {
		sql  func() (string, []any, error)
		want string
	}{
		"no query": {
			sql:  func() (string, []any, error) { return orm.CountBy[string](nil, Users.Username).SQL() },
			want: "given no query",
		},
		"no key": {
			sql:  func() (string, []any, error) { return orm.CountBy[string](Users.With(db), nil).SQL() },
			want: "needs a key column",
		},
		"no column to aggregate": {
			sql: func() (string, []any, error) {
				return orm.SumBy[string, int](Users.With(db), Users.Username, nil).SQL()
			},
			want: "needs a column to aggregate",
		},
		"another table's key": {
			sql:  func() (string, []any, error) { return orm.CountBy(Users.With(db), Posts.Title).SQL() },
			want: `table "posts"`,
		},
		"another table's aggregate column": {
			sql: func() (string, []any, error) {
				return orm.SumBy(Users.With(db), Users.Username, Posts.ID).SQL()
			},
			want: `table "posts"`,
		},
		"another table's column in the filter": {
			sql: func() (string, []any, error) {
				return orm.CountBy(Users.With(db).Where(Posts.Title.Equals("x")), Users.Username).SQL()
			},
			want: `table "posts"`,
		},
		"ordered by something other than the key": {
			sql: func() (string, []any, error) {
				return orm.CountBy(Users.With(db), Users.Username).OrderBy(Users.Age.Asc()).SQL()
			},
			want: "can only be ordered by its key",
		},
		"ordered by nothing at all": {
			sql: func() (string, []any, error) {
				return orm.CountBy(Users.With(db), Users.Username).OrderBy(orm.Ordering{}).SQL()
			},
			want: "can only be ordered by its key",
		},
		// Another table's column can share the key's name, so passing the name
		// check is not enough on its own.
		"ordered by another table's identically named column": {
			sql: func() (string, []any, error) {
				return orm.CountBy(Users.With(db), Users.ID).OrderBy(Posts.ID.Asc()).SQL()
			},
			want: `table "posts"`,
		},
		"a negative limit": {
			sql: func() (string, []any, error) {
				return orm.CountBy(Users.With(db), Users.Username).Limit(-1).SQL()
			},
			want: "negative",
		},
		"no handle": {
			sql:  func() (string, []any, error) { return orm.CountBy(Users.With(nil), Users.Username).SQL() },
			want: "no database handle",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := tt.sql()
			if err == nil {
				t.Fatal("SQL() error = nil, want the query rejected")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}

	t.Run("the statement fails", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.FailOn(`SELECT "username", COUNT(*) FROM "users" GROUP BY "username"`)
		_, err := orm.CountBy(Users.With(orm.NewDB(c, postgres.Dialect{})), Users.Username).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the driver failure")
		}
	})

	t.Run("a result set that fails partway", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.RowsErr = errors.New("connection lost")
		_, err := orm.CountBy(Users.With(orm.NewDB(c, postgres.Dialect{})), Users.Username).All(ctx)
		if err == nil || !strings.Contains(err.Error(), "connection lost") {
			t.Errorf("All() error = %v, want the driver's own", err)
		}
	})

	t.Run("a row that will not scan", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{42, int64(1)}) // an int key where a string is wanted
		_, err := orm.CountBy(Users.With(orm.NewDB(c, postgres.Dialect{})), Users.Username).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the scan failure")
		}
		if !strings.Contains(err.Error(), "scanning group") {
			t.Errorf("error %q does not say what failed", err)
		}
	})

	t.Run("Keys carries a failure through", func(t *testing.T) {
		_, err := orm.CountBy(Users.With(db), Posts.Title).Keys(ctx)
		if err == nil {
			t.Fatal("Keys() error = nil, want the foreign key column rejected")
		}
	})
}

// Grouping scans a key and a number rather than a row, so a model with no row
// type can still be grouped.
func TestGroup_WorksWithoutAnEntityMapping(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"legacy", int64(2)})
	db := orm.NewDB(c, postgres.Dialect{})

	got, err := orm.CountBy(Legacy.With(db), LegacyName).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(got) != 1 || got[0].Key != "legacy" || got[0].Value != 2 {
		t.Errorf("All() = %+v, want one bucket", got)
	}
}
