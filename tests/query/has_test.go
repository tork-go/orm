package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

const authorCols = `"id", "name"`

func TestHas_Shapes(t *testing.T) {
	tests := []struct {
		name string
		pred orm.Predicate
		want string
	}{
		{
			name: "has many, unfiltered",
			pred: orm.Has(Authors.Books),
			want: `EXISTS (SELECT 1 FROM "books" ` +
				`WHERE "books"."author_id" = "authors"."id")`,
		},
		{
			name: "has many, filtered",
			pred: orm.Has(Authors.Books, Books.Title.Equals("Mort")),
			want: `EXISTS (SELECT 1 FROM "books" ` +
				`WHERE "books"."author_id" = "authors"."id" AND "books"."title" = $1)`,
		},
		{
			name: "has none",
			pred: orm.HasNone(Authors.Books),
			want: `NOT EXISTS (SELECT 1 FROM "books" ` +
				`WHERE "books"."author_id" = "authors"."id")`,
		},
		{
			name: "has none, filtered",
			pred: orm.HasNone(Authors.Books, Books.Title.Equals("Mort")),
			want: `NOT EXISTS (SELECT 1 FROM "books" ` +
				`WHERE "books"."author_id" = "authors"."id" AND "books"."title" = $1)`,
		},
		{
			name: "has one",
			pred: orm.Has(Authors.Desk, Desks.Colour.Equals("oak")),
			want: `EXISTS (SELECT 1 FROM "desks" ` +
				`WHERE "desks"."author_id" = "authors"."id" AND "desks"."colour" = $1)`,
		},
		{
			// Several conditions on the related rows are joined with AND
			// inside the one subquery, not spread over several.
			name: "several conditions",
			pred: orm.Has(Authors.Books, Books.Title.Equals("Mort"), Books.ID.GreaterThan(5)),
			want: `EXISTS (SELECT 1 FROM "books" ` +
				`WHERE "books"."author_id" = "authors"."id" ` +
				`AND "books"."title" = $1 AND "books"."id" > $2)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := Authors.With(pg()).Where(tt.pred).SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			want := `SELECT ` + authorCols + ` FROM "authors" WHERE ` + tt.want
			if sql != want {
				t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
			}
		})
	}
}

// A BelongsTo holds the key locally, so the correlation is the other way round.
func TestHas_BelongsTo(t *testing.T) {
	sql, _, err := Books.With(pg()).Where(orm.Has(Books.Author, Authors.Name.Equals("Terry"))).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "author_id", "title" FROM "books" WHERE EXISTS (` +
		`SELECT 1 FROM "authors" ` +
		`WHERE "authors"."id" = "books"."author_id" AND "authors"."name" = $1)`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// A many to many has two hops, so it becomes two EXISTS rather than a join.
func TestHas_ManyToMany(t *testing.T) {
	t.Run("unfiltered stops at the join table", func(t *testing.T) {
		sql, _, err := Books.With(pg()).Where(orm.Has(Books.Tags)).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		want := `SELECT "id", "author_id", "title" FROM "books" WHERE EXISTS (` +
			`SELECT 1 FROM "book_tags" WHERE "book_tags"."book_id" = "books"."id")`
		if sql != want {
			t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
		}
	})

	t.Run("filtered reaches the far table", func(t *testing.T) {
		sql, args, err := Books.With(pg()).Where(orm.Has(Books.Tags, Tags.Name.Equals("go"))).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		want := `SELECT "id", "author_id", "title" FROM "books" WHERE EXISTS (` +
			`SELECT 1 FROM "book_tags" WHERE "book_tags"."book_id" = "books"."id" ` +
			`AND EXISTS (SELECT 1 FROM "tags" ` +
			`WHERE "tags"."id" = "book_tags"."tag_id" AND "tags"."name" = $1))`
		if sql != want {
			t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
		}
		if len(args) != 1 || args[0] != "go" {
			t.Errorf("args = %v, want [go]", args)
		}
	})

	t.Run("negated", func(t *testing.T) {
		sql, _, err := Books.With(pg()).Where(orm.HasNone(Books.Tags)).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if !strings.Contains(sql, `NOT EXISTS (SELECT 1 FROM "book_tags"`) {
			t.Errorf("SQL() = %s, want the whole thing negated", sql)
		}
	})
}

// It is an ordinary predicate, so it goes wherever one goes.
func TestHas_Composes(t *testing.T) {
	t.Run("beside other conditions", func(t *testing.T) {
		sql, args, err := Authors.With(pg()).Where(
			Authors.Name.StartsWith("U"),
			orm.Has(Authors.Books, Books.Title.Equals("Mort")),
		).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if !strings.Contains(sql, `"name" LIKE $1`) || !strings.Contains(sql, `"title" = $2`) {
			t.Errorf("SQL() = %s, want both conditions numbered in order", sql)
		}
		if len(args) != 2 {
			t.Errorf("args = %v, want two", args)
		}
	})

	t.Run("inside Or", func(t *testing.T) {
		sql, _, err := Authors.With(pg()).Where(
			orm.Or(orm.Has(Authors.Books), orm.Has(Authors.Desk)),
		).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if !strings.Contains(sql, ` OR `) || strings.Count(sql, "EXISTS") != 2 {
			t.Errorf("SQL() = %s, want both alternatives", sql)
		}
	})

	t.Run("inside Not", func(t *testing.T) {
		sql, _, err := Authors.With(pg()).Where(orm.Not(orm.Has(Authors.Books))).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if !strings.Contains(sql, "NOT (EXISTS") {
			t.Errorf("SQL() = %s, want the negation around it", sql)
		}
	})

	t.Run("nested inside another Has", func(t *testing.T) {
		// Authors who have a book that itself has a tag: the inner Has is a
		// condition on the related rows, and compiles against their table.
		sql, _, err := Authors.With(pg()).Where(
			orm.Has(Authors.Books, orm.Has(Books.Tags, Tags.Name.Equals("go"))),
		).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if strings.Count(sql, "EXISTS") != 3 {
			t.Errorf("SQL() = %s, want three levels", sql)
		}
		if !strings.Contains(sql, `"book_tags"."book_id" = "books"."id"`) {
			t.Errorf("SQL() = %s, want the inner correlation against books", sql)
		}
	})

	t.Run("in front of a write", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.RowsAffected = 2
		db := orm.NewDB(c, postgres.Dialect{})

		n, err := Authors.With(db).Where(orm.HasNone(Authors.Books)).DeleteAll(context.Background())
		if err != nil {
			t.Fatalf("DeleteAll() error = %v", err)
		}
		if n != 2 {
			t.Errorf("DeleteAll() = %d, want 2", n)
		}
		want := `DELETE FROM "authors" WHERE NOT EXISTS (SELECT 1 FROM "books" ` +
			`WHERE "books"."author_id" = "authors"."id")`
		if got := c.ExecCalls()[0]; got != want {
			t.Errorf("ran  %s\nwant %s", got, want)
		}
	})

	t.Run("alongside a load", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(author(1, "alice"))
		c.QueueRows(book(10, 1, "first"))
		db := orm.NewDB(c, postgres.Dialect{})

		got, err := Authors.With(db).
			Where(orm.Has(Authors.Books)).
			Load(Authors.Books).
			All(context.Background())
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got[0].Books) != 1 {
			t.Errorf("the load did not run alongside the filter")
		}
	})
}

// Nothing about it may assume Postgres's spelling.
func TestHas_AsksTheDialect(t *testing.T) {
	sql, _, err := Authors.With(fake()).Where(orm.Has(Authors.Books, Books.Title.Equals("x"))).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT [id], [name] FROM [authors] WHERE EXISTS (SELECT 1 FROM [books] ` +
		`WHERE [books].[author_id] = [authors].[id] AND [books].[title] = ?)`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

func TestHas_Rejected(t *testing.T) {
	tests := map[string]struct {
		pred  orm.Predicate
		query func(orm.Predicate) (string, []any, error)
		want  string
	}{
		"a nil relationship": {
			pred: orm.Has(nil),
			want: "no relationship",
		},
		"a relationship attached to no table": {
			pred: orm.Has(orm.HasMany[Book]{}),
			want: "no relationship",
		},
		"another table's relationship": {
			pred: orm.Has(Books.Tags),
			want: `belongs to table "books"`,
		},
		"a relationship that cannot resolve": {
			pred: orm.Has(Unjoinable.Books),
			want: "no column on books references unjoinable",
		},
		"another table's column in the conditions": {
			pred: orm.Has(Authors.Books, Tags.Name.Equals("go")),
			want: `table "tags"`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := Authors.With(pg()).Where(tt.pred).SQL()
			if err == nil {
				t.Fatal("SQL() error = nil, want the predicate rejected")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

// The far side of a many to many is compiled against its own table, so a
// condition naming another one is rejected there too.
func TestHas_ManyToManyRejectsAForeignColumn(t *testing.T) {
	_, _, err := Books.With(pg()).Where(orm.Has(Books.Tags, Authors.Name.Equals("x"))).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign column rejected")
	}
	if !strings.Contains(err.Error(), `table "authors"`) {
		t.Errorf("error %q does not name the other table", err)
	}
}

// The check that a projection kept the matching column has to resolve the
// relationship to know which column that is, so a relationship that cannot
// resolve is reported from there.
func TestLoad_ProjectionCheckReportsAnUnresolvableRelationship(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1})
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := Unjoinable.With(db).Select(Unjoinable.ID).Load(Unjoinable.Books).
		All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want the unresolvable relationship reported")
	}
	if !strings.Contains(err.Error(), "no column on books references unjoinable") {
		t.Errorf("error %q does not explain why it cannot resolve", err)
	}
}

// The conditions are copied, so a slice the caller keeps changing does not
// rewrite a predicate already built.
func TestHas_DoesNotAliasTheCallersSlice(t *testing.T) {
	preds := []orm.Predicate{Books.Title.Equals("first")}
	pred := orm.Has(Authors.Books, preds...)

	preds[0] = Books.Title.Equals("second")
	_, args, err := Authors.With(pg()).Where(pred).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if len(args) != 1 || args[0] != "first" {
		t.Errorf("bound %v, want the condition as it was when Has was called", args)
	}
}
