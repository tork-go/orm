package query_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

func TestSelectAs_PlainColumns(t *testing.T) {
	type report struct {
		ID    int
		Title string
	}

	sql, _, err := orm.SelectAs[report](Books.With(pg()), Books.ID, Books.Title).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT "id", "title" FROM "books"`; sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// The motivating example: a Join paired with an aggregate and a GroupBy,
// producing a combined report SelectAs is what makes possible at all.
func TestSelectAs_JoinAggregateAndGroupBy(t *testing.T) {
	type report struct {
		Name      string
		BookCount int64
	}

	sql, args, err := orm.SelectAs[report](
		Authors.With(pg()).LeftJoin(Authors.Books),
		Authors.Name,
		orm.CountOf(Books.ID),
	).GroupBy(Authors.Name).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "authors"."name", COUNT("books"."id") FROM "authors" ` +
		`LEFT JOIN "books" ON "books"."author_id" = "authors"."id" GROUP BY "authors"."name"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none", args)
	}
}

func TestSelectAs_CountAll(t *testing.T) {
	type report struct {
		Name  string
		Total int64
	}
	sql, _, err := orm.SelectAs[report](Authors.With(pg()), Authors.Name, orm.CountAll()).
		GroupBy(Authors.Name).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, "COUNT(*)") {
		t.Errorf("SQL() = %s, want COUNT(*)", sql)
	}
}

func TestSelectAs_SumMinMaxAvg(t *testing.T) {
	type report struct {
		Total    int
		Cheapest int
		Priciest int
		Mean     float64
	}
	sql, _, err := orm.SelectAs[report](
		Users.With(pg()),
		orm.SumOf(Users.Age),
		orm.MinOf(Users.Age),
		orm.MaxOf(Users.Age),
		orm.AvgOf(Users.Age),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT SUM("age"), MIN("age"), MAX("age"), AVG("age") FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

func TestSelectAs_Having(t *testing.T) {
	type report struct {
		Name      string
		BookCount int64
	}
	bookCount := orm.CountOf(Books.ID)

	sql, args, err := orm.SelectAs[report](
		Authors.With(pg()).LeftJoin(Authors.Books),
		Authors.Name,
		bookCount,
	).GroupBy(Authors.Name).Having(bookCount, orm.OpGte, 3).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "authors"."name", COUNT("books"."id") FROM "authors" ` +
		`LEFT JOIN "books" ON "books"."author_id" = "authors"."id" ` +
		`GROUP BY "authors"."name" HAVING COUNT("books"."id") >= $1`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != 3 {
		t.Errorf("args = %v, want [3]", args)
	}
}

// Having accepts an aggregate not in the SELECT list, matching ordinary
// SQL rather than restricting to SelectAs's own expressions.
func TestSelectAs_HavingAggregateNotInSelectList(t *testing.T) {
	type report struct{ Name string }
	sql, _, err := orm.SelectAs[report](Authors.With(pg()).LeftJoin(Authors.Books), Authors.Name).
		GroupBy(Authors.Name).Having(orm.CountAll(), orm.OpGt, 0).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, "HAVING COUNT(*) > $1") {
		t.Errorf("SQL() = %s, want the HAVING clause", sql)
	}
}

func TestSelectAs_OrderByAndLimit(t *testing.T) {
	type report struct{ Username string }
	sql, _, err := orm.SelectAs[report](Users.With(pg()), Users.Username).
		OrderBy(Users.Username.Asc()).Limit(5).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "username" FROM "users" ORDER BY "username" ASC LIMIT 5`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

func TestSelectAs_NegativeLimit(t *testing.T) {
	type report struct{ Username string }
	_, _, err := orm.SelectAs[report](Users.With(pg()), Users.Username).Limit(-1).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the negative limit rejected")
	}
}

// SelectAs carries the source query's own conditions, the same way
// orm.Select and the aggregate functions do.
func TestSelectAs_CarriesTheQuery(t *testing.T) {
	type report struct{ Username string }
	sql, args, err := orm.SelectAs[report](
		Users.With(pg()).Where(Users.Age.Gt(18)), Users.Username,
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT "username" FROM "users" WHERE "age" > $1`; sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != 18 {
		t.Errorf("args = %v, want [18]", args)
	}
}

func TestSelectAs_FieldCountMismatch(t *testing.T) {
	type report struct {
		Username string
		Age      int
	}
	_, _, err := orm.SelectAs[report](Users.With(pg()), Users.Username).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the field count mismatch rejected")
	}
	if !strings.Contains(err.Error(), "1 expression(s) were given") {
		t.Errorf("error %q does not name the mismatch", err)
	}
}

func TestSelectAs_FieldTypeMismatch(t *testing.T) {
	type report struct{ Username int } // string column, int field
	_, _, err := orm.SelectAs[report](Users.With(pg()), Users.Username).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the type mismatch rejected")
	}
	if !strings.Contains(err.Error(), `field 0, "Username"`) {
		t.Errorf("error %q does not name the mismatched field", err)
	}
}

func TestSelectAs_NonStructType(t *testing.T) {
	_, _, err := orm.SelectAs[string](Users.With(pg()), Users.Username).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a non-struct T rejected")
	}
	if !strings.Contains(err.Error(), "is not a struct") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestSelectAs_NoQuery(t *testing.T) {
	type report struct{ Username string }
	_, _, err := orm.SelectAs[report](nil, Users.Username).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the missing query rejected")
	}
	if !strings.Contains(err.Error(), "no query") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestSelectAs_NoHandle(t *testing.T) {
	type report struct{ Username string }
	_, _, err := orm.SelectAs[report](Users.With(nil), Users.Username).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the missing handle rejected")
	}
}

// An expression from outside this package, unrecognised by the compiler's
// type switch, is a real error rather than a defensive one: SelectExpr's
// only method is trivially implementable by anyone.
type customExpr struct{}

func (customExpr) GoType() reflect.Type { return reflect.TypeFor[string]() }

func TestSelectAs_UnknownExpression(t *testing.T) {
	type report struct{ X string }
	_, _, err := orm.SelectAs[report](Users.With(pg()), customExpr{}).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the unknown expression rejected")
	}
	if !strings.Contains(err.Error(), "unknown select expression") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestSelectAs_LockRejected(t *testing.T) {
	type report struct{ Username string }
	_, _, err := orm.SelectAs[report](Users.With(pg()).Where(Users.ID.Gt(0)).ForUpdate(), Users.Username).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the lock rejected")
	}
}

// A plain column in the SELECT list itself, not just in GroupBy or Having,
// is checked against the statement's own tables the same way Select's is.
func TestSelectAs_ForeignColumnInListRejected(t *testing.T) {
	type report struct{ Title string }
	_, _, err := orm.SelectAs[report](Authors.With(pg()), Books.Title).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign column rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "books"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

// An aggregate in the SELECT list itself is checked the same way.
func TestSelectAs_ForeignAggregateInListRejected(t *testing.T) {
	type report struct {
		Name  string
		Total int64
	}
	_, _, err := orm.SelectAs[report](Authors.With(pg()), Authors.Name, orm.CountOf(Books.ID)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign aggregate rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "books"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

// compile builds its own compiler independently of every other terminal
// and rejects a bad Join the same way they do.
func TestSelectAs_JoinRejected(t *testing.T) {
	type report struct {
		ID    int
		Title string
	}
	_, _, err := orm.SelectAs[report](Books.With(pg()).Join(Books.Tags), Books.ID, Books.Title).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the many to many join rejected")
	}
	if !strings.Contains(err.Error(), "many to many") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// A condition on the source query itself, naming a column outside this
// statement, is rejected the same way Where already rejects one directly.
func TestSelectAs_WhereForeignColumnRejected(t *testing.T) {
	type report struct{ Name string }
	_, _, err := orm.SelectAs[report](
		Authors.With(pg()).Where(Books.Title.Eq("x")), Authors.Name,
	).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign condition rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "books"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestSelectAs_OrderByForeignColumnRejected(t *testing.T) {
	type report struct{ Name string }
	_, _, err := orm.SelectAs[report](Authors.With(pg()), Authors.Name).
		OrderBy(Books.Title.Asc()).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign OrderBy column rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "books"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestSelectAs_GroupByForeignColumnRejected(t *testing.T) {
	type report struct{ Name string }
	_, _, err := orm.SelectAs[report](Authors.With(pg()), Authors.Name).
		GroupBy(Books.Title).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign GROUP BY column rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "books"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestSelectAs_HavingForeignAggregateRejected(t *testing.T) {
	type report struct{ Name string }
	_, _, err := orm.SelectAs[report](Authors.With(pg()), Authors.Name).
		GroupBy(Authors.Name).Having(orm.CountOf(Books.ID), orm.OpGt, 0).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign column in Having rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "books"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestSelectAs_All(t *testing.T) {
	type report struct {
		Username string
		Age      int
	}
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice", 30}, []any{"bob", 41})
	db := orm.NewDB(c, postgres.Dialect{})

	rows, err := orm.SelectAs[report](Users.With(db), Users.Username, Users.Age).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(rows) != 2 || rows[0].Username != "alice" || rows[1].Age != 41 {
		t.Errorf("rows = %+v, want [{alice 30} {bob 41}]", rows)
	}
}

func TestSelectAs_First(t *testing.T) {
	type report struct{ Username string }
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice"})
	db := orm.NewDB(c, postgres.Dialect{})

	row, err := orm.SelectAs[report](Users.With(db), Users.Username).First(context.Background())
	if err != nil {
		t.Fatalf("First() error = %v", err)
	}
	if row.Username != "alice" {
		t.Errorf("row = %+v, want {alice}", row)
	}
	if got := c.QueryCalls()[0]; !strings.HasSuffix(got, "LIMIT 1") {
		t.Errorf("First ran %s, want a LIMIT 1", got)
	}
}

func TestSelectAs_First_NoRows(t *testing.T) {
	type report struct{ Username string }
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	_, err := orm.SelectAs[report](Users.With(db), Users.Username).First(context.Background())
	if !errors.Is(err, orm.ErrNoRows) {
		t.Errorf("First() error = %v, want ErrNoRows", err)
	}
}

// All wraps compile's own error the same way it wraps a driver failure.
func TestSelectAs_All_CompileErrorSurfaces(t *testing.T) {
	type report struct {
		Username string
		Age      int
	}
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := orm.SelectAs[report](Users.With(db), Users.Username).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want the field count mismatch rejected")
	}
}

// First surfaces whatever error All (built on compile) hits, the same way
// it surfaces ErrNoRows for an empty result.
func TestSelectAs_First_CompileErrorSurfaces(t *testing.T) {
	type report struct {
		Username string
		Age      int
	}
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := orm.SelectAs[report](Users.With(db), Users.Username).First(context.Background())
	if err == nil {
		t.Fatal("First() error = nil, want the field count mismatch rejected")
	}
}

// A scan failure — here, more columns queued than T has fields for — is
// reported rather than silently dropped, the same as RawQuery's is.
func TestSelectAs_ScanFailure(t *testing.T) {
	type report struct{ Username string }
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice", 30})
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := orm.SelectAs[report](Users.With(db), Users.Username).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want the scan failure reported")
	}
}

func TestSelectAs_ExecFailure(t *testing.T) {
	type report struct{ Username string }
	c := fakedriver.NewConn()
	c.FailOn(`SELECT "username" FROM "users"`)
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := orm.SelectAs[report](Users.With(db), Users.Username).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want the driver's failure")
	}
}

func TestSelectAs_RowsErrIsReported(t *testing.T) {
	type report struct{ Username string }
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice"})
	c.RowsErr = errors.New("connection lost")
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := orm.SelectAs[report](Users.With(db), Users.Username).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want rows.Err() reported")
	}
	if !strings.Contains(err.Error(), "reading rows") {
		t.Errorf("error %q does not say what failed", err)
	}
}

// GroupBy, OrderBy, Limit and Having all clone rather than narrow in
// place, the same as every other builder in the package.
func TestSelectAs_LeavesOriginalAlone(t *testing.T) {
	type report struct{ Username string }
	base := orm.SelectAs[report](Users.With(pg()), Users.Username)
	want, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}

	branches := map[string]func() *orm.Projection[report]{
		"GroupBy": func() *orm.Projection[report] { return base.GroupBy(Users.Username) },
		"OrderBy": func() *orm.Projection[report] { return base.OrderBy(Users.Username.Asc()) },
		"Limit":   func() *orm.Projection[report] { return base.Limit(5) },
	}
	for name, branch := range branches {
		t.Run(name, func(t *testing.T) {
			narrowed, _, err := branch().SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if narrowed == want {
				t.Errorf("%s did not narrow anything", name)
			}
			got, _, err := base.SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if got != want {
				t.Errorf("%s changed the query it was called on:\n got %s\nwant %s", name, got, want)
			}
		})
	}
}
