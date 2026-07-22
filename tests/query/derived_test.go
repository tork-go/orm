package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// Ranked is the shape of the top-N-per-group report, declared as a model so
// its columns are typed handles like any other table's.
type Ranked struct {
	Username string
	Rank     int64
}

type RankedModel struct {
	orm.DerivedTable[Ranked]
	Username *orm.StringColumn
	Rank     *orm.BigIntColumn
}

var RankedT = orm.DefineDerived[Ranked]("ranked",
	func(t *orm.TableBuilder[Ranked]) *RankedModel {
		return &RankedModel{
			DerivedTable: t.Derived(),
			Username:     t.String("username"),
			Rank:         t.BigInt("rank"),
		}
	})

// rankedSource is the projection every test below wraps: each user with
// their position within their own age group.
func rankedSource(db *orm.DB) *orm.Projection[Ranked] {
	return orm.SelectAs[Ranked](
		Users.With(db), Users.Username,
		orm.RowNumber().PartitionBy(Users.Age).OrderBy(Users.ID.Asc()),
	)
}

// The shape this whole step exists for: a window function's result filtered
// by the query that reads it, which needs the projection wrapped as a table.
func TestDerived_TopNPerGroup(t *testing.T) {
	db := pg()
	sql, args, err := RankedT.From(rankedSource(db)).
		Where(RankedT.Rank.LessOrEqual(3)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "username", "rank" FROM (` +
		`SELECT "username" AS "username", ` +
		`ROW_NUMBER() OVER (PARTITION BY "age" ORDER BY "id" ASC) AS "rank" ` +
		`FROM "users") AS "ranked" WHERE "rank" <= $1`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != int64(3) {
		t.Errorf("args = %v, want [3]", args)
	}
}

// The FROM is textually before the WHERE, so the source's own placeholders
// have to number first whatever order the clauses were built in.
func TestDerived_PlaceholdersNumberFromFirst(t *testing.T) {
	db := pg()
	inner := orm.SelectAs[Ranked](
		Users.With(db).Where(Users.Username.Equals("alice")),
		Users.Username,
		orm.RowNumber().OrderBy(Users.ID.Asc()),
	)
	sql, args, err := RankedT.From(inner).Where(RankedT.Rank.LessOrEqual(3)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `"username" = $1`) {
		t.Errorf("SQL() = %s, want the source's placeholder numbered $1", sql)
	}
	if !strings.Contains(sql, `"rank" <= $2`) {
		t.Errorf("SQL() = %s, want the outer condition's placeholder numbered $2", sql)
	}
	if len(args) != 2 || args[0] != "alice" || args[1] != int64(3) {
		t.Errorf("args = %v, want [alice 3]", args)
	}
}

// The other shape it unlocks: an aggregate over an aggregate, which needs
// the grouped result to be a table before it can be grouped again.
func TestDerived_AggregateOfAnAggregate(t *testing.T) {
	db := pg()
	type PerUser struct {
		Username string
		Total    int
	}
	type perUserModel struct {
		orm.DerivedTable[PerUser]
		Username *orm.StringColumn
		Total    *orm.IntColumn
	}
	perUser := orm.DefineDerived[PerUser]("per_user",
		func(t *orm.TableBuilder[PerUser]) *perUserModel {
			return &perUserModel{
				DerivedTable: t.Derived(),
				Username:     t.String("username"),
				Total:        t.Int("total"),
			}
		})

	inner := orm.SelectAs[PerUser](Users.With(db), Users.Username, orm.SumOf(Users.Age)).
		GroupBy(Users.Username)

	sql, _, err := orm.Select(perUser.From(inner), perUser.Total).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "total" FROM (SELECT "username" AS "username", SUM("age") AS "total" ` +
		`FROM "users" GROUP BY "username") AS "per_user"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

func TestDerived_ShapeMismatch(t *testing.T) {
	db := pg()
	tests := map[string]struct {
		src  orm.DerivedSource
		want string
	}{
		"too few columns": {
			orm.SelectAs[struct{ Username string }](Users.With(db), Users.Username),
			"declares 2 column(s) but the source yields 1",
		},
		"wrong type": {
			orm.SelectAs[struct {
				Username string
				Rank     string
			}](Users.With(db), Users.Username, Users.Username),
			`column 1, "rank", is int64 but the source's expression 1 is string`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := RankedT.From(tt.src).SQL()
			if err == nil {
				t.Fatal("SQL() error = nil, want the shape mismatch rejected")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestDerived_NoSource(t *testing.T) {
	_, _, err := RankedT.From(nil).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the missing source rejected")
	}
	if !strings.Contains(err.Error(), "no source") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// A zero-valued DerivedTable has no table behind it, and says so rather
// than dereferencing nothing.
func TestDerived_ZeroValued(t *testing.T) {
	var zero orm.DerivedTable[Ranked]
	_, _, err := zero.From(rankedSource(pg())).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the zero-valued table rejected")
	}
	if !strings.Contains(err.Error(), "DefineDerived") {
		t.Errorf("error %q does not say how to fix it", err)
	}
}

// Nothing stops a model declared with DefineDerived from embedding Table
// rather than DerivedTable, since both come from the same builder. That
// model has With, which cannot work: there is no source. It says so rather
// than compiling to a reference to a table that does not exist.
func TestDerived_WithInsteadOfFrom(t *testing.T) {
	type mistaken struct{ Username string }
	type mistakenModel struct {
		orm.Table[mistaken] // the mistake: should be DerivedTable
		Username            *orm.StringColumn
	}
	m := orm.DefineDerived[mistaken]("mistaken",
		func(t *orm.TableBuilder[mistaken]) *mistakenModel {
			return &mistakenModel{Table: t.Table(), Username: t.String("username")}
		})

	_, _, err := m.With(pg()).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the missing source rejected")
	}
	if !strings.Contains(err.Error(), "queried with From, not With") {
		t.Errorf("error %q does not say how to fix it", err)
	}
}

// Everything a read does over a stored table works over a derived one:
// ordering, paging, counting and reading one column.
func TestDerived_OrdinaryReads(t *testing.T) {
	db := pg()
	base := RankedT.From(rankedSource(db))

	t.Run("order and page", func(t *testing.T) {
		sql, _, err := base.OrderBy(RankedT.Rank.Desc()).Limit(5).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if !strings.HasSuffix(sql, `ORDER BY "rank" DESC LIMIT 5`) {
			t.Errorf("SQL() = %s", sql)
		}
	})

	t.Run("count", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{int64(7)})
		n, err := RankedT.From(rankedSource(orm.NewDB(c, postgres.Dialect{}))).
			Count(context.Background())
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if n != 7 {
			t.Errorf("Count() = %d, want 7", n)
		}
		if got := c.QueryCalls()[0]; !strings.HasPrefix(got, `SELECT COUNT(*) FROM (SELECT`) {
			t.Errorf("Count ran %s", got)
		}
	})

	t.Run("one column", func(t *testing.T) {
		sql, _, err := orm.Select(base, RankedT.Username).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if !strings.HasPrefix(sql, `SELECT "username" FROM (SELECT`) {
			t.Errorf("SQL() = %s", sql)
		}
	})

	t.Run("grouped", func(t *testing.T) {
		sql, _, err := orm.CountBy(base, RankedT.Username).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if !strings.Contains(sql, `FROM (SELECT`) || !strings.HasSuffix(sql, `GROUP BY "username"`) {
			t.Errorf("SQL() = %s", sql)
		}
	})
}

// It runs, not merely compiles, and the rows land in their fields.
func TestDerived_All(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice", int64(1)}, []any{"bob", int64(2)})
	db := orm.NewDB(c, postgres.Dialect{})

	rows, err := RankedT.From(rankedSource(db)).
		Where(RankedT.Rank.LessOrEqual(3)).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(rows) != 2 || rows[0].Username != "alice" || rows[1].Rank != 2 {
		t.Errorf("rows = %+v, want [{alice 1} {bob 2}]", rows)
	}
}

// A derived table has no relationships and no stored rows, so the
// operations that need either say so by name.
func TestDerived_Rejects(t *testing.T) {
	db := pg()
	base := func() *orm.Filtered[Ranked] { return RankedT.From(rankedSource(db)) }

	t.Run("Load", func(t *testing.T) {
		_, _, err := base().Load(Authors.Books).SQL()
		if err == nil || !strings.Contains(err.Error(), "Load cannot run over a derived table") {
			t.Errorf("error = %v, want Load rejected by name", err)
		}
	})
	t.Run("Join", func(t *testing.T) {
		_, _, err := base().Join(Authors.Books).SQL()
		if err == nil || !strings.Contains(err.Error(), "Join cannot run over a derived table") {
			t.Errorf("error = %v, want Join rejected by name", err)
		}
	})
	t.Run("UpdateAll", func(t *testing.T) {
		_, err := base().UpdateAll(context.Background(), RankedT.Username.Set("x"))
		if err == nil || !strings.Contains(err.Error(), "cannot run over a derived table") {
			t.Errorf("error = %v, want UpdateAll rejected", err)
		}
	})
	t.Run("DeleteAll", func(t *testing.T) {
		_, err := base().DeleteAll(context.Background())
		if err == nil || !strings.Contains(err.Error(), "cannot run over a derived table") {
			t.Errorf("error = %v, want DeleteAll rejected", err)
		}
	})
}

// A projection reading from a derived table carries the same requirement:
// the derived table needs a source, however it is being read.
func TestDerived_ProjectionOverASourcelessDerivedTable(t *testing.T) {
	type mistaken struct{ Username string }
	type mistakenModel struct {
		orm.Table[mistaken] // the mistake again, read through SelectAs
		Username            *orm.StringColumn
	}
	m := orm.DefineDerived[mistaken]("mistaken_projection",
		func(t *orm.TableBuilder[mistaken]) *mistakenModel {
			return &mistakenModel{Table: t.Table(), Username: t.String("username")}
		})

	type row struct{ Username string }
	_, _, err := orm.SelectAs[row](m.With(pg()), m.Username).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the missing source rejected")
	}
	if !strings.Contains(err.Error(), "queried with From, not With") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// A source rejected before it renders at all — here by carrying a lock,
// which SelectAs refuses — reports that rather than the derived table's own
// complaint.
func TestDerived_SourceCheckErrorSurfaces(t *testing.T) {
	db := pg()
	locked := orm.SelectAs[Ranked](
		Users.With(db).Where(Users.ID.GreaterThan(0)).ForUpdate(),
		Users.Username,
		orm.RowNumber().OrderBy(Users.ID.Asc()),
	)
	_, _, err := RankedT.From(locked).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the lock rejected")
	}
	if !strings.Contains(err.Error(), "ForUpdate") {
		t.Errorf("error %q is not the source's own", err)
	}
}

// A source that cannot itself compile reports its own error rather than one
// about the derived table.
func TestDerived_SourceErrorSurfaces(t *testing.T) {
	db := pg()
	bad := orm.SelectAs[Ranked](
		Users.With(db).Where(Books.Title.Equals("x")), // a column of another table
		Users.Username,
		orm.RowNumber(),
	)
	_, _, err := RankedT.From(bad).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the source's own error")
	}
	if !strings.Contains(err.Error(), `belongs to table "books"`) {
		t.Errorf("error %q is not the source's own", err)
	}
}
