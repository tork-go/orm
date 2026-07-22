package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// A Raw fragment's ? becomes the dialect's own placeholder, and its value is
// bound as a parameter rather than written into the statement.
func TestRaw_BindsItsPlaceholder(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	sql, args, err := Users.With(db).Where(orm.Raw("lower(username) = ?", "alice")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `WHERE lower(username) = $1`; !strings.HasSuffix(sql, want) {
		t.Errorf("compiled %s, want it to end %s", sql, want)
	}
	if len(args) != 1 || args[0] != "alice" {
		t.Errorf("bound %v, want [alice]", args)
	}
}

// The placeholder is the dialect's, so the same fragment wears $1 on Postgres
// and ? on the fake. The SQL a caller wrote is untouched either way.
func TestRaw_AsksTheDialect(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), fakedriver.NewDialect())

	sql, args, err := Users.With(db).Where(orm.Raw("lower(username) = ?", "alice")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `WHERE lower(username) = ?`; !strings.HasSuffix(sql, want) {
		t.Errorf("compiled %s, want it to end %s", sql, want)
	}
	if len(args) != 1 || args[0] != "alice" {
		t.Errorf("bound %v, want [alice]", args)
	}
}

// Its placeholders are numbered in with the statement's other parameters, so a
// Raw fragment beside typed predicates counts on from where they left off.
func TestRaw_NumbersOnFromTypedPredicates(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	sql, args, err := Users.With(db).Where(
		Users.Age.GreaterThan(18),
		orm.Raw("lower(username) = ?", "alice"),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `WHERE ("age" > $1 AND lower(username) = $2)`; !strings.HasSuffix(sql, want) {
		t.Errorf("compiled %s, want it to end %s", sql, want)
	}
	if len(args) != 2 || args[0] != 18 || args[1] != "alice" {
		t.Errorf("bound %v, want [18 alice]", args)
	}
}

// Several placeholders in one fragment bind their arguments in order.
func TestRaw_SeveralPlaceholders(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	sql, args, err := Users.With(db).Where(orm.Raw("age BETWEEN ? AND ?", 18, 65)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `WHERE age BETWEEN $1 AND $2`; !strings.HasSuffix(sql, want) {
		t.Errorf("compiled %s, want it to end %s", sql, want)
	}
	if len(args) != 2 || args[0] != 18 || args[1] != 65 {
		t.Errorf("bound %v, want [18 65]", args)
	}
}

// A doubled ?? is an escaped literal question mark that binds nothing, which
// is how the jsonb ? operator is written beside real placeholders.
func TestRaw_EscapesADoubledQuestionMark(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	sql, args, err := Users.With(db).Where(orm.Raw(`"prefs" ?? ?`, "theme")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `WHERE "prefs" ? $1`; !strings.HasSuffix(sql, want) {
		t.Errorf("compiled %s, want it to end %s", sql, want)
	}
	if len(args) != 1 || args[0] != "theme" {
		t.Errorf("bound %v, want [theme]", args)
	}
}

// A fragment with no placeholders and no arguments renders verbatim.
func TestRaw_NoPlaceholders(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	sql, args, err := Users.With(db).Where(orm.Raw("username IS NOT NULL")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `WHERE username IS NOT NULL`; !strings.HasSuffix(sql, want) {
		t.Errorf("compiled %s, want it to end %s", sql, want)
	}
	if len(args) != 0 {
		t.Errorf("bound %v, want nothing", args)
	}
}

// More placeholders than arguments is reported when the statement compiles,
// naming the fragment.
func TestRaw_TooFewArguments(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	_, _, err := Users.With(db).Where(orm.Raw("a = ? AND b = ?", 1)).SQL()
	if err == nil || !strings.Contains(err.Error(), "more ? placeholders") {
		t.Errorf("SQL() error = %v, want it to name the placeholder shortfall", err)
	}
}

// More arguments than placeholders is reported the same way.
func TestRaw_TooManyArguments(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	_, _, err := Users.With(db).Where(orm.Raw("a = ?", 1, 2)).SQL()
	if err == nil || !strings.Contains(err.Error(), "argument(s) given") {
		t.Errorf("SQL() error = %v, want it to name the surplus argument", err)
	}
}

// A value is bound, never written into the statement, so a fragment is as
// injection-safe as a typed predicate: the SQL text is the caller's, the
// value is a parameter.
func TestRaw_ValueIsAParameterNotText(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	danger := "x'; DROP TABLE users; --"
	sql, args, err := Users.With(db).Where(orm.Raw("username = ?", danger)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(sql, "DROP TABLE") {
		t.Errorf("the value was written into the statement: %s", sql)
	}
	if len(args) != 1 || args[0] != danger {
		t.Errorf("bound %v, want the value carried as a parameter", args)
	}
}

// Being an ordinary predicate, it composes with the combinators.
func TestRaw_ComposesWithOrAndNot(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	t.Run("inside Or", func(t *testing.T) {
		sql, _, err := Users.With(db).Where(
			orm.Or(Users.Age.LessThan(18), orm.Raw("lower(username) = ?", "alice")),
		).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE ("age" < $1 OR lower(username) = $2)`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
	})

	t.Run("inside Not", func(t *testing.T) {
		sql, _, err := Users.With(db).Where(orm.Not(orm.Raw("age = ?", 30))).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE NOT (age = $1)`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
	})
}

// It is a predicate, so it works as a condition on a conditional write, its
// placeholder numbered in after the key.
func TestRaw_UsableInUpdateIf(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if err := Users.With(db).UpdateIf(context.Background(),
		&User{ID: 1}, orm.Raw("age = ?", 30)); err != nil {
		t.Fatalf("UpdateIf() error = %v", err)
	}
	if got := c.ExecCalls()[0]; !strings.HasSuffix(got, `WHERE ("id" = $6 AND age = $7)`) {
		t.Errorf("ran %s, want the raw condition joined after the key", got)
	}
	if args := c.ExecArgs(0); len(args) != 7 || args[6] != 30 {
		t.Errorf("bound %v, want the raw value last", args)
	}
}
