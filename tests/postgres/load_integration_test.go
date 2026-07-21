//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type lAuthor struct {
	ID   int
	Name string

	Books []lBook
	Desk  *lDesk
}

type lAuthorModel struct {
	orm.Table[lAuthor]
	ID    *orm.IntColumn
	Name  *orm.StringColumn
	Books orm.HasMany[lBook]
	Desk  orm.HasOne[lDesk]
}

var lAuthors = orm.DefineTable[lAuthor]("l_authors", func(t *orm.TableBuilder[lAuthor]) *lAuthorModel {
	return &lAuthorModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
	}
})

type lBook struct {
	ID       int
	AuthorID int
	Title    string

	Author *lAuthor
	Tags   []lTag
}

type lBookModel struct {
	orm.Table[lBook]
	ID       *orm.IntColumn
	AuthorID *orm.IntColumn
	Title    *orm.StringColumn
	Author   orm.BelongsTo[lAuthor]
	Tags     orm.ManyToMany[lTag]
}

func (m *lBookModel) Relations() []orm.RelationDef {
	return []orm.RelationDef{orm.Through(&m.Tags, lBookTags.BookID, lBookTags.TagID)}
}

var lBooks = orm.DefineTable[lBook]("l_books", func(t *orm.TableBuilder[lBook]) *lBookModel {
	return &lBookModel{
		Table:    t.Table(),
		ID:       t.Int("id").PrimaryKey(),
		AuthorID: t.Int("author_id").NotNull().References(lAuthors.ID),
		Title:    t.String("title").NotNull(),
	}
})

type lDesk struct {
	ID       int
	AuthorID int
	Colour   string
}

type lDeskModel struct {
	orm.Table[lDesk]
	ID       *orm.IntColumn
	AuthorID *orm.IntColumn
	Colour   *orm.StringColumn
}

var lDesks = orm.DefineTable[lDesk]("l_desks", func(t *orm.TableBuilder[lDesk]) *lDeskModel {
	return &lDeskModel{
		Table:    t.Table(),
		ID:       t.Int("id").PrimaryKey(),
		AuthorID: t.Int("author_id").Unique().NotNull().References(lAuthors.ID),
		Colour:   t.String("colour").NotNull(),
	}
})

type lTag struct {
	ID   int
	Name string
}

type lTagModel struct {
	orm.Table[lTag]
	ID   *orm.IntColumn
	Name *orm.StringColumn
}

var lTags = orm.DefineTable[lTag]("l_tags", func(t *orm.TableBuilder[lTag]) *lTagModel {
	return &lTagModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").Unique().NotNull(),
	}
})

type lBookTag struct {
	BookID int
	TagID  int
}

type lBookTagModel struct {
	orm.Table[lBookTag]
	BookID *orm.IntColumn
	TagID  *orm.IntColumn
}

var lBookTags = orm.DefineTable[lBookTag]("l_book_tags", func(t *orm.TableBuilder[lBookTag]) *lBookTagModel {
	return &lBookTagModel{
		Table:  t.Table(),
		BookID: t.Int("book_id").PrimaryKey().References(lBooks.ID),
		TagID:  t.Int("tag_id").PrimaryKey().References(lTags.ID),
	}
})

// Relationships are checked against a fake driver elsewhere. This runs them
// against the database they were written for, where the keys are ones Postgres
// generated, the rows have to find their way back to the right parent on their
// own, and a correlated EXISTS has to mean what it looks like it means.
func TestRelationships_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}

	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS l_book_tags, l_books, l_desks, l_tags, l_authors CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(lAuthors, lDesks, lTags, lBooks, lBookTags)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	ops, _ := migrate.Diff(schema.Schema{}, desired)
	ddl, err := migrate.Generate(dialect, ops)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if _, err := conn.Exec(ctx, ddl); err != nil {
		t.Fatalf("applying schema failed: %v\n%s", err, ddl)
	}

	db := orm.NewDB(conn, dialect)

	// Enough authors to cross a chunk boundary later, each with a predictable
	// number of books, so a misplaced row shows up as a count that is wrong.
	const authors = 40
	as := make([]*lAuthor, authors)
	for i := range as {
		as[i] = &lAuthor{Name: fmt.Sprintf("author-%02d", i)}
	}
	if err := lAuthors.With(db).InsertMany(ctx, as...); err != nil {
		t.Fatalf("InsertMany authors: %v", err)
	}

	var books []*lBook
	for i, a := range as {
		// Author i gets i%3 + 1 books, so the counts differ between them.
		for j := 0; j <= i%3; j++ {
			books = append(books, &lBook{
				AuthorID: a.ID,
				Title:    fmt.Sprintf("%s-book-%d", a.Name, j),
			})
		}
	}
	if err := lBooks.With(db).InsertMany(ctx, books...); err != nil {
		t.Fatalf("InsertMany books: %v", err)
	}

	desks := []*lDesk{{AuthorID: as[0].ID, Colour: "oak"}, {AuthorID: as[1].ID, Colour: "pine"}}
	if err := lDesks.With(db).InsertMany(ctx, desks...); err != nil {
		t.Fatalf("InsertMany desks: %v", err)
	}

	tags := []*lTag{{Name: "go"}, {Name: "sql"}, {Name: "orm"}}
	if err := lTags.With(db).InsertMany(ctx, tags...); err != nil {
		t.Fatalf("InsertMany tags: %v", err)
	}
	// The first book gets every tag, the second gets one they share.
	pairs := []*lBookTag{
		{BookID: books[0].ID, TagID: tags[0].ID},
		{BookID: books[0].ID, TagID: tags[1].ID},
		{BookID: books[0].ID, TagID: tags[2].ID},
		{BookID: books[1].ID, TagID: tags[0].ID},
	}
	if err := lBookTags.With(db).InsertMany(ctx, pairs...); err != nil {
		t.Fatalf("InsertMany pairs: %v", err)
	}

	t.Run("has many, with every row landing on its own parent", func(t *testing.T) {
		got, err := lAuthors.With(db).Load(lAuthors.Books).OrderBy(lAuthors.ID.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != authors {
			t.Fatalf("All() returned %d authors, want %d", len(got), authors)
		}
		for i, a := range got {
			want := i%3 + 1
			if len(a.Books) != want {
				t.Fatalf("%s has %d books, want %d", a.Name, len(a.Books), want)
			}
			for _, b := range a.Books {
				if b.AuthorID != a.ID {
					t.Fatalf("%s was given a book belonging to %d", a.Name, b.AuthorID)
				}
			}
		}
	})

	t.Run("belongs to", func(t *testing.T) {
		got, err := lBooks.With(db).Load(lBooks.Author).OrderBy(lBooks.ID.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		for _, b := range got {
			if b.Author == nil {
				t.Fatalf("%q has no author", b.Title)
			}
			if b.Author.ID != b.AuthorID {
				t.Errorf("%q was given author %d, want %d", b.Title, b.Author.ID, b.AuthorID)
			}
		}
	})

	t.Run("has one, present and absent", func(t *testing.T) {
		got, err := lAuthors.With(db).Load(lAuthors.Desk).OrderBy(lAuthors.ID.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if got[0].Desk == nil || got[0].Desk.Colour != "oak" {
			t.Errorf("first author's desk = %+v, want the oak one", got[0].Desk)
		}
		if got[2].Desk != nil {
			t.Errorf("third author's desk = %+v, want nil", got[2].Desk)
		}
	})

	t.Run("many to many, in two statements", func(t *testing.T) {
		got, err := lBooks.With(db).Load(lBooks.Tags).
			Where(lBooks.ID.In(books[0].ID, books[1].ID)).
			OrderBy(lBooks.ID.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got[0].Tags) != 3 {
			t.Errorf("first book has %d tags, want 3", len(got[0].Tags))
		}
		if len(got[1].Tags) != 1 || got[1].Tags[0].Name != "go" {
			t.Errorf("second book's tags = %+v, want the one they share", got[1].Tags)
		}
	})

	t.Run("nested", func(t *testing.T) {
		got, err := lAuthors.With(db).
			Where(lAuthors.ID.Eq(as[0].ID)).
			Load(lAuthors.Books.Load(lBooks.Tags)).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got[0].Books) != 1 {
			t.Fatalf("author has %d books", len(got[0].Books))
		}
		if len(got[0].Books[0].Tags) != 3 {
			t.Errorf("the nested load did not reach the book: %+v", got[0].Books[0].Tags)
		}
	})

	t.Run("narrowed and sorted", func(t *testing.T) {
		got, err := lAuthors.With(db).
			Where(lAuthors.ID.Eq(as[2].ID)).
			Load(lAuthors.Books.
				Where(lBooks.Title.EndsWith("-1")).
				OrderBy(lBooks.Title.Desc())).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got[0].Books) != 1 {
			t.Fatalf("author has %d matching books, want 1: %+v", len(got[0].Books), got[0].Books)
		}
	})

	t.Run("a limit is per parent", func(t *testing.T) {
		got, err := lAuthors.With(db).
			Load(lAuthors.Books.OrderBy(lBooks.Title.Asc()).Limit(1)).
			OrderBy(lAuthors.ID.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		// Every author has at least one book, and none gets more than one.
		for _, a := range got {
			if len(a.Books) != 1 {
				t.Fatalf("%s has %d books, want exactly 1", a.Name, len(a.Books))
			}
		}
	})

	// Forty parents at one key each is well under Postgres's ceiling, so this
	// forces the split by lowering nothing: it proves the chunked path
	// assembles the same answer the single statement does.
	t.Run("the parents' keys are chunked", func(t *testing.T) {
		got, err := lAuthors.With(db).Load(lAuthors.Books).OrderBy(lAuthors.ID.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		total := 0
		for _, a := range got {
			total += len(a.Books)
		}
		if total != len(books) {
			t.Errorf("loaded %d books in total, want %d", total, len(books))
		}
	})

	// ── Has and HasNone ─────────────────────────────────────────────────
	//
	// A correlated EXISTS is the first statement Tork writes that names two
	// tables, so what matters here is that Postgres resolves the outer
	// table's column from inside the subquery at all.

	t.Run("has a desk, and has none", func(t *testing.T) {
		with, err := lAuthors.With(db).Where(orm.Has(lAuthors.Desk)).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(with) != len(desks) {
			t.Errorf("%d authors have a desk, want %d", len(with), len(desks))
		}

		without, err := lAuthors.With(db).Where(orm.HasNone(lAuthors.Desk)).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(without) != authors-len(desks) {
			t.Errorf("%d authors have no desk, want %d", len(without), authors-len(desks))
		}
		// Between them they account for everyone, exactly once.
		if len(with)+len(without) != authors {
			t.Errorf("has and has-none cover %d authors, want %d", len(with)+len(without), authors)
		}
	})

	t.Run("has, narrowed by a condition on the related rows", func(t *testing.T) {
		got, err := lAuthors.With(db).
			Where(orm.Has(lAuthors.Books, lBooks.Title.EndsWith("-2"))).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		// Only authors given three books have a "-2", which is one in three.
		want := 0
		for i := range authors {
			if i%3 == 2 {
				want++
			}
		}
		if len(got) != want {
			t.Errorf("%d authors have a third book, want %d", len(got), want)
		}
	})

	t.Run("has, through a join table", func(t *testing.T) {
		tagged, err := lBooks.With(db).Where(orm.Has(lBooks.Tags)).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(tagged) != 2 {
			t.Errorf("%d books are tagged, want 2", len(tagged))
		}

		byName, err := lBooks.With(db).
			Where(orm.Has(lBooks.Tags, lTags.Name.Eq("sql"))).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(byName) != 1 || byName[0].ID != books[0].ID {
			t.Errorf("books tagged sql = %+v, want just the first", byName)
		}

		untagged, err := lBooks.With(db).Where(orm.HasNone(lBooks.Tags)).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(untagged) != len(books)-2 {
			t.Errorf("%d books are untagged, want %d", len(untagged), len(books)-2)
		}
	})

	t.Run("has, nested one relationship inside another", func(t *testing.T) {
		got, err := lAuthors.With(db).
			Where(orm.Has(lAuthors.Books, orm.Has(lBooks.Tags))).
			OrderBy(lAuthors.ID.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		// The two tagged books belong to the first two authors.
		if len(got) != 2 || got[0].ID != as[0].ID || got[1].ID != as[1].ID {
			t.Errorf("authors with a tagged book = %+v, want the first two", got)
		}
	})

	t.Run("has, beside other conditions and a load", func(t *testing.T) {
		got, err := lAuthors.With(db).
			Where(
				lAuthors.Name.StartsWith("author-0"),
				orm.Has(lAuthors.Books, lBooks.Title.EndsWith("-0")),
			).
			Load(lAuthors.Books).
			OrderBy(lAuthors.ID.Asc()).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) == 0 {
			t.Fatal("no authors matched, want the first ten")
		}
		for _, a := range got {
			if len(a.Books) == 0 {
				t.Errorf("%s matched but loaded no books", a.Name)
			}
		}
	})
}
