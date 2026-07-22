package query_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
)

func TestWindowExpr_RowNumberNoPartitionNoOrder(t *testing.T) {
	type report struct {
		Username string
		Row      int64
	}
	sql, _, err := orm.SelectAs[report](Users.With(pg()), Users.Username, orm.RowNumber()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "username", ROW_NUMBER() OVER () FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

func TestWindowExpr_PartitionAndOrder(t *testing.T) {
	type report struct {
		Username string
		Age      int
		Row      int64
	}
	sql, _, err := orm.SelectAs[report](
		Users.With(pg()), Users.Username, Users.Age,
		orm.RowNumber().PartitionBy(Users.Age).OrderBy(Users.Username.Asc()),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "username", "age", ROW_NUMBER() OVER (PARTITION BY "age" ORDER BY "username" ASC) FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

func TestWindowExpr_PartitionOnly(t *testing.T) {
	type report struct {
		Age int
		Row int64
	}
	sql, _, err := orm.SelectAs[report](
		Users.With(pg()), Users.Age, orm.RowNumber().PartitionBy(Users.Age),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `ROW_NUMBER() OVER (PARTITION BY "age")`) {
		t.Errorf("SQL() = %s, want a PARTITION BY clause with no ORDER BY", sql)
	}
}

func TestWindowExpr_OrderOnly(t *testing.T) {
	type report struct {
		Username string
		Row      int64
	}
	sql, _, err := orm.SelectAs[report](
		Users.With(pg()), Users.Username, orm.RowNumber().OrderBy(Users.Username.Desc()),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `ROW_NUMBER() OVER (ORDER BY "username" DESC)`) {
		t.Errorf("SQL() = %s, want an ORDER BY clause with no PARTITION BY", sql)
	}
}

func TestWindowExpr_Rank(t *testing.T) {
	type report struct {
		Username string
		Rank     int64
	}
	sql, _, err := orm.SelectAs[report](
		Users.With(pg()), Users.Username, orm.Rank().OrderBy(Users.Username.Asc()),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, "RANK() OVER") {
		t.Errorf("SQL() = %s, want RANK()", sql)
	}
}

func TestWindowExpr_DenseRank(t *testing.T) {
	type report struct {
		Username string
		Rank     int64
	}
	sql, _, err := orm.SelectAs[report](
		Users.With(pg()), Users.Username, orm.DenseRank().OrderBy(Users.Username.Asc()),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, "DENSE_RANK() OVER") {
		t.Errorf("SQL() = %s, want DENSE_RANK()", sql)
	}
}

// A window function combines with a Join the same way an AggregateExpr
// does, since both go through the same SelectExpr list.
func TestWindowExpr_WithJoin(t *testing.T) {
	type report struct {
		Title string
		Row   int64
	}
	sql, _, err := orm.SelectAs[report](
		Books.With(pg()).Join(Books.Author),
		Books.Title,
		orm.RowNumber().PartitionBy(Books.AuthorID).OrderBy(Books.Title.Asc()),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "books"."title", ROW_NUMBER() OVER (PARTITION BY "books"."author_id" ORDER BY "books"."title" ASC) ` +
		`FROM "books" JOIN "authors" ON "authors"."id" = "books"."author_id"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// A PartitionBy column outside the statement's own tables is rejected the
// same way a plain column or an aggregate's column already is.
func TestWindowExpr_ForeignPartitionColumnRejected(t *testing.T) {
	type report struct {
		Name string
		Row  int64
	}
	_, _, err := orm.SelectAs[report](
		Authors.With(pg()), Authors.Name, orm.RowNumber().PartitionBy(Books.Title),
	).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign partition column rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "books"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

// An OrderBy column outside the statement's own tables is rejected too.
func TestWindowExpr_ForeignOrderColumnRejected(t *testing.T) {
	type report struct {
		Name string
		Row  int64
	}
	_, _, err := orm.SelectAs[report](
		Authors.With(pg()), Authors.Name, orm.RowNumber().OrderBy(Books.Title.Asc()),
	).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign order column rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "books"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

// PartitionBy and OrderBy each copy rather than mutate the receiver, the
// same as every other builder in the package.
func TestWindowExpr_LeavesOriginalAlone(t *testing.T) {
	base := orm.RowNumber()

	partitioned := base.PartitionBy(Users.Age)
	ordered := base.OrderBy(Users.Username.Asc())

	type baseReport struct {
		Username string
		Row      int64
	}
	sql, _, err := orm.SelectAs[baseReport](Users.With(pg()), Users.Username, base).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT "username", ROW_NUMBER() OVER () FROM "users"`; sql != want {
		t.Errorf("base was mutated: SQL() = %s, want %s", sql, want)
	}

	type withAge struct {
		Username string
		Age      int
		Row      int64
	}
	if _, _, err := orm.SelectAs[withAge](Users.With(pg()), Users.Username, Users.Age, partitioned).SQL(); err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if _, _, err := orm.SelectAs[baseReport](Users.With(pg()), Users.Username, ordered).SQL(); err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
}
