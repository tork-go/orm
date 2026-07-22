package query_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tork-go/orm"
)

// An aggregate is an ordinary expression, so arithmetic over one needs no
// vocabulary of its own.
func TestAggregate_InArithmetic(t *testing.T) {
	type row struct{ Mean int }
	sql, _, err := orm.SelectAs[row](Users.With(pg()),
		orm.SumOf(Users.Age).DividedBy(orm.CountAll())).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if sql != `SELECT (SUM("age") / COUNT(*)) FROM "users"` {
		t.Errorf("SQL() = %s", sql)
	}
}

// A call may wrap an aggregate, since both are the same kind of node.
func TestAggregate_InsideACall(t *testing.T) {
	type row struct{ Mean float64 }
	sql, _, err := orm.SelectAs[row](Users.With(pg()),
		orm.Round(orm.AvgOf(Users.Age), 2)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if sql != `SELECT ROUND(AVG("age"), CAST($1 AS INTEGER)) FROM "users"` {
		t.Errorf("SQL() = %s", sql)
	}
}

// COUNT(DISTINCT x), the honest answer to counting rows a join multiplied.
func TestAggregate_CountDistinct(t *testing.T) {
	type row struct{ N int64 }
	sql, _, err := orm.SelectAs[row](Users.With(pg()), orm.CountOf(Users.Age).Distinct()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if sql != `SELECT COUNT(DISTINCT "age") FROM "users"` {
		t.Errorf("SQL() = %s", sql)
	}
}

// Every aggregate takes DISTINCT, not only COUNT.
func TestAggregate_SumDistinct(t *testing.T) {
	type row struct{ N int }
	sql, _, err := orm.SelectAs[row](Users.With(pg()), orm.SumOf(Users.Age).Distinct()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if sql != `SELECT SUM(DISTINCT "age") FROM "users"` {
		t.Errorf("SQL() = %s", sql)
	}
}

// Ordering by an aggregate is what "the busiest first" means.
func TestAggregate_OrderBy(t *testing.T) {
	type row struct {
		Name string
		N    int64
	}
	sql, _, err := orm.SelectAs[row](Users.With(pg()), Users.Username, orm.CountAll()).
		GroupBy(Users.Username).
		OrderBy(orm.CountAll().Desc()).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `ORDER BY COUNT(*) DESC`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// Comparing one aggregate against another, which the old Having — a single
// aggregate against a literal — could not express at all.
func TestHaving_AggregateAgainstAggregate(t *testing.T) {
	type row struct{ Name string }
	sql, args, err := orm.SelectAs[row](Users.With(pg()), Users.Username).
		GroupBy(Users.Username).
		Having(orm.SumOf(Users.Age).GreaterThan(orm.MaxOf(Users.Age))).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `HAVING SUM("age") > MAX("age")`) {
		t.Errorf("SQL() = %s", sql)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none: both sides are aggregates, not values", args)
	}
}

// Or nests inside a HAVING exactly as it does inside a WHERE.
func TestHaving_Or(t *testing.T) {
	type row struct{ Name string }
	sql, _, err := orm.SelectAs[row](Users.With(pg()), Users.Username).
		GroupBy(Users.Username).
		Having(orm.Or(
			orm.CountAll().GreaterThan(int64(10)),
			orm.AvgOf(Users.Age).LessThan(5.0),
		)).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `HAVING (COUNT(*) > $1 OR AVG("age") < $2)`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// Conditions accumulate across calls and are joined with AND, as Where's do.
func TestHaving_Accumulates(t *testing.T) {
	type row struct{ Name string }
	sql, _, err := orm.SelectAs[row](Users.With(pg()), Users.Username).
		GroupBy(Users.Username).
		Having(orm.CountAll().GreaterThan(int64(1))).
		Having(orm.CountAll().LessThan(int64(9))).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `HAVING (COUNT(*) > $1 AND COUNT(*) < $2)`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// A HAVING may name a plain column, which SQL allows for a grouped one.
func TestHaving_OverAGroupedColumn(t *testing.T) {
	type row struct{ Name string }
	sql, _, err := orm.SelectAs[row](Users.With(pg()), Users.Username).
		GroupBy(Users.Username).
		Having(Users.Username.NotEquals("root")).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `HAVING "username" <> $1`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// The report the phase is for: group by a value the database computes.
func TestGroupBy_OnACall(t *testing.T) {
	type monthly struct {
		Month time.Time
		Total int
	}
	month := orm.Fn[time.Time]("date_trunc", "month", Users.CreatedAt)
	sql, args, err := orm.SelectAs[monthly](Users.With(pg()), month, orm.SumOf(Users.Age)).
		GroupBy(month).
		Having(orm.SumOf(Users.Age).GreaterThan(100)).
		OrderBy(month.Asc()).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT date_trunc(CAST($1 AS TEXT), "created_at"), SUM("age") FROM "users" ` +
		`GROUP BY date_trunc(CAST($2 AS TEXT), "created_at") ` +
		`HAVING SUM("age") > $3 ` +
		`ORDER BY date_trunc(CAST($4 AS TEXT), "created_at") ASC`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	// The expression is written out in each clause, so its own argument is
	// bound once per appearance, in the order the clauses read.
	if len(args) != 4 || args[0] != "month" || args[2] != 100 || args[3] != "month" {
		t.Errorf("args = %v", args)
	}
}

// A column and a call group together, in the order given.
func TestGroupBy_MixesColumnsAndCalls(t *testing.T) {
	type row struct {
		Name string
		N    int64
	}
	sql, _, err := orm.SelectAs[row](Users.With(pg()), orm.Lower(Users.Username), orm.CountAll()).
		GroupBy(Users.Age, orm.Lower(Users.Username)).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `GROUP BY "age", LOWER("username")`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// A grouping term is checked like any other: a column of a table the
// statement does not read is refused rather than compiled.
func TestGroupBy_ForeignColumnRejected(t *testing.T) {
	type row struct{ Name string }
	_, _, err := orm.SelectAs[row](Users.With(pg()), Users.Username).GroupBy(Posts.Title).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign column refused")
	}
	if !strings.Contains(err.Error(), `belongs to table "posts"`) {
		t.Errorf("error = %v, want it to name the table", err)
	}
}

func TestGroupBy_ForeignColumnInACallRejected(t *testing.T) {
	type row struct{ Name string }
	_, _, err := orm.SelectAs[row](Users.With(pg()), Users.Username).
		GroupBy(orm.Lower(Posts.Title)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign column inside the call refused")
	}
	if !strings.Contains(err.Error(), `belongs to table "posts"`) {
		t.Errorf("error = %v, want it to name the table", err)
	}
}

// A projection branched after Having or GroupBy does not leak either into
// its sibling, which every builder here owes its caller.
func TestProjection_GroupByAndHavingDoNotLeak(t *testing.T) {
	type row struct{ Name string }
	base := orm.SelectAs[row](Users.With(pg()), Users.Username).GroupBy(Users.Username)
	filtered := base.Having(orm.CountAll().GreaterThan(int64(1)))

	plain, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(plain, "HAVING") {
		t.Errorf("SQL() = %s, want the branch it was taken from unfiltered", plain)
	}
	withHaving, _, err := filtered.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(withHaving, "HAVING") {
		t.Errorf("SQL() = %s, want the branch itself filtered", withHaving)
	}
}

// COUNT(DISTINCT *) is not a thing: the argument is every row rather than a
// value to take the distinct ones of.
func TestDistinct_OnCountAll(t *testing.T) {
	type row struct{ N int64 }
	_, _, err := orm.SelectAs[row](Users.With(pg()), orm.CountAll().Distinct()).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want COUNT(*) DISTINCT refused")
	}
	if !strings.Contains(err.Error(), "count the column you mean") {
		t.Errorf("error = %v, want it to point at counting a column", err)
	}
}
