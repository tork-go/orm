package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

func TestIncrement_Renders(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).Where(Users.ID.Equals(1)).
		UpdateAll(context.Background(), Users.Age.Increment(1)); err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	want := `UPDATE "users" SET "age" = ("age" + $1) WHERE "id" = $2`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
	if args := c.ExecArgs(0); len(args) != 2 || args[0] != 1 || args[1] != 1 {
		t.Errorf("UpdateAll bound %v, want [1 1]", args)
	}
}

func TestDecrement_Renders(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).Where(Users.ID.Equals(1)).
		UpdateAll(context.Background(), Users.Age.Decrement(2)); err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	want := `UPDATE "users" SET "age" = ("age" - $1) WHERE "id" = $2`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
}

// Every operator renders its own SQL spelling.
func TestExpr_EveryOperator(t *testing.T) {
	tests := map[string]struct {
		expr orm.Expr[int]
		want string
	}{
		"Plus":      {Users.Age.Plus(2), `("age" + $1)`},
		"Minus":     {Users.Age.Minus(2), `("age" - $1)`},
		"Times":     {Users.Age.Times(2), `("age" * $1)`},
		"DividedBy": {Users.Age.DividedBy(2), `("age" / $1)`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			type row struct{ N int }
			sql, _, err := orm.SelectAs[row](Users.With(pg()), tt.expr).SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if want := "SELECT " + tt.want + ` FROM "users"`; sql != want {
				t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
			}
		})
	}
}

// An expression nests, and every level is parenthesised, so what the Go
// reads as is what the database computes regardless of its own precedence.
func TestExpr_Nests(t *testing.T) {
	type row struct{ N int }
	sql, args, err := orm.SelectAs[row](
		Users.With(pg()), Users.Age.Times(2).Plus(1),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT (("age" * $1) + $2) FROM "users"`; sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	// The left operand renders first, so the inner multiply binds before the
	// outer add.
	if len(args) != 2 || args[0] != 2 || args[1] != 1 {
		t.Errorf("args = %v, want [2 1]", args)
	}
}

// An operand may be another column rather than a literal, which binds
// nothing at all.
func TestExpr_ColumnOperand(t *testing.T) {
	type row struct{ N int }
	sql, args, err := orm.SelectAs[row](Users.With(pg()), Users.Age.Minus(Users.ID)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT ("age" - "id") FROM "users"`; sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none", args)
	}
}

// An operand may be another expression, not only a column or a literal.
func TestExpr_ExpressionOperand(t *testing.T) {
	type row struct{ N int }
	sql, _, err := orm.SelectAs[row](
		Users.With(pg()), Users.Age.Times(Users.ID.Plus(1)),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT ("age" * ("id" + $1)) FROM "users"`; sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// Value lifts a bare column, which renders as the column itself.
func TestExpr_ValueLift(t *testing.T) {
	type row struct{ Name string }
	sql, _, err := orm.SelectAs[row](Users.With(pg()), Users.Username.Value()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT "username" FROM "users"`; sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// The price of chaining without Go overloading: an operand is an any, so a
// wrongly typed literal is caught when the statement builds rather than by
// the compiler. It names both types, the way Find's key check does.
func TestExpr_LiteralTypeMismatch(t *testing.T) {
	type row struct{ N int }
	_, _, err := orm.SelectAs[row](Users.With(pg()), Users.Age.Plus("two")).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the wrongly typed literal rejected")
	}
	if !strings.Contains(err.Error(), "this expression is int") ||
		!strings.Contains(err.Error(), "is string") {
		t.Errorf("error %q does not name both types", err)
	}
}

// A nested expression's operand is checked too, not only a top-level one.
func TestExpr_LiteralTypeMismatchWhenNested(t *testing.T) {
	type row struct{ N int }
	_, _, err := orm.SelectAs[row](Users.With(pg()), Users.Age.Times(2).Plus("x")).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the wrongly typed literal rejected")
	}
}

// A nil literal binds as NULL rather than tripping the type check, since a
// nil has no type to compare against.
func TestExpr_NilLiteralBinds(t *testing.T) {
	type row struct{ N int }
	sql, args, err := orm.SelectAs[row](Users.With(pg()), Users.Age.Plus(nil)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT ("age" + $1) FROM "users"`; sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != nil {
		t.Errorf("args = %v, want [<nil>]", args)
	}
}

func TestSetExpr_TimesLiteral(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).Where(Users.ID.Equals(1)).
		UpdateAll(context.Background(), Users.Age.SetExpr(Users.Age.Times(2))); err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	want := `UPDATE "users" SET "age" = ("age" * $1) WHERE "id" = $2`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
}

func TestSetExpr_DividedBy(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).Where(Users.ID.Equals(1)).
		UpdateAll(context.Background(), Users.Age.SetExpr(Users.Age.DividedBy(2))); err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	want := `UPDATE "users" SET "age" = ("age" / $1) WHERE "id" = $2`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
}

// SetExpr's expression can name another column of the same table instead of
// a literal, binding nothing.
func TestSetExpr_ColumnMinusColumn(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).Where(Users.ID.Equals(1)).
		UpdateAll(context.Background(), Users.Age.SetExpr(Users.Age.Minus(Users.ID))); err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	want := `UPDATE "users" SET "age" = ("age" - "id") WHERE "id" = $1`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
	// Neither side of the expression binds a parameter, so the WHERE's own
	// value is the only one.
	if args := c.ExecArgs(0); len(args) != 1 || args[0] != 1 {
		t.Errorf("UpdateAll bound %v, want [1]", args)
	}
}

// An expression assignment sits beside an ordinary one, both rendered
// through the same compiler.set.
func TestIncrement_BesideAPlainAssignment(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).Where(Users.ID.Equals(1)).
		UpdateAll(context.Background(),
			Users.Age.Increment(1),
			Users.Username.Set("renamed"),
		); err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	want := `UPDATE "users" SET "age" = ("age" + $1), "username" = $2 WHERE "id" = $3`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
}

// An expression naming a column outside the statement is rejected the same
// way a predicate over one is, whichever side of the expression it is on.
func TestSetExpr_ForeignColumnRejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	tests := map[string]func() (int64, error){
		"left side": func() (int64, error) {
			return Posts.With(db).Where(Posts.ID.Equals(1)).
				UpdateAll(context.Background(), orm.Assignment{
					Col: Posts.ID, Expr: Users.Age.Plus(1),
				})
		},
		"right side": func() (int64, error) {
			return Users.With(db).Where(Users.ID.Equals(1)).
				UpdateAll(context.Background(), Users.Age.SetExpr(Users.Age.Plus(Posts.ID)))
		},
		"lifted column": func() (int64, error) {
			return Posts.With(db).Where(Posts.ID.Equals(1)).
				UpdateAll(context.Background(), orm.Assignment{
					Col: Posts.ID, Expr: Users.Age.Value(),
				})
		},
	}
	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := build()
			if err == nil {
				t.Fatal("UpdateAll() error = nil, want the foreign column rejected")
			}
			if !strings.Contains(err.Error(), "belongs to table") {
				t.Errorf("error %q does not name the problem", err)
			}
		})
	}
}

// A computed column read into a caller's own struct is what making Expr a
// SelectExpr buys, and needs no wrapper to get there.
func TestExpr_AsProjection(t *testing.T) {
	type report struct {
		Username string
		Doubled  int
	}
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice", 60})
	db := orm.NewDB(c, postgres.Dialect{})

	rows, err := orm.SelectAs[report](
		Users.With(db), Users.Username, Users.Age.Times(2),
	).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(rows) != 1 || rows[0].Username != "alice" || rows[0].Doubled != 60 {
		t.Errorf("rows = %+v, want [{alice 60}]", rows)
	}
	if got := c.QueryCalls()[0]; got != `SELECT "username", ("age" * $1) FROM "users"` {
		t.Errorf("All ran %s", got)
	}
}

// An expression's Go type is the column's, so SelectAs checks a projection
// field against it the same way it checks a plain column.
func TestExpr_ProjectionTypeMismatch(t *testing.T) {
	type report struct{ Doubled string } // int expression, string field
	_, _, err := orm.SelectAs[report](Users.With(pg()), Users.Age.Times(2)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the field type mismatch rejected")
	}
	if !strings.Contains(err.Error(), "is string but expression 0 is int") {
		t.Errorf("error %q does not name the mismatch", err)
	}
}
