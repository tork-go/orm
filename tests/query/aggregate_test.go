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

func TestAggregate_Statements(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		run  func(db *orm.DB) error
		row  []any
		want string
	}{
		{
			name: "sum",
			run:  func(db *orm.DB) error { _, err := orm.Sum(ctx, Users.With(db), Users.Age); return err },
			row:  []any{100},
			want: `SELECT SUM("age") FROM "users"`,
		},
		{
			name: "avg",
			run:  func(db *orm.DB) error { _, err := orm.Avg(ctx, Users.With(db), Users.Age); return err },
			row:  []any{30.5},
			want: `SELECT AVG("age") FROM "users"`,
		},
		{
			name: "min",
			run:  func(db *orm.DB) error { _, err := orm.Min(ctx, Users.With(db), Users.Age); return err },
			row:  []any{1},
			want: `SELECT MIN("age") FROM "users"`,
		},
		{
			name: "max",
			run:  func(db *orm.DB) error { _, err := orm.Max(ctx, Users.With(db), Users.Age); return err },
			row:  []any{99},
			want: `SELECT MAX("age") FROM "users"`,
		},
		{
			name: "carries the filter",
			run: func(db *orm.DB) error {
				_, err := orm.Sum(ctx, Users.With(db).Where(Users.Age.Gt(18)), Users.Age)
				return err
			},
			row:  []any{100},
			want: `SELECT SUM("age") FROM "users" WHERE "age" > $1`,
		},
		{
			name: "distinct",
			run: func(db *orm.DB) error {
				_, err := orm.Sum(ctx, Users.With(db).Distinct(), Users.Age)
				return err
			},
			row:  []any{100},
			want: `SELECT SUM(DISTINCT "age") FROM "users"`,
		},
		{
			// Ordering and paging change which rows come back, not what the
			// set adds up to, so they are dropped as Count drops them.
			name: "ordering and paging are dropped",
			run: func(db *orm.DB) error {
				_, err := orm.Sum(ctx,
					Users.With(db).OrderBy(Users.ID.Desc()).Limit(5).Offset(2), Users.Age)
				return err
			},
			row:  []any{100},
			want: `SELECT SUM("age") FROM "users"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fakedriver.NewConn()
			c.QueueRows(tt.row)
			if err := tt.run(orm.NewDB(c, postgres.Dialect{})); err != nil {
				t.Fatalf("error = %v", err)
			}
			if got := c.QueryCalls()[0]; got != tt.want {
				t.Errorf("ran  %s\nwant %s", got, tt.want)
			}
		})
	}
}

// Each returns the column's own type, inferred from the column, so nothing
// has to be spelled out or converted at the call site.
func TestAggregate_Typed(t *testing.T) {
	ctx := context.Background()

	t.Run("sum of an int column is an int", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{240})
		db := orm.NewDB(c, postgres.Dialect{})

		total, err := orm.Sum(ctx, Users.With(db), Users.Age)
		if err != nil {
			t.Fatalf("Sum() error = %v", err)
		}
		var _ int = total
		if total != 240 {
			t.Errorf("Sum() = %d, want 240", total)
		}
	})

	t.Run("max of a time column is a time", func(t *testing.T) {
		at := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
		c := fakedriver.NewConn()
		c.QueueRows([]any{at})
		db := orm.NewDB(c, postgres.Dialect{})

		newest, err := orm.Max(ctx, Users.With(db), Users.CreatedAt)
		if err != nil {
			t.Fatalf("Max() error = %v", err)
		}
		var _ time.Time = newest
		if !newest.Equal(at) {
			t.Errorf("Max() = %v, want %v", newest, at)
		}
	})

	t.Run("avg is a float whatever the column holds", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{30.5})
		db := orm.NewDB(c, postgres.Dialect{})

		mean, err := orm.Avg(ctx, Users.With(db), Users.Age)
		if err != nil {
			t.Fatalf("Avg() error = %v", err)
		}
		var _ float64 = mean
		if mean != 30.5 {
			t.Errorf("Avg() = %v, want 30.5", mean)
		}
	})

	// A nullable column is a Column[*T], so its aggregate carries the same
	// pointer, and a NULL stays distinguishable from a value.
	t.Run("a nullable column keeps its pointer", func(t *testing.T) {
		c := fakedriver.NewConn()
		email := "z"
		c.QueueRows([]any{&email})
		db := orm.NewDB(c, postgres.Dialect{})

		largest, err := orm.Max(ctx, Users.With(db), Users.Email)
		if err != nil {
			t.Fatalf("Max() error = %v", err)
		}
		var _ *string = largest
		if largest == nil || *largest != "z" {
			t.Errorf("Max() = %v, want the value", largest)
		}
	})
}

// An aggregate over no rows answers NULL, and what that means differs by
// aggregate.
func TestAggregate_OverNoRows(t *testing.T) {
	ctx := context.Background()

	t.Run("a sum of nothing is zero", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{nil})
		db := orm.NewDB(c, postgres.Dialect{})

		total, err := orm.Sum(ctx, Users.With(db), Users.Age)
		if err != nil {
			t.Fatalf("Sum() error = %v", err)
		}
		if total != 0 {
			t.Errorf("Sum() = %d, want 0", total)
		}
	})

	t.Run("there is no smallest or largest of nothing", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			run  func(*orm.DB) error
		}{
			{"min", func(db *orm.DB) error { _, err := orm.Min(ctx, Users.With(db), Users.Age); return err }},
			{"max", func(db *orm.DB) error { _, err := orm.Max(ctx, Users.With(db), Users.Age); return err }},
			{"avg", func(db *orm.DB) error { _, err := orm.Avg(ctx, Users.With(db), Users.Age); return err }},
		} {
			t.Run(tt.name, func(t *testing.T) {
				c := fakedriver.NewConn()
				c.QueueRows([]any{nil})
				err := tt.run(orm.NewDB(c, postgres.Dialect{}))
				if !errors.Is(err, orm.ErrNoRows) {
					t.Errorf("%s error = %v, want ErrNoRows", tt.name, err)
				}
			})
		}
	})

	// A nullable column's NULL aggregate is a nil pointer, which is the same
	// statement in the type the column already uses. It is a value, not the
	// absence of one, so it is not ErrNoRows even for Min and Max.
	t.Run("a nullable column reports a nil rather than ErrNoRows", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			run  func(*orm.DB) (*string, error)
		}{
			{"sum", func(db *orm.DB) (*string, error) { return orm.Sum(ctx, Users.With(db), Users.Email) }},
			{"min", func(db *orm.DB) (*string, error) { return orm.Min(ctx, Users.With(db), Users.Email) }},
			{"max", func(db *orm.DB) (*string, error) { return orm.Max(ctx, Users.With(db), Users.Email) }},
		} {
			t.Run(tt.name, func(t *testing.T) {
				c := fakedriver.NewConn()
				c.QueueRows([]any{nil})
				got, err := tt.run(orm.NewDB(c, postgres.Dialect{}))
				if err != nil {
					t.Fatalf("%s error = %v, want nil: a nullable column can hold NULL",
						tt.name, err)
				}
				if got != nil {
					t.Errorf("%s = %v, want nil", tt.name, got)
				}
			})
		}
	})
}

// Nothing about an aggregate may assume Postgres's spelling.
func TestAggregate_AsksTheDialect(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1})
	db := orm.NewDB(c, fakedriver.NewDialect())

	if _, err := orm.Sum(context.Background(),
		Users.With(db).Where(Users.Age.Gt(1)), Users.Age); err != nil {
		t.Fatalf("Sum() error = %v", err)
	}
	if want := `SELECT SUM([age]) FROM [users] WHERE [age] > ?`; c.QueryCalls()[0] != want {
		t.Errorf("ran  %s\nwant %s", c.QueryCalls()[0], want)
	}
}

func TestAggregate_Rejected(t *testing.T) {
	ctx := context.Background()
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	tests := map[string]struct {
		run  func() error
		want string
	}{
		"no query": {
			run:  func() error { _, err := orm.Sum[int](ctx, nil, Users.Age); return err },
			want: "given no query",
		},
		"no column": {
			run:  func() error { _, err := orm.Sum[int](ctx, Users.With(db), nil); return err },
			want: "given no column",
		},
		"another table's column": {
			run:  func() error { _, err := orm.Sum(ctx, Users.With(db), Posts.ID); return err },
			want: `table "posts"`,
		},
		"another table's column in the filter": {
			run: func() error {
				_, err := orm.Sum(ctx, Users.With(db).Where(Posts.Title.Eq("x")), Users.Age)
				return err
			},
			want: `table "posts"`,
		},
		"no handle": {
			run:  func() error { _, err := orm.Sum(ctx, Users.With(nil), Users.Age); return err },
			want: "no database handle",
		},
		"a failure reaches Avg, which reads its own result": {
			run:  func() error { _, err := orm.Avg(ctx, Users.With(db), Posts.ID); return err },
			want: `table "posts"`,
		},
		"a builder error surfaces": {
			run: func() error {
				_, err := orm.Sum(ctx, Users.With(db).Limit(-1), Users.Age)
				return err
			},
			want: "negative",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.run()
			if err == nil {
				t.Fatal("no error, want the aggregate rejected")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}

	t.Run("the statement fails", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.FailOn(`SELECT SUM("age") FROM "users"`)
		_, err := orm.Sum(ctx, Users.With(orm.NewDB(c, postgres.Dialect{})), Users.Age)
		if err == nil {
			t.Fatal("Sum() error = nil, want the driver failure")
		}
	})

	t.Run("the aggregate returns no row at all", func(t *testing.T) {
		c := fakedriver.NewConn() // nothing queued, so no row comes back
		_, err := orm.Sum(ctx, Users.With(orm.NewDB(c, postgres.Dialect{})), Users.Age)
		if err == nil {
			t.Fatal("Sum() error = nil, want the missing row reported")
		}
		if !strings.Contains(err.Error(), "returned no row") {
			t.Errorf("error %q does not name the problem", err)
		}
	})

	t.Run("a result set that fails partway", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.RowsErr = errors.New("connection lost")
		_, err := orm.Sum(ctx, Users.With(orm.NewDB(c, postgres.Dialect{})), Users.Age)
		if err == nil || !strings.Contains(err.Error(), "connection lost") {
			t.Errorf("Sum() error = %v, want the driver's own", err)
		}
	})

	t.Run("a value of the wrong type", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{"not a number"})
		_, err := orm.Sum(ctx, Users.With(orm.NewDB(c, postgres.Dialect{})), Users.Age)
		if err == nil {
			t.Fatal("Sum() error = nil, want the scan failure")
		}
		if !strings.Contains(err.Error(), "scanning SUM") {
			t.Errorf("error %q does not say what failed", err)
		}
	})

	t.Run("a nullable column given a value of the wrong type", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{42})
		_, err := orm.Max(ctx, Users.With(orm.NewDB(c, postgres.Dialect{})), Users.Email)
		if err == nil {
			t.Fatal("Max() error = nil, want the scan failure")
		}
	})
}

// An aggregate scans a number rather than a row, so a model with no row type
// can still be aggregated.
func TestAggregate_WorksWithoutAnEntityMapping(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{7})
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := orm.Max(context.Background(), Legacy.With(db), LegacyCount)
	if err != nil {
		t.Fatalf("Max() error = %v", err)
	}
	if n != 7 {
		t.Errorf("Max() = %d, want 7", n)
	}
}
