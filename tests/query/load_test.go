package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// author and book build the queued rows in each table's column order.
func author(id int, name string) []any { return []any{id, name} }
func book(id, authorID int, title string) []any {
	return []any{id, authorID, title}
}

// The parents come back in one statement and every child in a second, so the
// count is fixed however many parents there are.
func TestLoad_HasMany(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(author(1, "alice"), author(2, "bob"))
	c.QueueRows(book(10, 1, "first"), book(11, 2, "second"), book(12, 1, "third"))
	db := orm.NewDB(c, postgres.Dialect{})

	authors, err := Authors.With(db).Load(Authors.Books).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(authors) != 2 {
		t.Fatalf("All() returned %d rows, want 2", len(authors))
	}

	calls := c.QueryCalls()
	if len(calls) != 2 {
		t.Fatalf("ran %d statements, want 2:\n%v", len(calls), calls)
	}
	if want := `SELECT "id", "name" FROM "authors"`; calls[0] != want {
		t.Errorf("parents ran %s, want %s", calls[0], want)
	}
	want := `SELECT "id", "author_id", "title" FROM "books" WHERE "author_id" IN ($1, $2)`
	if calls[1] != want {
		t.Errorf("children ran  %s\nwant          %s", calls[1], want)
	}
	if args := c.QueryArgs(1); len(args) != 2 || args[0] != 1 || args[1] != 2 {
		t.Errorf("children bound %v, want the parents' keys", args)
	}

	// Each author got their own books, in the order they came back.
	if len(authors[0].Books) != 2 {
		t.Fatalf("alice has %d books, want 2", len(authors[0].Books))
	}
	if authors[0].Books[0].Title != "first" || authors[0].Books[1].Title != "third" {
		t.Errorf("alice's books = %+v", authors[0].Books)
	}
	if len(authors[1].Books) != 1 || authors[1].Books[0].Title != "second" {
		t.Errorf("bob's books = %+v", authors[1].Books)
	}
}

func TestLoad_BelongsTo(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(book(10, 1, "first"), book(11, 1, "second"))
	c.QueueRows(author(1, "alice"))
	db := orm.NewDB(c, postgres.Dialect{})

	books, err := Books.With(db).Load(Books.Author).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}

	// Two books share one author, so the author is asked for once and both
	// books get it.
	if args := c.QueryArgs(1); len(args) != 1 || args[0] != 1 {
		t.Errorf("bound %v, want the one distinct key", args)
	}
	for i, b := range books {
		if b.Author == nil {
			t.Fatalf("book %d has no author", i)
		}
		if b.Author.Name != "alice" {
			t.Errorf("book %d author = %q, want alice", i, b.Author.Name)
		}
	}
	// The rows are copies, so one book's author is not the other's.
	if books[0].Author == books[1].Author {
		t.Error("both books point at one value; a change through one would be seen by the other")
	}
}

func TestLoad_HasOne(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(author(1, "alice"), author(2, "bob"))
	c.QueueRows([]any{5, 1, "oak"})
	db := orm.NewDB(c, postgres.Dialect{})

	authors, err := Authors.With(db).Load(Authors.Desk).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if authors[0].Desk == nil || authors[0].Desk.Colour != "oak" {
		t.Errorf("alice's desk = %+v, want the oak one", authors[0].Desk)
	}
	// Nothing came back for bob, so the field stays nil rather than becoming
	// a zero value that reads as a real desk.
	if authors[1].Desk != nil {
		t.Errorf("bob's desk = %+v, want nil", authors[1].Desk)
	}
}

// A many to many runs two statements rather than a join: the join table's
// pairs, then the rows those pairs name.
func TestLoad_ManyToMany(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(book(10, 1, "first"), book(11, 1, "second"))
	c.QueueRows([]any{10, 100}, []any{10, 101}, []any{11, 100})
	c.QueueRows([]any{100, "go"}, []any{101, "sql"})
	db := orm.NewDB(c, postgres.Dialect{})

	books, err := Books.With(db).Load(Books.Tags).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}

	calls := c.QueryCalls()
	if len(calls) != 3 {
		t.Fatalf("ran %d statements, want 3:\n%v", len(calls), calls)
	}
	wantPairs := `SELECT "book_id", "tag_id" FROM "book_tags" WHERE "book_id" IN ($1, $2)`
	if calls[1] != wantPairs {
		t.Errorf("pairs ran  %s\nwant       %s", calls[1], wantPairs)
	}
	wantTags := `SELECT "id", "name" FROM "tags" WHERE "id" IN ($1, $2)`
	if calls[2] != wantTags {
		t.Errorf("tags ran  %s\nwant      %s", calls[2], wantTags)
	}

	if len(books[0].Tags) != 2 {
		t.Fatalf("first book has %d tags, want 2: %+v", len(books[0].Tags), books[0].Tags)
	}
	if books[0].Tags[0].Name != "go" || books[0].Tags[1].Name != "sql" {
		t.Errorf("first book's tags = %+v", books[0].Tags)
	}
	// The tag both books share was fetched once and given to both.
	if len(books[1].Tags) != 1 || books[1].Tags[0].Name != "go" {
		t.Errorf("second book's tags = %+v", books[1].Tags)
	}
}

func TestLoad_Nested(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(author(1, "alice"))
	c.QueueRows(book(10, 1, "first"))
	c.QueueRows([]any{10, 100})
	c.QueueRows([]any{100, "go"})
	db := orm.NewDB(c, postgres.Dialect{})

	authors, err := Authors.With(db).
		Load(Authors.Books.Load(Books.Tags)).
		All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(c.QueryCalls()) != 4 {
		t.Fatalf("ran %d statements, want 4:\n%v", len(c.QueryCalls()), c.QueryCalls())
	}
	if len(authors[0].Books) != 1 {
		t.Fatalf("alice has %d books", len(authors[0].Books))
	}
	if len(authors[0].Books[0].Tags) != 1 || authors[0].Books[0].Tags[0].Name != "go" {
		t.Errorf("the nested load did not reach the books: %+v", authors[0].Books[0].Tags)
	}
}

func TestLoad_NarrowedAndSorted(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(author(1, "alice"))
	c.QueueRows(book(10, 1, "first"))
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := Authors.With(db).Load(
		Authors.Books.Where(Books.Title.StartsWith("f")).OrderBy(Books.Title.Desc()),
	).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	want := `SELECT "id", "author_id", "title" FROM "books" ` +
		`WHERE ("author_id" IN ($1) AND "title" LIKE $2 ESCAPE '\') ORDER BY "title" DESC`
	if got := c.QueryCalls()[1]; got != want {
		t.Errorf("children ran  %s\nwant          %s", got, want)
	}
}

// A limit is per parent, which one statement cannot express, so a limited
// load runs one for each.
func TestLoad_LimitIsPerParent(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(author(1, "alice"), author(2, "bob"))
	c.QueueRows(book(10, 1, "a"))
	c.QueueRows(book(11, 2, "b"))
	db := orm.NewDB(c, postgres.Dialect{})

	authors, err := Authors.With(db).Load(Authors.Books.Limit(1)).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	calls := c.QueryCalls()
	if len(calls) != 3 {
		t.Fatalf("ran %d statements, want one per parent plus the parents:\n%v",
			len(calls), calls)
	}
	for _, i := range []int{1, 2} {
		if !strings.HasSuffix(calls[i], "LIMIT 1") {
			t.Errorf("statement %d = %s, want it capped", i, calls[i])
		}
		if args := c.QueryArgs(i); len(args) != 1 {
			t.Errorf("statement %d bound %v, want one parent's key", i, args)
		}
	}
	if len(authors[0].Books) != 1 || len(authors[1].Books) != 1 {
		t.Errorf("each author should have one book: %+v %+v", authors[0].Books, authors[1].Books)
	}
}

// The keys are values a statement binds, so a batch of parents past the
// driver's ceiling is split like any other.
func TestLoad_ChunksTheParentKeys(t *testing.T) {
	c := fakedriver.NewConn()
	parents := make([][]any, 5)
	for i := range parents {
		parents[i] = author(i+1, "a")
	}
	c.QueueRows(parents...)
	c.QueueRows(book(10, 1, "x"))
	c.QueueRows(book(11, 4, "y"))

	d := fakedriver.NewDialect()
	d.BindLimit = 3 // one key each, so three parents a statement
	db := orm.NewDB(c, d)

	if _, err := Authors.With(db).Load(Authors.Books).All(context.Background()); err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(c.QueryCalls()) != 3 {
		t.Fatalf("ran %d statements, want the children split in two:\n%v",
			len(c.QueryCalls()), c.QueryCalls())
	}
	if n := len(c.QueryArgs(1)); n != 3 {
		t.Errorf("the first chunk bound %d keys, want 3", n)
	}
	if n := len(c.QueryArgs(2)); n != 2 {
		t.Errorf("the second chunk bound %d keys, want 2", n)
	}
}

// Two parents with one key ask for the rows once and both get them.
func TestLoad_SharedKeyIsFetchedOnce(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(book(10, 1, "first"), book(11, 1, "second"), book(12, 1, "third"))
	c.QueueRows(author(1, "alice"))
	db := orm.NewDB(c, postgres.Dialect{})

	books, err := Books.With(db).Load(Books.Author).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if args := c.QueryArgs(1); len(args) != 1 {
		t.Errorf("bound %v, want the one distinct key", args)
	}
	for i, b := range books {
		if b.Author == nil {
			t.Errorf("book %d has no author", i)
		}
	}
}

// Nothing to load from means nothing to load: no second statement.
func TestLoad_NoParentsRunsNoSecondStatement(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	authors, err := Authors.With(db).Load(Authors.Books).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(authors) != 0 {
		t.Fatalf("All() returned %d rows, want none", len(authors))
	}
	if len(c.QueryCalls()) != 1 {
		t.Errorf("ran %d statements, want only the parents'", len(c.QueryCalls()))
	}
}

// First reads one row and loads for it, so a relationship is as available
// there as it is from All.
func TestLoad_AppliesToFirst(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(author(1, "alice"))
	c.QueueRows(book(10, 1, "first"))
	db := orm.NewDB(c, postgres.Dialect{})

	a, err := Authors.With(db).Load(Authors.Books).First(context.Background())
	if err != nil {
		t.Fatalf("First() error = %v", err)
	}
	if len(a.Books) != 1 {
		t.Errorf("alice has %d books, want 1", len(a.Books))
	}
}

// A hook on the related row type runs for every row loaded, the same as for
// rows a query returned directly.
func TestLoad_FiresAfterLoadOnRelatedRows(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(author(1, "alice"))
	c.QueueRows(book(10, 1, "  Spaced  "))
	db := orm.NewDB(c, postgres.Dialect{})

	authors, err := Authors.With(db).Load(Authors.Books).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if got := authors[0].Books[0].Title; got != "spaced" {
		t.Errorf("title = %q, want AfterLoad to have normalised it", got)
	}
}

// Load narrows a copy, so the query it was called on has no loads.
func TestLoad_LeavesTheQueryAlone(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(author(1, "alice"))
	c.QueueRows(book(10, 1, "first"))
	c.QueueRows(author(1, "alice"))
	db := orm.NewDB(c, postgres.Dialect{})
	ctx := context.Background()

	base := Authors.With(db)
	if _, err := base.Load(Authors.Books).All(ctx); err != nil {
		t.Fatalf("All() error = %v", err)
	}
	before := len(c.QueryCalls())
	if _, err := base.All(ctx); err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if got := len(c.QueryCalls()) - before; got != 1 {
		t.Errorf("the plain query ran %d statements, want 1: it should carry no load", got)
	}
}

// A narrowed relationship is a value like a query is, so branching from one
// must not let the branches see each other.
func TestPreload_BranchesAreIndependent(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(author(1, "alice"))
	c.QueueRows(book(10, 1, "x"))
	db := orm.NewDB(c, postgres.Dialect{})

	base := Authors.Books.Where(Books.Title.StartsWith("a"))
	published := base.Where(Books.Title.EndsWith("z"))

	if _, err := Authors.With(db).Load(base).All(context.Background()); err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if got := c.QueryCalls()[1]; strings.Count(got, "LIKE") != 1 {
		t.Errorf("the base load carries a branch's condition: %s", got)
	}
	_ = published
}

func TestLoad_Rejected(t *testing.T) {
	ctx := context.Background()

	t.Run("a nil relationship", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		_, err := Authors.With(db).Load(nil).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the nil relationship reported")
		}
		if !strings.Contains(err.Error(), "relationship 0 is nil") {
			t.Errorf("error %q does not say which", err)
		}
	})

	t.Run("a relationship attached to no table", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		var loose orm.HasMany[Book]
		_, err := Authors.With(db).Load(loose).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the unattached marker reported")
		}
		if !strings.Contains(err.Error(), "not attached to a table") {
			t.Errorf("error %q does not name the problem", err)
		}
	})

	t.Run("no field on the row type", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{1})
		db := orm.NewDB(c, postgres.Dialect{})

		_, err := Orphans.With(db).Load(Orphans.Leaves).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the missing field reported")
		}
		for _, want := range []string{"no exported field named", `"Leaves"`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
	})

	t.Run("a field of the wrong shape", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{1})
		db := orm.NewDB(c, postgres.Dialect{})

		_, err := Mistypeds.With(db).Load(Mistypeds.Leaves).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the wrong field shape reported")
		}
		if !strings.Contains(err.Error(), "loads many rows") {
			t.Errorf("error %q does not explain the shape", err)
		}
	})

	t.Run("a single valued field given a slice", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{1})
		db := orm.NewDB(c, postgres.Dialect{})

		_, err := Mistypeds.With(db).Load(Mistypeds.Solo).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the wrong field shape reported")
		}
	})

	t.Run("a projection that leaves out the key", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{"alice"})
		db := orm.NewDB(c, postgres.Dialect{})

		_, err := Authors.With(db).Select(Authors.Name).Load(Authors.Books).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the missing key reported")
		}
		for _, want := range []string{`needs column "id"`, "does not select"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
	})

	t.Run("a projection that keeps the key", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{1, "alice"})
		c.QueueRows(book(10, 1, "first"))
		db := orm.NewDB(c, postgres.Dialect{})

		authors, err := Authors.With(db).Select(Authors.ID, Authors.Name).
			Load(Authors.Books).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(authors[0].Books) != 1 {
			t.Errorf("the load did not run: %+v", authors[0].Books)
		}
	})

	t.Run("a set operation", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		_, err := Authors.With(db).Load(Authors.Books).Where(Authors.ID.Equals(1)).DeleteAll(ctx)
		if err == nil {
			t.Fatal("DeleteAll() error = nil, want the load rejected")
		}
		if !strings.Contains(err.Error(), "a Load") {
			t.Errorf("error %q does not name the clause", err)
		}
	})

	t.Run("the children's query fails", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(author(1, "alice"))
		c.FailOn(`SELECT "id", "author_id", "title" FROM "books" WHERE "author_id" IN ($1)`)
		db := orm.NewDB(c, postgres.Dialect{})

		_, err := Authors.With(db).Load(Authors.Books).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the driver failure")
		}
	})

	t.Run("an unresolvable relationship", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{1, "x"})
		db := orm.NewDB(c, postgres.Dialect{})

		_, err := Unjoinable.With(db).Load(Unjoinable.Books).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the unresolvable relationship reported")
		}
		if !strings.Contains(err.Error(), "no column") {
			t.Errorf("error %q does not explain why it cannot resolve", err)
		}
	})
}
