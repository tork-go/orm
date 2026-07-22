package query_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
)

// A value read back as a column, sent with its type spelled out: a
// parameter standing alone in a SELECT list has nothing beside it to say
// what it is.
func TestLit_InAProjection(t *testing.T) {
	type row struct {
		Name string
		Tag  string
	}
	sql, args, err := orm.SelectAs[row](Users.With(pg()), Users.Username, orm.Lit("user")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "username", CAST($1 AS TEXT) FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != "user" {
		t.Errorf("args = %v, want [user]", args)
	}
}

// T comes from the value, so the cast follows the Go type it was given.
func TestLit_TypesFollowTheValue(t *testing.T) {
	tests := []struct {
		name string
		expr orm.SelectExpr
		want string
	}{
		{"int", orm.Lit(0), "CAST($1 AS INTEGER)"},
		{"string", orm.Lit("x"), "CAST($1 AS TEXT)"},
		{"bool", orm.Lit(true), "CAST($1 AS BOOLEAN)"},
		{"float", orm.Lit(1.5), "CAST($1 AS DOUBLE PRECISION)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := orm.SelectAs[oneValue](Users.With(pg()), tt.expr).SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if !strings.HasPrefix(sql, "SELECT "+tt.want) {
				t.Errorf("SQL() = %s, want it to select %s", sql, tt.want)
			}
		})
	}
}

// It is an expression like any other: it combines, compares and orders.
func TestLit_Composes(t *testing.T) {
	type row struct{ N int }
	sql, args, err := orm.SelectAs[row](Users.With(pg()), orm.Lit(1).Plus(Users.Age)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `(CAST($1 AS INTEGER) + "age")`) {
		t.Errorf("SQL() = %s", sql)
	}
	if len(args) != 1 || args[0] != 1 {
		t.Errorf("args = %v, want [1]", args)
	}
}

func TestLit_InAWhere(t *testing.T) {
	sql, args, err := Users.With(pg()).Where(orm.Lit(1).Equals(1)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `WHERE CAST($1 AS INTEGER) = $2`) {
		t.Errorf("SQL() = %s", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, want both sides bound", args)
	}
}

// A literal is a value, not a way to write SQL: what goes in comes back as
// characters, never as a condition.
func TestLit_IsNotAWayToWriteSQL(t *testing.T) {
	sql, args, err := orm.SelectAs[oneValue](Users.With(pg()), orm.Lit("1=1 OR TRUE")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(sql, "1=1") {
		t.Errorf("SQL() = %s, want the value bound rather than written", sql)
	}
	if len(args) != 1 || args[0] != "1=1 OR TRUE" {
		t.Errorf("args = %v, want the value bound whole", args)
	}
}

// It is what tags one arm of a union apart from another's.
func TestLit_TagsAUnionArm(t *testing.T) {
	type tagged struct {
		Name string
		Tag  string
	}
	left := orm.SelectAs[tagged](Users.With(pg()), Users.Username, orm.Lit("user"))
	sql, args, err := left.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `CAST($1 AS TEXT)`) || args[0] != "user" {
		t.Errorf("SQL() = %s, args = %v", sql, args)
	}
}
