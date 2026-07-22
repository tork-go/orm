package query_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// A row with a nullable JSON column, which the Users fixture has no equivalent
// of, so the nullable mixin's Contains (which takes the address of its value)
// has somewhere to be exercised.
type jsonRow struct {
	ID   int
	Meta *Prefs
}

type jsonRowModel struct {
	orm.Table[jsonRow]
	ID   *orm.IntColumn
	Meta *orm.NullableJSONColumn[Prefs]
}

var jsonRows = orm.DefineTable[jsonRow]("json_rows", func(t *orm.TableBuilder[jsonRow]) *jsonRowModel {
	return &jsonRowModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Meta:  orm.NewNullableJSONColumn[Prefs]("meta"),
	}
})

func TestJSON_HasKey(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		sql, args, err := Users.With(db).Where(Users.Prefs.HasKey("theme")).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE "prefs" ? $1`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
		if len(args) != 1 || args[0] != "theme" {
			t.Errorf("bound %v, want [theme]", args)
		}
	})

	t.Run("fake", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), fakedriver.NewDialect())
		sql, _, err := Users.With(db).Where(Users.Prefs.HasKey("theme")).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE HASKEY([prefs], ?)`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
	})
}

func TestJSON_Contains(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		sql, args, err := Users.With(db).Where(Users.Prefs.Contains(Prefs{Theme: "dark"})).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE "prefs" @> $1::jsonb`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
		// The value is encoded through the column's codec and bound as text, so
		// the ::jsonb cast has something to cast.
		if len(args) != 1 || args[0] != `{"theme":"dark"}` {
			t.Errorf("bound %v, want the JSON text of the value", args)
		}
	})

	t.Run("fake", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), fakedriver.NewDialect())
		sql, _, err := Users.With(db).Where(Users.Prefs.Contains(Prefs{Theme: "dark"})).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE CONTAINS([prefs], ?)`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
	})
}

func TestJSON_Key(t *testing.T) {
	t.Run("postgres eq", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		sql, args, err := Users.With(db).Where(Users.Prefs.Key("theme").Eq("dark")).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE ("prefs" ->> $1) = $2`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
		if len(args) != 2 || args[0] != "theme" || args[1] != "dark" {
			t.Errorf("bound %v, want [theme dark] in that order", args)
		}
	})

	t.Run("postgres noteq", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		sql, _, err := Users.With(db).Where(Users.Prefs.Key("theme").NotEq("dark")).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE ("prefs" ->> $1) <> $2`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
	})

	t.Run("fake", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), fakedriver.NewDialect())
		sql, _, err := Users.With(db).Where(Users.Prefs.Key("theme").Eq("dark")).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE GET([prefs], ?) = ?`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
	})
}

// The nullable column's operations render the same, and its Contains encodes
// the value the same, through the *T codec its address satisfies.
func TestJSON_NullableColumn(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	t.Run("has key", func(t *testing.T) {
		sql, _, err := jsonRows.With(db).Where(jsonRows.Meta.HasKey("theme")).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE "meta" ? $1`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
	})

	t.Run("contains", func(t *testing.T) {
		sql, args, err := jsonRows.With(db).Where(jsonRows.Meta.Contains(Prefs{Theme: "dark"})).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE "meta" @> $1::jsonb`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
		if len(args) != 1 || args[0] != `{"theme":"dark"}` {
			t.Errorf("bound %v, want the JSON text of the value", args)
		}
	})

	t.Run("key", func(t *testing.T) {
		sql, _, err := jsonRows.With(db).Where(jsonRows.Meta.Key("theme").Eq("dark")).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if want := `WHERE ("meta" ->> $1) = $2`; !strings.HasSuffix(sql, want) {
			t.Errorf("compiled %s, want it to end %s", sql, want)
		}
	})
}

// The JSON operations number in with the statement's other placeholders, like
// any predicate.
func TestJSON_NumbersWithOtherPredicates(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	sql, args, err := Users.With(db).Where(
		Users.Age.Gt(18),
		Users.Prefs.Key("theme").Eq("dark"),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `WHERE ("age" > $1 AND ("prefs" ->> $2) = $3)`; !strings.HasSuffix(sql, want) {
		t.Errorf("compiled %s, want it to end %s", sql, want)
	}
	if len(args) != 3 || args[0] != 18 || args[1] != "theme" || args[2] != "dark" {
		t.Errorf("bound %v, want [18 theme dark]", args)
	}
}

// Naming another table's JSON column is caught by the compiler, like it is
// for any predicate, rather than compiling into a reference the statement
// cannot resolve.
func TestJSON_ForeignColumnIsRejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	tests := map[string]orm.Predicate{
		"HasKey":   jsonRows.Meta.HasKey("theme"),
		"Contains": jsonRows.Meta.Contains(Prefs{Theme: "dark"}),
		"Key":      jsonRows.Meta.Key("theme").Eq("dark"),
	}
	for name, pred := range tests {
		t.Run(name, func(t *testing.T) {
			// The column belongs to json_rows, not users.
			_, _, err := Users.With(db).Where(pred).SQL()
			if err == nil || !strings.Contains(err.Error(), `belongs to table "json_rows"`) {
				t.Errorf("SQL() error = %v, want the foreign column rejected", err)
			}
		})
	}
}

// A Contains value the column's codec cannot encode is reported rather than
// passed to the driver. No typed call can produce one, so the predicate is
// built directly, the way the exported fields allow.
func TestJSON_ContainsValueIsEncoded(t *testing.T) {
	_, _, err := Users.With(pg()).Where(orm.JSONContains{
		Col:   Users.Prefs,
		Value: "not a Prefs",
	}).SQL()
	if err == nil || !strings.Contains(err.Error(), `column "prefs"`) {
		t.Errorf("SQL() error = %v, want an encode failure naming the column", err)
	}
}

// A dialect that cannot query inside a document says so, naming the operation,
// rather than emitting SQL that does not run.
func TestJSON_UnsupportedByTheDialect(t *testing.T) {
	d := fakedriver.NewDialect()
	d.NoJSON = true
	db := orm.NewDB(fakedriver.NewConn(), d)

	tests := map[string]orm.Predicate{
		"HasKey":   Users.Prefs.HasKey("theme"),
		"Contains": Users.Prefs.Contains(Prefs{Theme: "dark"}),
		"Key":      Users.Prefs.Key("theme").Eq("dark"),
	}
	for name, pred := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := Users.With(db).Where(pred).SQL()
			if err == nil || !strings.Contains(err.Error(), "JSON document") {
				t.Errorf("SQL() error = %v, want it to name the unsupported operation", err)
			}
		})
	}
}
