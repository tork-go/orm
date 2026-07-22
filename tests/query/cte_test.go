package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

func TestWith_Renders(t *testing.T) {
	db := pg()
	sql, args, err := Authors.With(db).
		With("book_authors", authorIDs(db)).
		Where(Authors.ID.InQuery(orm.CTE[int]("book_authors"))).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `WITH "book_authors" AS (SELECT "books"."author_id" FROM "books" WHERE "books"."title" = $1) ` +
		`SELECT "id", "name" FROM "authors" WHERE "id" IN (SELECT * FROM "book_authors")`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != "Mort" {
		t.Errorf("args = %v, want [Mort]", args)
	}
}

// The CTE's own placeholder is textually first in the finished statement,
// so it must be $1 even though the outer condition that reads it back was
// not the first Where call.
func TestWith_PlaceholderNumberingCTEFirst(t *testing.T) {
	db := pg()
	sql, args, err := Authors.With(db).
		With("book_authors", authorIDs(db)).
		Where(Authors.Name.Equals("Pratchett"), Authors.ID.InQuery(orm.CTE[int]("book_authors"))).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `"books"."title" = $1`) {
		t.Errorf("SQL() = %s, want the CTE's own placeholder numbered $1", sql)
	}
	if !strings.Contains(sql, `"name" = $2`) {
		t.Errorf("SQL() = %s, want the outer condition's placeholder numbered $2", sql)
	}
	if len(args) != 2 || args[0] != "Mort" || args[1] != "Pratchett" {
		t.Errorf("args = %v, want [Mort Pratchett]", args)
	}
}

// Calls accumulate, each rendered in the order With was called.
func TestWith_MultipleCTEsAccumulate(t *testing.T) {
	db := pg()
	other := orm.Select(Books.With(db).Where(Books.Title.Equals("Sourcery")), Books.AuthorID)
	sql, _, err := Authors.With(db).
		With("a", authorIDs(db)).
		With("b", other).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasPrefix(sql, `WITH "a" AS (`) {
		t.Errorf("SQL() = %s, want to start with the first CTE", sql)
	}
	idxA := strings.Index(sql, `"a" AS`)
	idxB := strings.Index(sql, `"b" AS`)
	if idxA == -1 || idxB == -1 || idxA > idxB {
		t.Errorf("SQL() = %s, want \"a\" rendered before \"b\"", sql)
	}
}

func TestWith_EmptyNameRejected(t *testing.T) {
	db := pg()
	_, _, err := Authors.With(db).With("", authorIDs(db)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the empty name rejected")
	}
	if !strings.Contains(err.Error(), "empty name") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestWith_NilSourceRejected(t *testing.T) {
	db := pg()
	_, _, err := Authors.With(db).With("x", nil).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the nil source rejected")
	}
	if !strings.Contains(err.Error(), "no query") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// A nil *Scalars[T] passed as an interface value is not a nil interface, so
// With's own check does not catch it; Scalars.compileWithin's own nil
// receiver check does, the same way it already does for InQuery.
func TestWith_NilTypedSourceRejected(t *testing.T) {
	db := pg()
	var nilSource *orm.Scalars[int]
	_, _, err := Authors.With(db).With("x", nilSource).
		Where(Authors.ID.InQuery(orm.CTE[int]("x"))).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the nil subquery rejected")
	}
	if !strings.Contains(err.Error(), "subquery is nil") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// CTE is constructible independently of With, so an empty name reaches
// compileWithin directly, not only through With's own check.
func TestCTE_EmptyNameRejected(t *testing.T) {
	db := pg()
	_, _, err := Authors.With(db).
		With("book_authors", authorIDs(db)).
		Where(Authors.ID.InQuery(orm.CTE[int](""))).
		SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the empty CTE name rejected")
	}
	if !strings.Contains(err.Error(), "empty name") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestWith_CountRendersClause(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{int64(2)})
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := Authors.With(db).
		With("book_authors", authorIDs(db)).
		Where(Authors.ID.InQuery(orm.CTE[int]("book_authors"))).
		Count(context.Background())
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if n != 2 {
		t.Errorf("Count() = %d, want 2", n)
	}
	if got := c.QueryCalls()[0]; !strings.HasPrefix(got, `WITH "book_authors" AS (`) {
		t.Errorf("Count ran %s, want it prefixed with the WITH clause", got)
	}
}

// Count builds its own compiler independently of the other terminals and
// surfaces a bad CTE source the same way SQL/All do.
func TestWith_CountRejectsBadSource(t *testing.T) {
	db := pg()
	var nilSource *orm.Scalars[int]
	_, err := Authors.With(db).With("x", nilSource).Count(context.Background())
	if err == nil {
		t.Fatal("Count() error = nil, want the bad CTE source rejected")
	}
	if !strings.Contains(err.Error(), "subquery is nil") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestWith_All(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "Pratchett"})
	db := orm.NewDB(c, postgres.Dialect{})

	rows, err := Authors.With(db).
		With("book_authors", authorIDs(db)).
		Where(Authors.ID.InQuery(orm.CTE[int]("book_authors"))).
		All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "Pratchett" {
		t.Errorf("rows = %+v, want [{1 Pratchett}]", rows)
	}
}

func TestWith_SelectRejected(t *testing.T) {
	db := pg()
	_, _, err := orm.Select(
		Authors.With(db).With("book_authors", authorIDs(db)), Authors.Name,
	).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the With rejected")
	}
	if !strings.Contains(err.Error(), "With") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestWith_CountByRejected(t *testing.T) {
	db := pg()
	_, _, err := orm.CountBy(
		Authors.With(db).With("book_authors", authorIDs(db)), Authors.Name,
	).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the With rejected")
	}
	if !strings.Contains(err.Error(), "With") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestWith_SumRejected(t *testing.T) {
	db := pg()
	_, err := orm.Sum(context.Background(),
		Users.With(db).With("x", orm.Select(Users.With(db), Users.ID)), Users.Age)
	if err == nil {
		t.Fatal("Sum() error = nil, want the With rejected")
	}
	if !strings.Contains(err.Error(), "With") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestWith_SelectAsRejected(t *testing.T) {
	db := pg()
	type report struct{ Name string }
	_, _, err := orm.SelectAs[report](
		Authors.With(db).With("book_authors", authorIDs(db)), Authors.Name,
	).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the With rejected")
	}
	if !strings.Contains(err.Error(), "With") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestWith_UnionRejected(t *testing.T) {
	db := pg()
	a := Authors.With(db).With("book_authors", authorIDs(db))
	b := Authors.With(db).Where()
	_, _, err := orm.Union(a, b).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the With rejected")
	}
	if !strings.Contains(err.Error(), "With") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestWith_UpdateAllRejected(t *testing.T) {
	db := pg()
	_, err := Authors.With(db).
		With("book_authors", authorIDs(db)).
		UpdateAll(context.Background(), Authors.Name.Set("x"))
	if err == nil {
		t.Fatal("UpdateAll() error = nil, want the With rejected")
	}
	if !strings.Contains(err.Error(), "a With") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestWith_DeleteAllRejected(t *testing.T) {
	db := pg()
	_, err := Authors.With(db).
		With("book_authors", authorIDs(db)).
		DeleteAll(context.Background())
	if err == nil {
		t.Fatal("DeleteAll() error = nil, want the With rejected")
	}
	if !strings.Contains(err.Error(), "a With") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// With clones rather than narrows in place, the same as every other
// builder in the package.
func TestWith_LeavesOriginalAlone(t *testing.T) {
	db := pg()
	base := Authors.With(db).Where()
	want, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}

	narrowed := base.With("book_authors", authorIDs(db))
	narrowedSQL, _, err := narrowed.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if narrowedSQL == want {
		t.Error("With did not narrow anything")
	}

	got, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if got != want {
		t.Errorf("With changed the query it was called on:\n got %s\nwant %s", got, want)
	}
}

// Query.With is Filtered.With, starting from an unfiltered query.
func TestWith_QueryForwarder(t *testing.T) {
	db := pg()
	sql, _, err := Authors.With(db).
		With("book_authors", authorIDs(db)).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasPrefix(sql, `WITH "book_authors" AS (`) {
		t.Errorf("SQL() = %s, want it prefixed with the WITH clause", sql)
	}
}
