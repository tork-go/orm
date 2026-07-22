package query_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
)

// A function's name is the one thing about a call this package cannot bind,
// since SQL has no parameter for it. It is therefore checked to be a name:
// an identifier, optionally qualified by a schema, and nothing else.
func TestFn_NameMustBeAnIdentifier(t *testing.T) {
	refused := []struct {
		name string
		why  string
	}{
		{"", "empty"},
		{" ", "a space"},
		{"lower name", "a space inside"},
		{"1lower", "a leading digit"},
		{"lo-wer", "a hyphen"},
		{"lower()", "parentheses"},
		{`lower"`, "a quote"},
		{"lower;DROP TABLE users", "a statement terminator"},
		{"lower(1) OR 1=1 --", "an injected condition"},
		{"pg_catalog..lower", "an empty schema part"},
		{"a.b.c", "two qualifiers"},
		{".lower", "a missing schema"},
		{"lower.", "a missing name"},
	}
	for _, r := range refused {
		t.Run(r.why, func(t *testing.T) {
			_, _, err := orm.SelectAs[oneValue](Users.With(pg()),
				orm.Fn[string](r.name, Users.Username)).SQL()
			if err == nil {
				t.Fatalf("SQL() error = nil, want %q refused for %s", r.name, r.why)
			}
			if !strings.Contains(err.Error(), "not a usable function name") {
				t.Errorf("error = %v, want it to name the problem", err)
			}
		})
	}
}

// The names that are legal, including the qualified form.
func TestFn_NameAccepted(t *testing.T) {
	for _, name := range []string{"lower", "LOWER", "_x", "x1", "date_trunc", "pg_catalog.lower"} {
		if _, _, err := orm.SelectAs[oneValue](Users.With(pg()),
			orm.Fn[string](name, Users.Username)).SQL(); err != nil {
			t.Errorf("Fn(%q): %v", name, err)
		}
	}
}

// A hostile name is refused; a hostile value is bound. Between them there is
// no way for a caller's own text to reach the statement as SQL.
func TestFn_HostileInputCannotReachTheStatement(t *testing.T) {
	const attack = `x'); DROP TABLE users; --`

	if _, _, err := orm.SelectAs[oneValue](Users.With(pg()),
		orm.Fn[string](attack, Users.Username)).SQL(); err == nil {
		t.Error("SQL() error = nil, want a hostile function name refused")
	}

	sql, args, err := orm.SelectAs[oneValue](Users.With(pg()),
		orm.Fn[string]("lower", attack)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(sql, "DROP TABLE") {
		t.Errorf("SQL() = %s, want the value bound rather than written", sql)
	}
	if len(args) != 1 || args[0] != attack {
		t.Errorf("args = %v, want the hostile value bound whole", args)
	}
}

// The text helpers say what they need, rather than leaving a column of the
// wrong type to the database.
func TestFn_TextHelperRejectsNonText(t *testing.T) {
	_, _, err := orm.SelectAs[oneValue](Users.With(pg()), orm.Lower(Users.Age)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want LOWER over an int column refused")
	}
	if !strings.Contains(err.Error(), "LOWER argument 0") || !strings.Contains(err.Error(), "int") {
		t.Errorf("error = %v, want it to name the function, the position and the type", err)
	}
}

func TestFn_LengthRejectsNonText(t *testing.T) {
	_, _, err := orm.SelectAs[oneValue](Users.With(pg()), orm.Length(Users.Age)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want LENGTH over an int column refused")
	}
	if !strings.Contains(err.Error(), "LENGTH argument 0") {
		t.Errorf("error = %v, want it to name the function and the position", err)
	}
}

// A value argument of the wrong type is caught the same way a column is.
func TestFn_TextHelperRejectsNonTextValue(t *testing.T) {
	_, _, err := orm.SelectAs[oneValue](Users.With(pg()), orm.Upper(42)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want UPPER over a number refused")
	}
	if !strings.Contains(err.Error(), "UPPER argument 0") {
		t.Errorf("error = %v, want it to name the function and the position", err)
	}
}

// A nil argument says nothing about what it was meant to be, so it is left
// to the database rather than guessed at here.
func TestFn_NilArgumentAccepted(t *testing.T) {
	if _, _, err := orm.SelectAs[oneValue](Users.With(pg()), orm.Lower(nil)).SQL(); err != nil {
		t.Errorf("SQL() error = %v, want a nil argument left alone", err)
	}
}

// A column of another table is rejected inside a call exactly as it is
// outside one: an argument renders through the same ownership check.
func TestFn_ForeignColumnRejected(t *testing.T) {
	_, _, err := orm.SelectAs[oneValue](Users.With(pg()), orm.Lower(Posts.Title)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign column refused")
	}
	if !strings.Contains(err.Error(), `belongs to table "posts"`) {
		t.Errorf("error = %v, want it to name the table", err)
	}
}

// DISTINCT narrows what an aggregate reads. An ordinary call has nothing for
// it to narrow, and says so.
func TestDistinct_OnANonAggregate(t *testing.T) {
	_, _, err := orm.SelectAs[oneValue](Users.With(pg()), orm.Lower(Users.Username).Distinct()).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want DISTINCT on a plain call refused")
	}
	if !strings.Contains(err.Error(), "not an aggregate") {
		t.Errorf("error = %v, want it to say what DISTINCT belongs to", err)
	}
}
