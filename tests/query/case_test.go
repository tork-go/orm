package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

func TestCase_RendersInASelect(t *testing.T) {
	type row struct{ Band int }
	sql, args, err := orm.SelectAs[row](
		Users.With(pg()),
		orm.Case[int]().When(Users.Age.LessThan(18), 1).Else(0),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT CASE WHEN "age" < $1 THEN CAST($2 AS INTEGER) ELSE CAST($3 AS INTEGER) END FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	// Each arm binds in the order it appears in the statement, the ELSE last.
	if len(args) != 3 || args[0] != 18 || args[1] != 1 || args[2] != 0 {
		t.Errorf("args = %v, want [18 1 0]", args)
	}
}

// Arms accumulate in the order given, which is the order SQL tests them in.
func TestCase_MultipleArms(t *testing.T) {
	type row struct{ Band int }
	sql, args, err := orm.SelectAs[row](
		Users.With(pg()),
		orm.Case[int]().
			When(Users.Age.LessThan(13), 1).
			When(Users.Age.LessThan(20), 2).
			Else(3),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT CASE WHEN "age" < $1 THEN CAST($2 AS INTEGER) WHEN "age" < $3 THEN CAST($4 AS INTEGER) ELSE CAST($5 AS INTEGER) END FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 5 || args[0] != 13 || args[2] != 20 || args[4] != 3 {
		t.Errorf("args = %v, want [13 1 20 2 3]", args)
	}
}

// An arm's value may be a column or an expression, not only a literal.
func TestCase_ArmYieldsAColumn(t *testing.T) {
	type row struct{ N int }
	sql, _, err := orm.SelectAs[row](
		Users.With(pg()),
		orm.Case[int]().When(Users.Age.LessThan(18), Users.ID).Else(Users.Age.Times(2)),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT CASE WHEN "age" < $1 THEN "id" ELSE ("age" * $2) END FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

func TestCase_InAWhere(t *testing.T) {
	sql, _, err := Users.With(pg()).Where(
		orm.Case[int]().When(Users.Age.LessThan(18), 1).Else(0).Equals(1),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `WHERE CASE WHEN "age" < $1 THEN CAST($2 AS INTEGER) ELSE CAST($3 AS INTEGER) END = $4`) {
		t.Errorf("SQL() = %s", sql)
	}
}

func TestCase_InAnOrderBy(t *testing.T) {
	sql, _, err := Users.With(pg()).OrderBy(
		orm.Case[int]().When(Users.Username.Equals("admin"), 0).Else(1).Asc(),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `ORDER BY CASE WHEN "username" = $1 THEN CAST($2 AS INTEGER) ELSE CAST($3 AS INTEGER) END ASC`) {
		t.Errorf("SQL() = %s", sql)
	}
}

func TestCase_InAnAssignment(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).Where(Users.ID.Equals(1)).UpdateAll(
		context.Background(),
		Users.Age.SetExpr(orm.Case[int]().When(Users.Age.LessThan(0), 0).Else(Users.Age)),
	); err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	// The ELSE names a column, which carries its own type and so takes no
	// cast; only the bound value needs one.
	want := `UPDATE "users" SET "age" = CASE WHEN "age" < $1 THEN CAST($2 AS INTEGER) ` +
		`ELSE "age" END WHERE "id" = $3`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
}

// A CASE inside arithmetic is parenthesised by the arithmetic, not by
// itself: END already closes it.
func TestCase_InsideArithmetic(t *testing.T) {
	type row struct{ N int }
	sql, _, err := orm.SelectAs[row](
		Users.With(pg()),
		orm.Case[int]().When(Users.Age.LessThan(18), 1).Else(0).Plus(10),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT (CASE WHEN "age" < $1 THEN CAST($2 AS INTEGER) ELSE CAST($3 AS INTEGER) END + $4) FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// A pointer T is how NULL is said, since Else is required and a non-pointer
// T has no way to hold the absence of a value.
func TestCase_PointerTypeYieldsNull(t *testing.T) {
	type row struct{ Band *int }
	sql, args, err := orm.SelectAs[row](
		Users.With(pg()),
		orm.Case[*int]().When(Users.Age.LessThan(18), nil).Else(nil),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, "CASE WHEN") {
		t.Errorf("SQL() = %s", sql)
	}
	if len(args) != 3 || args[1] != nil || args[2] != nil {
		t.Errorf("args = %v, want the two nils bound", args)
	}
}

func TestCase_NoArmsRejected(t *testing.T) {
	type row struct{ N int }
	_, _, err := orm.SelectAs[row](Users.With(pg()), orm.Case[int]().Else(0)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a Case with no When rejected")
	}
	if !strings.Contains(err.Error(), "no When arms") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestCase_NilConditionRejected(t *testing.T) {
	type row struct{ N int }
	_, _, err := orm.SelectAs[row](
		Users.With(pg()), orm.Case[int]().When(nil, 1).Else(0),
	).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the nil condition rejected")
	}
	if !strings.Contains(err.Error(), "no condition") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// An arm's value and the Else are both checked against T, the same way any
// other operand is.
func TestCase_LiteralTypeMismatch(t *testing.T) {
	type row struct{ N int }
	tests := map[string]orm.Expr[int]{
		"in a When arm": orm.Case[int]().When(Users.Age.LessThan(18), "x").Else(0),
		"in the Else":   orm.Case[int]().When(Users.Age.LessThan(18), 1).Else("x"),
	}
	for name, expr := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := orm.SelectAs[row](Users.With(pg()), expr).SQL()
			if err == nil {
				t.Fatal("SQL() error = nil, want the wrongly typed value rejected")
			}
			if !strings.Contains(err.Error(), "this expression is int") {
				t.Errorf("error %q does not name the problem", err)
			}
		})
	}
}

// A condition naming a column outside the statement is rejected, the same
// way it is anywhere else.
func TestCase_ForeignColumnRejected(t *testing.T) {
	type row struct{ N int }
	_, _, err := orm.SelectAs[row](
		Users.With(pg()), orm.Case[int]().When(Posts.ID.Equals(1), 1).Else(0),
	).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign column rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "posts"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

// When copies rather than appending in place, so a builder is safe to
// branch from the way every other builder in the package is.
func TestCase_LeavesOriginalAlone(t *testing.T) {
	type row struct{ N int }
	base := orm.Case[int]().When(Users.Age.LessThan(13), 1)

	minor := base.Else(0)
	teen := base.When(Users.Age.LessThan(20), 2).Else(0)

	minorSQL, _, err := orm.SelectAs[row](Users.With(pg()), minor).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Count(minorSQL, "WHEN") != 1 {
		t.Errorf("the base builder grew an arm: %s", minorSQL)
	}
	teenSQL, _, err := orm.SelectAs[row](Users.With(pg()), teen).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Count(teenSQL, "WHEN") != 2 {
		t.Errorf("the branch did not get its own arm: %s", teenSQL)
	}
}
