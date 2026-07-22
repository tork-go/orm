package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// Every comparison renders its own SQL spelling, reusing the names a
// column already carries.
func TestExprCompare_EveryOperator(t *testing.T) {
	tests := map[string]struct {
		pred orm.Predicate
		want string
	}{
		"Equals":         {Users.Age.Value().Equals(1), `"age" = $1`},
		"NotEquals":      {Users.Age.Value().NotEquals(1), `"age" <> $1`},
		"GreaterThan":    {Users.Age.Value().GreaterThan(1), `"age" > $1`},
		"GreaterOrEqual": {Users.Age.Value().GreaterOrEqual(1), `"age" >= $1`},
		"LessThan":       {Users.Age.Value().LessThan(1), `"age" < $1`},
		"LessOrEqual":    {Users.Age.Value().LessOrEqual(1), `"age" <= $1`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sql, _, err := Users.With(pg()).Where(tt.pred).SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if !strings.HasSuffix(sql, " WHERE "+tt.want) {
				t.Errorf("SQL() = %s\nwant a WHERE of %s", sql, tt.want)
			}
		})
	}
}

// The case Value exists for: comparing one column against another, which
// no column method could express.
func TestExprCompare_ColumnAgainstColumn(t *testing.T) {
	sql, args, err := Users.With(pg()).Where(Users.Age.Value().GreaterThan(Users.ID)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `WHERE "age" > "id"`) {
		t.Errorf("SQL() = %s, want a column-to-column comparison", sql)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none — neither side is a value", args)
	}
}

// Arithmetic needs no Value, since it already yields an expression.
func TestExprCompare_ArithmeticThenCompare(t *testing.T) {
	sql, args, err := Users.With(pg()).Where(Users.Age.Times(2).GreaterThan(100)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `WHERE ("age" * $1) > $2`) {
		t.Errorf("SQL() = %s", sql)
	}
	if len(args) != 2 || args[0] != 2 || args[1] != 100 {
		t.Errorf("args = %v, want [2 100]", args)
	}
}

// The right-hand side may itself be an expression.
func TestExprCompare_ExpressionAgainstExpression(t *testing.T) {
	sql, _, err := Users.With(pg()).
		Where(Users.Age.Times(2).GreaterThan(Users.ID.Plus(10))).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `WHERE ("age" * $1) > ("id" + $2)`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// An expression comparison is an ordinary predicate, so it composes
// wherever one goes.
func TestExprCompare_Composes(t *testing.T) {
	tests := map[string]struct {
		pred orm.Predicate
		want string
	}{
		"inside Or": {
			orm.Or(Users.Age.Value().GreaterThan(Users.ID), Users.Username.Equals("alice")),
			`("age" > "id" OR "username" = $1)`,
		},
		"inside Not": {
			orm.Not(Users.Age.Value().GreaterThan(Users.ID)),
			`NOT ("age" > "id")`,
		},
		"beside a column condition": {
			orm.And(Users.Username.Equals("alice"), Users.Age.Value().GreaterThan(Users.ID)),
			`("username" = $1 AND "age" > "id")`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sql, _, err := Users.With(pg()).Where(tt.pred).SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if !strings.HasSuffix(sql, "WHERE "+tt.want) {
				t.Errorf("SQL() = %s\nwant a WHERE of %s", sql, tt.want)
			}
		})
	}
}

// The right-hand side is checked against the expression's own Go type, so
// a wrongly typed literal is named rather than left to the database.
func TestExprCompare_LiteralTypeMismatch(t *testing.T) {
	_, _, err := Users.With(pg()).Where(Users.Age.Value().GreaterThan("x")).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the wrongly typed literal rejected")
	}
	if !strings.Contains(err.Error(), "this expression is int") ||
		!strings.Contains(err.Error(), "is string") {
		t.Errorf("error %q does not name both types", err)
	}
}

// A column outside the statement is rejected on either side, the same way
// a plain condition over one is.
func TestExprCompare_ForeignColumnRejected(t *testing.T) {
	tests := map[string]orm.Predicate{
		"left side":  Posts.ID.Value().GreaterThan(1),
		"right side": Users.Age.Value().GreaterThan(Posts.ID),
	}
	for name, pred := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := Users.With(pg()).Where(pred).SQL()
			if err == nil {
				t.Fatal("SQL() error = nil, want the foreign column rejected")
			}
			if !strings.Contains(err.Error(), `belongs to table "posts"`) {
				t.Errorf("error %q does not name the problem", err)
			}
		})
	}
}

// A joined statement can compare its two tables against each other, which
// is the shape Join plus expressions exists to allow.
func TestExprCompare_AcrossAJoin(t *testing.T) {
	sql, _, err := Books.With(pg()).Join(Books.Author).
		Where(Books.AuthorID.Value().Equals(Authors.ID)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `WHERE "books"."author_id" = "authors"."id"`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// It reaches a set operation's WHERE like any other predicate, so a write
// can be filtered on one column against another.
func TestExprCompare_InASetOperation(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).
		Where(Users.Age.Value().GreaterThan(Users.ID)).
		UpdateAll(context.Background(), Users.Username.Set("x")); err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	want := `UPDATE "users" SET "username" = $1 WHERE "age" > "id"`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
}
