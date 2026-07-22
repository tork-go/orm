package query_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tork-go/orm"
)

// A call over a column reads as the function applied to that column.
func TestFn_OverAColumn(t *testing.T) {
	type row struct{ Name string }
	sql, args, err := orm.SelectAs[row](Users.With(pg()), orm.Fn[string]("lower", Users.Username)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if sql != `SELECT lower("username") FROM "users"` {
		t.Errorf("SQL() = %s", sql)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none: a column is written, not bound", args)
	}
}

// The name is written exactly as given. A caller reaching for Fn has chosen
// a spelling, and folding it would call a function they did not name.
func TestFn_NameIsWrittenAsGiven(t *testing.T) {
	type row struct{ Name string }
	for _, name := range []string{"lower", "LOWER", "Lower", "pg_catalog.lower"} {
		sql, _, err := orm.SelectAs[row](Users.With(pg()), orm.Fn[string](name, Users.Username)).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if !strings.HasPrefix(sql, "SELECT "+name+`("username")`) {
			t.Errorf("Fn(%q) rendered %s", name, sql)
		}
	}
}

// A value argument is bound, and told what it is: a bare parameter inside a
// call has nothing beside it to settle its type.
func TestFn_ValueArgumentIsBoundAndCast(t *testing.T) {
	type row struct{ Month time.Time }
	sql, args, err := orm.SelectAs[row](Users.With(pg()),
		orm.Fn[time.Time]("date_trunc", "month", Users.CreatedAt)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT date_trunc(CAST($1 AS TEXT), "created_at") FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != "month" {
		t.Errorf("args = %v, want [month]", args)
	}
}

// The cast names the argument's own type, not the call's result type: a
// function's result says nothing about what goes into it.
func TestFn_ArgumentCastIsTheArgumentsOwnType(t *testing.T) {
	type row struct{ N int }
	sql, _, err := orm.SelectAs[row](Users.With(pg()), orm.Fn[int]("width_bucket", Users.Age, 10)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, "CAST($1 AS INTEGER)") {
		t.Errorf("SQL() = %s, want the integer argument cast as one", sql)
	}
}

// A call with no arguments is legal and common — NOW(), CURRENT_DATE.
func TestFn_NoArguments(t *testing.T) {
	type row struct{ At time.Time }
	sql, _, err := orm.SelectAs[row](Users.With(pg()), orm.Fn[time.Time]("now")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if sql != `SELECT now() FROM "users"` {
		t.Errorf("SQL() = %s", sql)
	}
}

// Arguments may be columns, expressions and values in any mix.
func TestFn_MixedArguments(t *testing.T) {
	type row struct{ S string }
	sql, args, err := orm.SelectAs[row](Users.With(pg()),
		orm.Fn[string]("concat_ws", ", ", Users.Username, orm.Lower(Users.Username)),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT concat_ws(CAST($1 AS TEXT), "username", LOWER("username")) FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != ", " {
		t.Errorf("args = %v", args)
	}
}

// A call is an expression, so it goes everywhere one already does.
func TestFn_InAWhere(t *testing.T) {
	sql, args, err := Users.With(pg()).Where(orm.Lower(Users.Username).Equals("ada")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `WHERE LOWER("username") = $1`) {
		t.Errorf("SQL() = %s", sql)
	}
	if len(args) != 1 || args[0] != "ada" {
		t.Errorf("args = %v, want [ada]", args)
	}
}

func TestFn_InAnOrderBy(t *testing.T) {
	sql, _, err := Users.With(pg()).OrderBy(orm.Lower(Users.Username).Asc()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `ORDER BY LOWER("username") ASC`) {
		t.Errorf("SQL() = %s", sql)
	}
}

func TestFn_InArithmetic(t *testing.T) {
	type row struct{ N int }
	sql, _, err := orm.SelectAs[row](Users.With(pg()),
		orm.Fn[int]("char_length", Users.Username).Plus(1)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `(char_length("username") + $1)`) {
		t.Errorf("SQL() = %s, want the call inside the arithmetic", sql)
	}
}

func TestFn_InsideACase(t *testing.T) {
	type row struct{ Label string }
	sql, _, err := orm.SelectAs[row](Users.With(pg()),
		orm.Case[string]().
			When(orm.Lower(Users.Username).Equals("ada"), "founder").
			Else("member"),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `CASE WHEN LOWER("username") = $1 THEN`) {
		t.Errorf("SQL() = %s, want the call as the arm's condition", sql)
	}
}

// Nesting one call inside another is what an expression tree is for.
func TestFn_Nested(t *testing.T) {
	type row struct{ N int64 }
	sql, _, err := orm.SelectAs[row](Users.With(pg()), orm.Length(orm.Trim(Users.Username))).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if sql != `SELECT LENGTH(TRIM("username")) FROM "users"` {
		t.Errorf("SQL() = %s", sql)
	}
}

// A call carries into a derived table like any other projected expression,
// which is what lets a computed value be filtered on: a WHERE cannot name a
// call the same statement computes.
func TestFn_InADerivedTable(t *testing.T) {
	inner := orm.SelectAs[foldedName](Users.With(pg()), orm.Lower(Users.Username))
	sql, _, err := foldedNames.From(inner).Where(foldedNames.Username.Equals("ada")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "username" FROM (SELECT LOWER("username") AS "username" FROM "users") ` +
		`AS "folded_names" WHERE "username" = $1`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

type foldedName struct{ Username string }

type foldedNameModel struct {
	orm.DerivedTable[foldedName]
	Username *orm.StringColumn
}

var foldedNames = orm.DefineDerived[foldedName]("folded_names",
	func(t *orm.TableBuilder[foldedName]) *foldedNameModel {
		return &foldedNameModel{DerivedTable: t.Derived(), Username: t.String("username")}
	})

// The named helpers, each over a column.
func TestFn_NamedHelpers(t *testing.T) {
	tests := []struct {
		name string
		expr orm.SelectExpr
		want string
	}{
		{"Lower", orm.Lower(Users.Username), `LOWER("username")`},
		{"Upper", orm.Upper(Users.Username), `UPPER("username")`},
		{"Trim", orm.Trim(Users.Username), `TRIM("username")`},
		{"Length", orm.Length(Users.Username), `LENGTH("username")`},
		{"Abs", orm.Abs(Users.Age.Value()), `ABS("age")`},
		{"Round", orm.Round(Users.Age.Value(), 2), `ROUND("age", CAST($1 AS INTEGER))`},
		{"Coalesce", orm.Coalesce[string](Users.Username, "unknown"),
			`COALESCE("username", CAST($1 AS TEXT))`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := orm.SelectAs[oneValue](Users.With(pg()), tt.expr).SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if !strings.HasPrefix(sql, "SELECT "+tt.want+" FROM") {
				t.Errorf("SQL() = %s\nwant it to select %s", sql, tt.want)
			}
		})
	}
}

// oneValue reads a single projected expression whatever its type, so the
// helper table above needs no struct per case.
type oneValue struct{ V any }

// Coalescing a nullable column with a fallback reads back as an ordinary
// value: the result cannot be NULL, and its declared type says so.
func TestFn_CoalesceOverColumns(t *testing.T) {
	type row struct{ Name string }
	sql, _, err := orm.SelectAs[row](Users.With(pg()),
		orm.Coalesce[string](Users.Email, Users.Username)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `COALESCE("email", "username")`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// A nullable text column satisfies the helpers' text requirement: *string is
// a string that may be absent, not a different kind of value.
func TestFn_NullableTextArgument(t *testing.T) {
	type row struct{ Name string }
	if _, _, err := orm.SelectAs[row](Users.With(pg()), orm.Lower(Users.Email)).SQL(); err != nil {
		t.Errorf("SQL() error = %v, want a nullable text column accepted", err)
	}
}
