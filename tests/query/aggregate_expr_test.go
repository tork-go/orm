package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// Each aggregate has an expression-taking twin, rendering the same function
// around a computed value rather than a stored column.
func TestAggregateOfExpr_EveryFunction(t *testing.T) {
	// Each case reads into the type its own aggregate yields: COUNT is
	// always int64, AVG always float64, and the rest keep the expression's.
	type (
		asInt64   struct{ N int64 }
		asFloat64 struct{ N float64 }
		asInt     struct{ N int }
	)
	doubled := Users.Age.Times(2)

	tests := map[string]struct {
		sql  func() (string, []any, error)
		want string
	}{
		"CountOfExpr": {
			func() (string, []any, error) {
				return orm.SelectAs[asInt64](Users.With(pg()), orm.CountOfExpr(doubled)).SQL()
			}, `COUNT(("age" * $1))`,
		},
		"SumOfExpr": {
			func() (string, []any, error) {
				return orm.SelectAs[asInt](Users.With(pg()), orm.SumOfExpr(doubled)).SQL()
			}, `SUM(("age" * $1))`,
		},
		"AvgOfExpr": {
			func() (string, []any, error) {
				return orm.SelectAs[asFloat64](Users.With(pg()), orm.AvgOfExpr(doubled)).SQL()
			}, `AVG(("age" * $1))`,
		},
		"MinOfExpr": {
			func() (string, []any, error) {
				return orm.SelectAs[asInt](Users.With(pg()), orm.MinOfExpr(doubled)).SQL()
			}, `MIN(("age" * $1))`,
		},
		"MaxOfExpr": {
			func() (string, []any, error) {
				return orm.SelectAs[asInt](Users.With(pg()), orm.MaxOfExpr(doubled)).SQL()
			}, `MAX(("age" * $1))`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sql, _, err := tt.sql()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if want := "SELECT " + tt.want + ` FROM "users"`; sql != want {
				t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
			}
		})
	}
}

// The shape this step exists for: SQL has no COUNT with a condition, so a
// conditional tally is a SUM over a CASE.
func TestAggregateOfExpr_ConditionalTally(t *testing.T) {
	type report struct {
		Username string
		Adults   int
	}
	sql, args, err := orm.SelectAs[report](
		Users.With(pg()),
		Users.Username,
		orm.SumOfExpr(orm.Case[int]().When(Users.Age.GreaterOrEqual(18), 1).Else(0)),
	).GroupBy(Users.Username).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "username", SUM(CASE WHEN "age" >= $1 THEN CAST($2 AS INTEGER) ELSE CAST($3 AS INTEGER) END) ` +
		`FROM "users" GROUP BY "username"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 3 || args[0] != 18 || args[1] != 1 || args[2] != 0 {
		t.Errorf("args = %v, want [18 1 0]", args)
	}
}

// It runs, not merely compiles.
func TestAggregateOfExpr_All(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice", 2})
	db := orm.NewDB(c, postgres.Dialect{})

	type report struct {
		Username string
		Adults   int
	}
	rows, err := orm.SelectAs[report](
		Users.With(db), Users.Username,
		orm.SumOfExpr(orm.Case[int]().When(Users.Age.GreaterOrEqual(18), 1).Else(0)),
	).GroupBy(Users.Username).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(rows) != 1 || rows[0].Adults != 2 {
		t.Errorf("rows = %+v, want [{alice 2}]", rows)
	}
}

// An aggregate over a bare lifted column is the same thing the column-taking
// form does, which is worth pinning: the two paths must agree.
func TestAggregateOfExpr_OverALiftedColumn(t *testing.T) {
	type row struct{ N int }
	byExpr, _, err := orm.SelectAs[row](Users.With(pg()), orm.SumOfExpr(Users.Age.Value())).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	byCol, _, err := orm.SelectAs[row](Users.With(pg()), orm.SumOf(Users.Age)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if byExpr != byCol {
		t.Errorf("SumOfExpr(col.Value()) = %s\nSumOf(col)            = %s", byExpr, byCol)
	}
}

// A column the statement does not select from is rejected from inside the
// expression, the same way it is anywhere else.
func TestAggregateOfExpr_ForeignColumnRejected(t *testing.T) {
	type row struct{ N int }
	_, _, err := orm.SelectAs[row](Users.With(pg()), orm.SumOfExpr(Posts.ID.Times(2))).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign column rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "posts"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

// Having takes one, since an aggregate over an expression compares like any
// other expression.
func TestAggregateOfExpr_InHaving(t *testing.T) {
	type report struct{ Username string }
	sql, _, err := orm.SelectAs[report](Users.With(pg()), Users.Username).
		GroupBy(Users.Username).
		Having(orm.SumOfExpr(orm.Case[int]().When(Users.Age.GreaterOrEqual(18), 1).Else(0)).
			GreaterThan(0)).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `HAVING SUM(CASE WHEN "age" >= $1 THEN CAST($2 AS INTEGER) ELSE CAST($3 AS INTEGER) END) > $4`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// A projection field is checked against the aggregate's own type: COUNT is
// always int64 whatever it counts, and SUM keeps the expression's type.
func TestAggregateOfExpr_ProjectionTypeChecked(t *testing.T) {
	type wrongCount struct{ N int } // COUNT is int64
	if _, _, err := orm.SelectAs[wrongCount](
		Users.With(pg()), orm.CountOfExpr(Users.Age.Times(2)),
	).SQL(); err == nil {
		t.Error("SQL() error = nil, want COUNT's int64 to reject an int field")
	}

	type wrongSum struct{ N string } // SUM over an int expression is int
	if _, _, err := orm.SelectAs[wrongSum](
		Users.With(pg()), orm.SumOfExpr(Users.Age.Times(2)),
	).SQL(); err == nil {
		t.Error("SQL() error = nil, want SUM's int to reject a string field")
	}
}
