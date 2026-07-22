package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// The motivating case: a subquery that refers back to the row it is being
// read for, which is what "correlated" means, and which needs the
// column-against-column comparison expressions brought.
func TestScalarSubquery_Correlated(t *testing.T) {
	db := pg()
	firstBook := orm.Select(
		Books.With(db).Where(Books.AuthorID.Value().Equals(Authors.ID)),
		Books.Title,
	)

	type report struct {
		Name  string
		Title string
	}
	sql, _, err := orm.SelectAs[report](Authors.With(db), Authors.Name, firstBook).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "name", (SELECT "books"."title" FROM "books" ` +
		`WHERE "books"."author_id" = "authors"."id") FROM "authors"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// A subquery need not correlate; one that stands alone reads the same way.
func TestScalarSubquery_Uncorrelated(t *testing.T) {
	db := pg()
	anyTitle := orm.Select(Books.With(db).Where(Books.Title.Equals("Mort")), Books.Title)

	type report struct {
		Name  string
		Title string
	}
	sql, args, err := orm.SelectAs[report](Authors.With(db), Authors.Name, anyTitle).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "name", (SELECT "books"."title" FROM "books" ` +
		`WHERE "books"."title" = $1) FROM "authors"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != "Mort" {
		t.Errorf("args = %v, want [Mort]", args)
	}
}

// Placeholders number continuously across the boundary: the SELECT list is
// rendered before the WHERE, so the subquery's own value binds first.
func TestScalarSubquery_PlaceholdersContinue(t *testing.T) {
	db := pg()
	title := orm.Select(Books.With(db).Where(Books.Title.Equals("Mort")), Books.Title)

	type report struct {
		Name  string
		Title string
	}
	sql, args, err := orm.SelectAs[report](
		Authors.With(db).Where(Authors.Name.Equals("Pratchett")),
		Authors.Name, title,
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `"books"."title" = $1`) {
		t.Errorf("SQL() = %s, want the subquery's placeholder first", sql)
	}
	if !strings.Contains(sql, `WHERE "name" = $2`) {
		t.Errorf("SQL() = %s, want the outer condition's placeholder second", sql)
	}
	if len(args) != 2 || args[0] != "Mort" || args[1] != "Pratchett" {
		t.Errorf("args = %v, want [Mort Pratchett]", args)
	}
}

// The subquery's own ordering and limit come along, which is how "the most
// recent one" is expressed.
func TestScalarSubquery_CarriesItsOwnClauses(t *testing.T) {
	db := pg()
	latest := orm.Select(
		Books.With(db).
			Where(Books.AuthorID.Value().Equals(Authors.ID)).
			OrderBy(Books.ID.Desc()).
			Limit(1),
		Books.Title,
	)

	type report struct {
		Name  string
		Title string
	}
	sql, _, err := orm.SelectAs[report](Authors.With(db), Authors.Name, latest).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `ORDER BY "books"."id" DESC LIMIT 1)`) {
		t.Errorf("SQL() = %s, want the subquery's own ordering and limit", sql)
	}
}

// A subquery's Go type is its column's, so the projection's field is
// checked against it the same way a plain column's is.
func TestScalarSubquery_ProjectionTypeMismatch(t *testing.T) {
	db := pg()
	ids := orm.Select(Books.With(db), Books.ID) // *Scalars[int]

	type report struct {
		Name string
		ID   string // int subquery, string field
	}
	_, _, err := orm.SelectAs[report](Authors.With(db), Authors.Name, ids).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the field type mismatch rejected")
	}
	if !strings.Contains(err.Error(), "is string but expression 1 is int") {
		t.Errorf("error %q does not name the mismatch", err)
	}
}

// orm.NonNull is a subquery source too, so it reads as a projected column
// as well, bringing its own IS NOT NULL along.
func TestScalarSubquery_NonNull(t *testing.T) {
	db := pg()
	emails := orm.NonNull(orm.Select(Users.With(db), Users.Email))

	type report struct {
		Name  string
		Email string
	}
	sql, _, err := orm.SelectAs[report](Authors.With(db), Authors.Name, emails).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `"users"."email" IS NOT NULL`) {
		t.Errorf("SQL() = %s, want NonNull's own condition", sql)
	}
}

// A bad subquery is reported rather than rendered.
func TestScalarSubquery_ErrorSurfaces(t *testing.T) {
	db := pg()
	bad := orm.Select(Books.With(db).Where(Mistypeds.ID.Equals(1)), Books.Title)

	type report struct {
		Name  string
		Title string
	}
	_, _, err := orm.SelectAs[report](Authors.With(db), Authors.Name, bad).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign condition rejected")
	}
	if !strings.Contains(err.Error(), "belongs to table") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// It runs, not merely compiles, and the value lands in its own field.
func TestScalarSubquery_All(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"Pratchett", "Mort"})
	db := orm.NewDB(c, postgres.Dialect{})

	firstBook := orm.Select(
		Books.With(db).Where(Books.AuthorID.Value().Equals(Authors.ID)),
		Books.Title,
	)
	type report struct {
		Name  string
		Title string
	}
	rows, err := orm.SelectAs[report](Authors.With(db), Authors.Name, firstBook).
		All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "Pratchett" || rows[0].Title != "Mort" {
		t.Errorf("rows = %+v, want [{Pratchett Mort}]", rows)
	}
}
