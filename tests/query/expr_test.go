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
	want := `UPDATE "users" SET "age" = "age" + $1 WHERE "id" = $2`
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
	want := `UPDATE "users" SET "age" = "age" - $1 WHERE "id" = $2`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
}

// SetExpr with a literal is Increment/Decrement's own escape hatch: Mul and
// Div have no dedicated builder, so this is how a column is scaled.
func TestSetExpr_ColumnAndLiteral(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).Where(Users.ID.Equals(1)).
		UpdateAll(context.Background(), Users.Age.SetExpr(orm.Mul(Users.Age, 2))); err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	want := `UPDATE "users" SET "age" = "age" * $1 WHERE "id" = $2`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
}

func TestSetExpr_Div(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).Where(Users.ID.Equals(1)).
		UpdateAll(context.Background(), Users.Age.SetExpr(orm.Div(Users.Age, 2))); err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	want := `UPDATE "users" SET "age" = "age" / $1 WHERE "id" = $2`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
}

// SetExpr's right-hand side can name another column of the same table
// instead of a literal.
func TestSetExpr_ColumnMinusColumn(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).Where(Users.ID.Equals(1)).
		UpdateAll(context.Background(), Users.Age.SetExpr(orm.Sub(Users.Age, Users.ID))); err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	want := `UPDATE "users" SET "age" = "age" - "id" WHERE "id" = $1`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
	// Neither side of the expression binds a parameter, so the WHERE's own
	// value is the only one.
	if args := c.ExecArgs(0); len(args) != 1 || args[0] != 1 {
		t.Errorf("UpdateAll bound %v, want [1]", args)
	}
}

// A single-row Update carries an Expr assignment the same way UpdateAll
// does: writer.update renders every column's Assignment through the same
// compiler.set, expression-valued or not.
func TestIncrement_SingleRowUpdate(t *testing.T) {
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
	want := `UPDATE "users" SET "age" = "age" + $1, "username" = $2 WHERE "id" = $3`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
}

// An Expr naming a column outside the statement is rejected the same way a
// predicate over one is, whichever side of the expression it is on.
func TestSetExpr_ForeignColumnRejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	tests := map[string]struct {
		build func() (int64, error)
	}{
		"left side": {
			build: func() (int64, error) {
				return Posts.With(db).Where(Posts.ID.Equals(1)).
					UpdateAll(context.Background(), orm.Assignment{
						Col: Posts.ID, Expr: exprPtr(orm.Add(Users.Age, 1)),
					})
			},
		},
		"right side": {
			build: func() (int64, error) {
				return Users.With(db).Where(Users.ID.Equals(1)).
					UpdateAll(context.Background(), Users.Age.SetExpr(orm.Add(Users.Age, Posts.ID)))
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := tt.build()
			if err == nil {
				t.Fatal("UpdateAll() error = nil, want the foreign column rejected")
			}
			if !strings.Contains(err.Error(), "belongs to table") {
				t.Errorf("error %q does not name the problem", err)
			}
		})
	}
}

func exprPtr(e orm.Expr) *orm.Expr { return &e }
