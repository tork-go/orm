package query_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

func TestFullText_Matches(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		sql, args, err := Users.With(db).Where(Users.Username.Matches("golang orm")).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE to_tsvector("username") @@ websearch_to_tsquery($1)`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
		if len(args) != 1 || args[0] != "golang orm" {
			t.Errorf("bound %v, want the query as a parameter", args)
		}
	})

	t.Run("fake", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), fakedriver.NewDialect())
		sql, _, err := Users.With(db).Where(Users.Username.Matches("golang orm")).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE SEARCH([username], ?)`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
	})
}

// It is on nullable string columns too, since textOps serves both.
func TestFullText_NullableColumn(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	sql, _, err := Users.With(db).Where(Users.Email.Matches("alice")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `WHERE to_tsvector("email") @@ websearch_to_tsquery($1)`; !strings.HasSuffix(sql, want) {
		t.Errorf("compiled %s, want it to end %s", sql, want)
	}
}

// The query is a parameter, whatever it contains, so nothing in it is written
// into the statement.
func TestFullText_QueryIsAParameter(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	danger := "'; DROP TABLE users; --"
	sql, args, err := Users.With(db).Where(Users.Username.Matches(danger)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(sql, "DROP TABLE") {
		t.Errorf("the query was written into the statement: %s", sql)
	}
	if len(args) != 1 || args[0] != danger {
		t.Errorf("bound %v, want the query carried as a parameter", args)
	}
}

// Being a predicate, it composes with the combinators and numbers in with the
// other placeholders.
func TestFullText_Composes(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	sql, args, err := Users.With(db).Where(
		Users.Age.Gt(18),
		orm.Or(Users.Username.Matches("go"), Users.Email.Matches("go")),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `WHERE ("age" > $1 AND (to_tsvector("username") @@ websearch_to_tsquery($2) ` +
		`OR to_tsvector("email") @@ websearch_to_tsquery($3)))`
	if !strings.HasSuffix(sql, want) {
		t.Errorf("compiled %s, want it to end %s", sql, want)
	}
	if len(args) != 3 || args[0] != 18 || args[1] != "go" || args[2] != "go" {
		t.Errorf("bound %v, want [18 go go]", args)
	}
}

// Naming another table's column is caught by the compiler, as for any
// predicate.
func TestFullText_ForeignColumnRejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, _, err := Users.With(db).Where(Posts.Title.Matches("x")).SQL()
	if err == nil || !strings.Contains(err.Error(), `belongs to table "posts"`) {
		t.Errorf("SQL() error = %v, want the foreign column rejected", err)
	}
}

// A database with no full-text search says so, naming the operation, rather
// than emitting SQL that does not run.
func TestFullText_UnsupportedByTheDialect(t *testing.T) {
	d := fakedriver.NewDialect()
	d.NoFullText = true
	db := orm.NewDB(fakedriver.NewConn(), d)

	_, _, err := Users.With(db).Where(Users.Username.Matches("go")).SQL()
	if err == nil || !strings.Contains(err.Error(), "full-text") {
		t.Errorf("SQL() error = %v, want it to name the unsupported operation", err)
	}
}
