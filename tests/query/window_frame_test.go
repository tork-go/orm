package query_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
)

// An aggregate windowed is the same call with an OVER clause between it and
// its arguments — the running total this phase exists for.
func TestWindow_RunningTotal(t *testing.T) {
	type row struct {
		Username string
		Running  int
	}
	sql, _, err := orm.SelectAs[row](Users.With(pg()),
		Users.Username,
		orm.SumOf(Users.Age).Over().OrderBy(Users.Username.Asc()),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "username", SUM("age") OVER (ORDER BY "username" ASC) FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// Over with nothing after it is an aggregate over the whole result, which is
// what puts a grand total beside every row.
func TestWindow_OverTheWholeResult(t *testing.T) {
	type row struct {
		Username string
		Total    int
	}
	sql, _, err := orm.SelectAs[row](Users.With(pg()), Users.Username, orm.SumOf(Users.Age).Over()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `SUM("age") OVER ()`) {
		t.Errorf("SQL() = %s, want an empty OVER clause", sql)
	}
}

// PartitionBy and OrderBy imply the window, so Over need not be written.
func TestWindow_PartitionImpliesOver(t *testing.T) {
	type row struct {
		Username string
		Mean     float64
	}
	sql, _, err := orm.SelectAs[row](Users.With(pg()),
		Users.Username, orm.AvgOf(Users.Age).PartitionBy(Users.Age),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `AVG("age") OVER (PARTITION BY "age")`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// The ranking functions carry an OVER clause from the start.
func TestWindow_RankingFunctions(t *testing.T) {
	tests := []struct {
		name string
		expr orm.SelectExpr
		want string
	}{
		{"RowNumber", orm.RowNumber(), `ROW_NUMBER() OVER ()`},
		{"Rank", orm.Rank(), `RANK() OVER ()`},
		{"DenseRank", orm.DenseRank(), `DENSE_RANK() OVER ()`},
		{"NTile", orm.NTile(4), `NTILE(CAST($1 AS INTEGER)) OVER ()`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			type row struct{ N int64 }
			sql, _, err := orm.SelectAs[row](Users.With(pg()), tt.expr).SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if !strings.HasPrefix(sql, "SELECT "+tt.want) {
				t.Errorf("SQL() = %s\nwant it to select %s", sql, tt.want)
			}
		})
	}
}

// Lag and Lead are typed as pointers, since the first and last rows of each
// window have no neighbour to read.
func TestWindow_LagAndLead(t *testing.T) {
	type row struct {
		Previous *int
		Next     *int
	}
	sql, _, err := orm.SelectAs[row](Users.With(pg()),
		orm.Lag(Users.Age).OrderBy(Users.Username.Asc()),
		orm.Lead(Users.Age).OrderBy(Users.Username.Asc()),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT LAG("age") OVER (ORDER BY "username" ASC), ` +
		`LEAD("age") OVER (ORDER BY "username" ASC) FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// Coalesce is what turns Lag's absent neighbour into a value, which is the
// pairing its own documentation points at.
func TestWindow_LagWithFallback(t *testing.T) {
	type row struct{ Previous int }
	sql, _, err := orm.SelectAs[row](Users.With(pg()),
		orm.Coalesce[int](orm.Lag(Users.Age).OrderBy(Users.Username.Asc()), 0),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `COALESCE(LAG("age") OVER (ORDER BY "username" ASC), CAST($1 AS INTEGER))`) {
		t.Errorf("SQL() = %s", sql)
	}
}

func TestWindow_FirstAndLastValue(t *testing.T) {
	type row struct {
		First int
		Last  int
	}
	sql, _, err := orm.SelectAs[row](Users.With(pg()),
		orm.FirstValue(Users.Age).OrderBy(Users.Username.Asc()),
		orm.LastValue(Users.Age).
			OrderBy(Users.Username.Asc()).
			Rows(orm.UnboundedPreceding(), orm.UnboundedFollowing()),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT FIRST_VALUE("age") OVER (ORDER BY "username" ASC), ` +
		`LAST_VALUE("age") OVER (ORDER BY "username" ASC ` +
		`ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING) FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// Every frame bound, in both frame kinds.
func TestWindow_FrameBounds(t *testing.T) {
	tests := []struct {
		name string
		expr orm.SelectExpr
		want string
	}{
		{
			"rows unbounded preceding to current row",
			orm.SumOf(Users.Age).OrderBy(Users.Username.Asc()).
				Rows(orm.UnboundedPreceding(), orm.CurrentRow()),
			"ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW",
		},
		{
			"rows n preceding to n following",
			orm.SumOf(Users.Age).OrderBy(Users.Username.Asc()).
				Rows(orm.Preceding(2), orm.Following(3)),
			"ROWS BETWEEN 2 PRECEDING AND 3 FOLLOWING",
		},
		{
			"rows current row to unbounded following",
			orm.SumOf(Users.Age).OrderBy(Users.Username.Asc()).
				Rows(orm.CurrentRow(), orm.UnboundedFollowing()),
			"ROWS BETWEEN CURRENT ROW AND UNBOUNDED FOLLOWING",
		},
		{
			"range unbounded preceding to current row",
			orm.SumOf(Users.Age).OrderBy(Users.Username.Asc()).
				Range(orm.UnboundedPreceding(), orm.CurrentRow()),
			"RANGE BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			type row struct{ N int }
			sql, _, err := orm.SelectAs[row](Users.With(pg()), tt.expr).SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if !strings.Contains(sql, tt.want) {
				t.Errorf("SQL() = %s\nwant it to contain %s", sql, tt.want)
			}
		})
	}
}

// A frame's offsets are written literally rather than bound, so a window
// carries no arguments of its own.
func TestWindow_FrameOffsetsAreNotBound(t *testing.T) {
	type row struct{ N int }
	_, args, err := orm.SelectAs[row](Users.With(pg()),
		orm.SumOf(Users.Age).OrderBy(Users.Username.Asc()).Rows(orm.Preceding(2), orm.CurrentRow()),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none: a frame's offsets are literals", args)
	}
}

// Partition and order accumulate across calls, as every other builder here
// does.
func TestWindow_ClausesAccumulate(t *testing.T) {
	type row struct{ N int64 }
	sql, _, err := orm.SelectAs[row](Users.With(pg()),
		orm.RowNumber().
			PartitionBy(Users.Age).PartitionBy(Users.Email).
			OrderBy(Users.Username.Asc()).OrderBy(Users.ID.Desc()),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `ROW_NUMBER() OVER (PARTITION BY "age", "email" ` +
		`ORDER BY "username" ASC, "id" DESC)`
	if !strings.Contains(sql, want) {
		t.Errorf("SQL() = %s\nwant   %s", sql, want)
	}
}

// A window is a value: branching from one must not let either branch see the
// other's clauses.
func TestWindow_DoesNotLeakBetweenBranches(t *testing.T) {
	base := orm.RowNumber().OrderBy(Users.Username.Asc())
	byAge := base.PartitionBy(Users.Age)

	type row struct{ N int64 }
	plain, _, err := orm.SelectAs[row](Users.With(pg()), base).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(plain, "PARTITION BY") {
		t.Errorf("SQL() = %s, want the window it was branched from unpartitioned", plain)
	}
	partitioned, _, err := orm.SelectAs[row](Users.With(pg()), byAge).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(partitioned, `PARTITION BY "age"`) {
		t.Errorf("SQL() = %s, want the branch itself partitioned", partitioned)
	}
}

// The window's own ordering is not the statement's: a read may sort one way
// and count another.
func TestWindow_OrderingIsSeparateFromTheStatements(t *testing.T) {
	type row struct {
		Username string
		N        int64
	}
	sql, _, err := orm.SelectAs[row](Users.With(pg()),
		Users.Username, orm.RowNumber().OrderBy(Users.Age.Desc()),
	).OrderBy(Users.Username.Asc()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `ROW_NUMBER() OVER (ORDER BY "age" DESC)`) ||
		!strings.HasSuffix(sql, `ORDER BY "username" ASC`) {
		t.Errorf("SQL() = %s, want the two orderings kept apart", sql)
	}
}

// The shape a window function is usually for: rank inside a derived table,
// then filter on the rank.
func TestWindow_FilteredThroughADerivedTable(t *testing.T) {
	inner := orm.SelectAs[Ranked](Users.With(pg()),
		Users.Username,
		orm.RowNumber().PartitionBy(Users.Age).OrderBy(Users.Username.Asc()),
	)
	sql, _, err := RankedT.From(inner).Where(RankedT.Rank.LessOrEqual(3)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "username", "rank" FROM (SELECT "username" AS "username", ` +
		`ROW_NUMBER() OVER (PARTITION BY "age" ORDER BY "username" ASC) AS "rank" ` +
		`FROM "users") AS "ranked" WHERE "rank" <= $1`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}
