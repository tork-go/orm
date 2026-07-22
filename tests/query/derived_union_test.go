package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// A derived table over the users' own shape, for wrapping a combined query
// or a plain read.
type NamedUser struct {
	ID       int
	Username string
}

type namedUserModel struct {
	orm.DerivedTable[NamedUser]
	ID       *orm.IntColumn
	Username *orm.StringColumn
}

var NamedUsers = orm.DefineDerived[NamedUser]("named_users",
	func(t *orm.TableBuilder[NamedUser]) *namedUserModel {
		return &namedUserModel{
			DerivedTable: t.Derived(),
			ID:           t.Int("id"),
			Username:     t.String("username"),
		}
	})

// Re-filtering a union, the third shape derived tables unlock: a combined
// result has no WHERE of its own to narrow.
func TestDerivedUnion_ReFiltersACombinedQuery(t *testing.T) {
	db := pg()
	young := Users.With(db).Select(Users.ID, Users.Username).Where(Users.Age.LessThan(20))
	old := Users.With(db).Select(Users.ID, Users.Username).Where(Users.Age.GreaterThan(60))

	sql, args, err := NamedUsers.From(orm.Union(young, old)).
		Where(NamedUsers.Username.StartsWith("a")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "username" FROM (` +
		`(SELECT "id" AS "id", "username" AS "username" FROM "users" WHERE "age" < $1) UNION ` +
		`(SELECT "id", "username" FROM "users" WHERE "age" > $2)` +
		`) AS "named_users" WHERE "username" LIKE $3 ESCAPE '\'`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 3 || args[0] != 20 || args[1] != 60 {
		t.Errorf("args = %v, want [20 60 a%%]", args)
	}
}

// Only the left operand is aliased: SQL takes a combined result's column
// names from its first operand, so naming them twice would say nothing.
func TestDerivedUnion_AliasesTheLeftOperandOnly(t *testing.T) {
	db := pg()
	a := Users.With(db).Select(Users.ID, Users.Username)
	b := Users.With(db).Select(Users.ID, Users.Username)

	sql, _, err := NamedUsers.From(orm.Union(a, b)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Count(sql, `AS "username"`) != 1 {
		t.Errorf("SQL() = %s\nwant the alias on the left operand only", sql)
	}
}

// A plain read is a source too, which is the simplest way to wrap a query
// so its result can be filtered again.
func TestDerivedUnion_PlainReadAsASource(t *testing.T) {
	db := pg()
	inner := Users.With(db).Select(Users.ID, Users.Username).Where(Users.Age.GreaterThan(18))

	sql, args, err := NamedUsers.From(inner).
		Where(NamedUsers.Username.Equals("alice")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "username" FROM (` +
		`SELECT "id" AS "id", "username" AS "username" FROM "users" WHERE "age" > $1` +
		`) AS "named_users" WHERE "username" = $2`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 2 || args[0] != 18 || args[1] != "alice" {
		t.Errorf("args = %v, want [18 alice]", args)
	}
}

func TestDerivedUnion_ShapeCheckedAgainstTheLeftOperand(t *testing.T) {
	db := pg()
	// One column where the derived table declares two.
	a := Users.With(db).Select(Users.ID)
	b := Users.With(db).Select(Users.ID)

	_, _, err := NamedUsers.From(orm.Union(a, b)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the shape mismatch rejected")
	}
	if !strings.Contains(err.Error(), "declares 2 column(s) but the source yields 1") {
		t.Errorf("error %q does not name the mismatch", err)
	}
}

// The combined query's own checks still run when it is a source: two
// operands of different widths is its own complaint, not the derived
// table's.
func TestDerivedUnion_CombinedOwnErrorSurfaces(t *testing.T) {
	db := pg()
	a := Users.With(db).Select(Users.ID, Users.Username)
	b := Users.With(db).Select(Users.ID)

	_, _, err := NamedUsers.From(orm.Union(a, b)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the operand mismatch rejected")
	}
	if !strings.Contains(err.Error(), "both sides of a Union") {
		t.Errorf("error %q is not the combined query's own", err)
	}
}

// A lock cannot survive being wrapped: no dialect locks rows through a
// derived table.
func TestDerivedUnion_LockRejected(t *testing.T) {
	db := pg()
	locked := Users.With(db).Select(Users.ID, Users.Username).
		Where(Users.ID.GreaterThan(0)).ForUpdate()

	_, _, err := NamedUsers.From(locked).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the lock rejected")
	}
	if !strings.Contains(err.Error(), "ForUpdate") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// A Preload runs a query of its own against rows the wrapping statement
// never has on their own.
func TestDerivedUnion_PreloadRejected(t *testing.T) {
	db := pg()
	type Wrapped struct {
		ID   int
		Name string
	}
	type wrappedModel struct {
		orm.DerivedTable[Wrapped]
		ID   *orm.IntColumn
		Name *orm.StringColumn
	}
	w := orm.DefineDerived[Wrapped]("wrapped_authors",
		func(t *orm.TableBuilder[Wrapped]) *wrappedModel {
			return &wrappedModel{
				DerivedTable: t.Derived(),
				ID:           t.Int("id"),
				Name:         t.String("name"),
			}
		})

	_, _, err := w.From(Authors.With(db).Load(Authors.Books)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the Preload rejected")
	}
	if !strings.Contains(err.Error(), "Preload") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// A source carrying a CTE brings its WITH clause inside the parentheses,
// where it still governs the subquery it belongs to.
func TestDerivedUnion_SourceCarryingACTE(t *testing.T) {
	db := pg()
	inner := Authors.With(db).
		With("book_authors", authorIDs(db)).
		Where(Authors.ID.InQuery(orm.CTE[int]("book_authors"))).
		Select(Authors.ID, Authors.Name)

	type Wrapped struct {
		ID   int
		Name string
	}
	type wrappedModel struct {
		orm.DerivedTable[Wrapped]
		ID   *orm.IntColumn
		Name *orm.StringColumn
	}
	w := orm.DefineDerived[Wrapped]("cte_wrapped",
		func(t *orm.TableBuilder[Wrapped]) *wrappedModel {
			return &wrappedModel{
				DerivedTable: t.Derived(),
				ID:           t.Int("id"),
				Name:         t.String("name"),
			}
		})

	sql, _, err := w.From(inner).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `FROM (WITH "book_authors" AS (`) {
		t.Errorf("SQL() = %s, want the WITH clause inside the subquery", sql)
	}
}

// A combined query built from a nil operand has no left side to take a
// handle or a shape from, and reports its own complaint rather than
// tripping over the absence.
func TestDerivedUnion_NilOperand(t *testing.T) {
	a := Users.With(pg()).Select(Users.ID, Users.Username)
	_, _, err := NamedUsers.From(orm.Union[User](a, nil)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the nil operand rejected")
	}
	if !strings.Contains(err.Error(), "nil query") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// Whatever stops a source compiling is reported as the source's own
// problem, at each stage From renders it through.
func TestDerivedUnion_SourceErrorsSurface(t *testing.T) {
	db := pg()

	type legacyModel struct {
		orm.Table[orm.NoEntity]
		ID       *orm.IntColumn
		Username *orm.StringColumn
	}
	legacy := &legacyModel{
		Table:    orm.NewTable[orm.NoEntity]("legacy"),
		ID:       orm.NewIntColumn("id"),
		Username: orm.NewStringColumn("username"),
	}

	var nilCTE *orm.Scalars[int]

	tests := map[string]struct {
		src  orm.DerivedSource
		want string
	}{
		"no entity mapping": {
			legacy.With(db).Select(legacy.ID, legacy.Username),
			"legacy",
		},
		"a join it cannot make": {
			Books.With(db).Join(Books.Tags).Select(Books.ID, Books.Title),
			"many to many",
		},
		"a CTE that cannot compile": {
			Users.With(db).With("x", nilCTE).Select(Users.ID, Users.Username),
			"subquery is nil",
		},
		"an ordering it cannot render": {
			Users.With(db).Select(Users.ID, Users.Username).OrderBy(Books.Title.Asc()),
			`belongs to table "books"`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := NamedUsers.From(tt.src).SQL()
			if err == nil {
				t.Fatal("SQL() error = nil, want the source's own error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

// It runs, not merely compiles.
func TestDerivedUnion_All(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "alice"}, []any{2, "bob"})
	db := orm.NewDB(c, postgres.Dialect{})

	a := Users.With(db).Select(Users.ID, Users.Username).Where(Users.Age.LessThan(20))
	b := Users.With(db).Select(Users.ID, Users.Username).Where(Users.Age.GreaterThan(60))

	rows, err := NamedUsers.From(orm.Union(a, b)).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(rows) != 2 || rows[0].Username != "alice" || rows[1].ID != 2 {
		t.Errorf("rows = %+v, want [{1 alice} {2 bob}]", rows)
	}
}
