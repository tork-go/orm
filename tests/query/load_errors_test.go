package query_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

func review(id int, bookID *int, text string) []any { return []any{id, bookID, text} }

// A key that is NULL matches nothing, on either side of the relationship.
func TestLoad_NullKeys(t *testing.T) {
	ctx := context.Background()

	t.Run("a child whose key is NULL joins no parent", func(t *testing.T) {
		c := fakedriver.NewConn()
		one := 10
		c.QueueRows(book(10, 1, "first"))
		c.QueueRows(review(100, &one, "kept"), review(101, nil, "orphaned"))
		db := orm.NewDB(c, postgres.Dialect{})

		books, err := Books.With(db).Load(Books.Reviews).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(books[0].Reviews) != 1 || books[0].Reviews[0].Text != "kept" {
			t.Errorf("reviews = %+v, want only the one with a key", books[0].Reviews)
		}
	})

	t.Run("a parent whose key is NULL asks for nothing", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(review(100, nil, "orphaned"))
		db := orm.NewDB(c, postgres.Dialect{})

		reviews, err := Reviews.With(db).Load(Reviews.Book).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if reviews[0].Book != nil {
			t.Errorf("book = %+v, want nil", reviews[0].Book)
		}
		// Every parent's key was NULL, so there was nothing to ask for.
		if len(c.QueryCalls()) != 1 {
			t.Errorf("ran %d statements, want only the parents'", len(c.QueryCalls()))
		}
	})
}

// The builders exist on a bare marker as well as on a narrowed one, so a
// relationship can be configured without a Where first.
func TestPreload_BuildersOnABareMarker(t *testing.T) {
	ctx := context.Background()

	t.Run("OrderBy", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(author(1, "alice"))
		c.QueueRows(book(10, 1, "x"))
		db := orm.NewDB(c, postgres.Dialect{})

		if _, err := Authors.With(db).Load(Authors.Books.OrderBy(Books.Title.Asc())).All(ctx); err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if !strings.Contains(c.QueryCalls()[1], `ORDER BY "title" ASC`) {
			t.Errorf("ran %s, want it ordered", c.QueryCalls()[1])
		}
	})

	t.Run("Limit", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(author(1, "alice"))
		c.QueueRows(book(10, 1, "x"))
		db := orm.NewDB(c, postgres.Dialect{})

		if _, err := Authors.With(db).Load(Authors.Books.Limit(3)).All(ctx); err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if !strings.HasSuffix(c.QueryCalls()[1], "LIMIT 3") {
			t.Errorf("ran %s, want it capped", c.QueryCalls()[1])
		}
	})

	t.Run("Load", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(author(1, "alice"))
		c.QueueRows(book(10, 1, "x"))
		c.QueueRows([]any{10, 100})
		c.QueueRows([]any{100, "go"})
		db := orm.NewDB(c, postgres.Dialect{})

		if _, err := Authors.With(db).Load(Authors.Books.Load(Books.Tags)).All(ctx); err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(c.QueryCalls()) != 4 {
			t.Errorf("ran %d statements, want the nested load to have run", len(c.QueryCalls()))
		}
	})

	t.Run("a nil nested relationship is skipped", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(author(1, "alice"))
		c.QueueRows(book(10, 1, "x"))
		db := orm.NewDB(c, postgres.Dialect{})

		if _, err := Authors.With(db).Load(Authors.Books.Load(nil)).All(ctx); err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(c.QueryCalls()) != 2 {
			t.Errorf("ran %d statements, want the nil load ignored", len(c.QueryCalls()))
		}
	})
}

// Whatever goes wrong in a load's own statement reaches the caller rather
// than leaving a half-filled row.
func TestLoad_ChildQueryFailures(t *testing.T) {
	ctx := context.Background()

	tests := map[string]struct {
		setup func(*fakedriver.Conn)
		load  func(db *orm.DB) error
		want  string
	}{
		"a foreign column in the load's filter": {
			setup: func(c *fakedriver.Conn) { c.QueueRows(author(1, "alice")) },
			load: func(db *orm.DB) error {
				_, err := Authors.With(db).Load(Authors.Books.Where(Tags.Name.Eq("go"))).All(ctx)
				return err
			},
			want: `table "tags"`,
		},
		"a foreign column in the load's ordering": {
			setup: func(c *fakedriver.Conn) { c.QueueRows(author(1, "alice")) },
			load: func(db *orm.DB) error {
				_, err := Authors.With(db).Load(Authors.Books.OrderBy(Tags.Name.Asc())).All(ctx)
				return err
			},
			want: `table "tags"`,
		},
		"a result set that fails partway": {
			setup: func(c *fakedriver.Conn) {
				c.QueueRows(author(1, "alice"))
				c.RowsErr = errors.New("connection lost")
			},
			load: func(db *orm.DB) error {
				_, err := Authors.With(db).Load(Authors.Books).All(ctx)
				return err
			},
			want: "connection lost",
		},
		"a row that will not scan": {
			setup: func(c *fakedriver.Conn) {
				c.QueueRows(author(1, "alice"))
				c.QueueRows([]any{"not an id", 1, "x"})
			},
			load: func(db *orm.DB) error {
				_, err := Authors.With(db).Load(Authors.Books).All(ctx)
				return err
			},
			want: "scanning row",
		},
		"a limited load whose statement fails": {
			setup: func(c *fakedriver.Conn) {
				c.QueueRows(author(1, "alice"))
				c.FailOn(`SELECT "id", "author_id", "title" FROM "books" ` +
					`WHERE "author_id" IN ($1) LIMIT 2`)
			},
			load: func(db *orm.DB) error {
				_, err := Authors.With(db).Load(Authors.Books.Limit(2)).All(ctx)
				return err
			},
			want: "books",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := fakedriver.NewConn()
			tt.setup(c)
			err := tt.load(orm.NewDB(c, postgres.Dialect{}))
			if err == nil {
				t.Fatal("no error, want the failure reported")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

// A load runs on another statement's behalf, so what goes wrong in it has to
// travel back out through the query that asked for it.
func TestLoad_ChildResultSetFailsPartway(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(author(1, "alice"))
	c.QueueFailingRows(errors.New("connection lost"), book(10, 1, "first"))
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := Authors.With(db).Load(Authors.Books).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want the failure the load hit")
	}
	if !strings.Contains(err.Error(), "connection lost") {
		t.Errorf("error %q does not carry the driver's own", err)
	}
	if !strings.Contains(err.Error(), `table "books"`) {
		t.Errorf("error %q does not say which statement failed", err)
	}
}

// A nested load whose level above found nothing has nothing to run for.
func TestLoad_NestedWithNoRowsAbove(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(author(1, "alice"))
	c.QueueRows() // alice has no books
	db := orm.NewDB(c, postgres.Dialect{})

	authors, err := Authors.With(db).
		Load(Authors.Books.Load(Books.Tags)).
		All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(authors[0].Books) != 0 {
		t.Errorf("books = %+v, want none", authors[0].Books)
	}
	if len(c.QueryCalls()) != 2 {
		t.Errorf("ran %d statements, want the nested load skipped", len(c.QueryCalls()))
	}
}

// A failure inside a nested load reaches the caller through every level above
// it, rather than leaving the rows above half filled.
func TestLoad_NestedFailurePropagates(t *testing.T) {
	ctx := context.Background()

	t.Run("under a has many", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(author(1, "alice"))
		c.QueueRows(book(10, 1, "first"))
		c.FailOn(`SELECT "book_id", "tag_id" FROM "book_tags" WHERE "book_id" IN ($1)`)
		db := orm.NewDB(c, postgres.Dialect{})

		if _, err := Authors.With(db).Load(Authors.Books.Load(Books.Tags)).All(ctx); err == nil {
			t.Fatal("All() error = nil, want the nested failure")
		}
	})

	t.Run("under a many to many", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(book(10, 1, "first"))
		c.QueueRows([]any{10, 100})
		c.QueueRows([]any{100, "go"})
		c.FailOn(`SELECT "id", "name" FROM "tags" WHERE "id" IN ($1)`)
		db := orm.NewDB(c, postgres.Dialect{})

		// The pairs came back, so the failure is the statement that reads the
		// rows those pairs name.
		_, err := Books.With(db).Load(Books.Tags).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the tags' statement to have failed")
		}
		if !strings.Contains(err.Error(), `table "tags"`) {
			t.Errorf("error %q does not say which statement failed", err)
		}
	})
}

// A hook on a loaded row can refuse, and that has to reach the caller.
func TestLoad_RelatedHookFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(author(1, "alice"))
	c.QueueRows(book(10, 1, "!refuse"))
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := Authors.With(db).Load(Authors.Books).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want the hook's refusal")
	}
	if !errors.Is(err, errBookRefused) {
		t.Errorf("error = %v, want the hook's own", err)
	}
}

// The many to many path has statements of its own that can fail.
func TestLoad_ManyToManyFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("no pairs means no second statement", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(book(10, 1, "first"))
		c.QueueRows() // the join table has nothing for it
		db := orm.NewDB(c, postgres.Dialect{})

		books, err := Books.With(db).Load(Books.Tags).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(books[0].Tags) != 0 {
			t.Errorf("tags = %+v, want none", books[0].Tags)
		}
		if len(c.QueryCalls()) != 2 {
			t.Errorf("ran %d statements, want the tags' one skipped", len(c.QueryCalls()))
		}
	})

	t.Run("the join table's statement fails", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(book(10, 1, "first"))
		c.FailOn(`SELECT "book_id", "tag_id" FROM "book_tags" WHERE "book_id" IN ($1)`)
		db := orm.NewDB(c, postgres.Dialect{})

		if _, err := Books.With(db).Load(Books.Tags).All(ctx); err == nil {
			t.Fatal("All() error = nil, want the driver failure")
		}
	})

	t.Run("a pair that will not scan", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(book(10, 1, "first"))
		c.QueueRows([]any{"not an id", 100})
		db := orm.NewDB(c, postgres.Dialect{})

		_, err := Books.With(db).Load(Books.Tags).All(ctx)
		if err == nil {
			t.Fatal("All() error = nil, want the scan failure")
		}
		if !strings.Contains(err.Error(), "book_tags") {
			t.Errorf("error %q does not name the join table", err)
		}
	})

	t.Run("a pair naming a row the filter excluded", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(book(10, 1, "first"))
		c.QueueRows([]any{10, 100}, []any{10, 101})
		c.QueueRows([]any{100, "go"}) // 101 was filtered out
		db := orm.NewDB(c, postgres.Dialect{})

		books, err := Books.With(db).Load(Books.Tags.Where(Tags.Name.Eq("go"))).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(books[0].Tags) != 1 || books[0].Tags[0].Name != "go" {
			t.Errorf("tags = %+v, want only the one that came back", books[0].Tags)
		}
	})
}

// A single valued relationship keeps the first row it is given rather than
// letting a second silently replace it.
func TestLoad_HasOneKeepsTheFirstRow(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(author(1, "alice"))
	c.QueueRows([]any{5, 1, "oak"}, []any{6, 1, "pine"})
	db := orm.NewDB(c, postgres.Dialect{})

	authors, err := Authors.With(db).Load(Authors.Desk).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if authors[0].Desk == nil || authors[0].Desk.Colour != "oak" {
		t.Errorf("desk = %+v, want the first row", authors[0].Desk)
	}
}
