package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// authorIDs is the subquery every shape test embeds: the author of every
// published book, which is the question a caller would actually ask.
func authorIDs(db *orm.DB) *orm.Scalars[int] {
	return orm.Select(Books.With(db).Where(Books.Title.Eq("Mort")), Books.AuthorID)
}

func TestInQuery_Shapes(t *testing.T) {
	tests := []struct {
		name string
		pred func(*orm.DB) orm.Predicate
		want string
	}{
		{
			name: "in",
			pred: func(db *orm.DB) orm.Predicate { return Authors.ID.InQuery(authorIDs(db)) },
			want: `"id" IN (SELECT "books"."author_id" FROM "books" ` +
				`WHERE "books"."title" = $1)`,
		},
		{
			name: "not in",
			pred: func(db *orm.DB) orm.Predicate { return Authors.ID.NotInQuery(authorIDs(db)) },
			want: `"id" NOT IN (SELECT "books"."author_id" FROM "books" ` +
				`WHERE "books"."title" = $1)`,
		},
		{
			name: "unfiltered subquery",
			pred: func(db *orm.DB) orm.Predicate {
				return Authors.ID.InQuery(orm.Select(Books.With(db), Books.AuthorID))
			},
			want: `"id" IN (SELECT "books"."author_id" FROM "books")`,
		},
		{
			// Distinct belongs to the subquery, not to the statement embedding
			// it, so it lands inside the parentheses.
			name: "distinct subquery",
			pred: func(db *orm.DB) orm.Predicate {
				return Authors.ID.InQuery(orm.Select(Books.With(db), Books.AuthorID).Distinct())
			},
			want: `"id" IN (SELECT DISTINCT "books"."author_id" FROM "books")`,
		},
		{
			name: "ordered and capped subquery",
			pred: func(db *orm.DB) orm.Predicate {
				return Authors.ID.InQuery(orm.Select(
					Books.With(db).OrderBy(Books.ID.Desc()).Limit(5), Books.AuthorID))
			},
			want: `"id" IN (SELECT "books"."author_id" FROM "books" ` +
				`ORDER BY "books"."id" DESC LIMIT 5)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := pg()
			sql, _, err := Authors.With(db).Where(tt.pred(db)).SQL()
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

// The subquery's columns are qualified and the outer statement's are not: two
// tables are in scope inside the parentheses, and only one outside them.
func TestInQuery_QualifiesOnlyInside(t *testing.T) {
	db := pg()
	sql, _, err := Authors.With(db).Where(
		Authors.Name.Eq("Terry"),
		Authors.ID.InQuery(authorIDs(db)),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `WHERE ("name" = $1 AND "id" IN (`) {
		t.Errorf("SQL() = %s, want the outer columns bare", sql)
	}
	if !strings.Contains(sql, `SELECT "books"."author_id" FROM "books" WHERE "books"."title" = $2`) {
		t.Errorf("SQL() = %s, want the inner columns qualified", sql)
	}
}

// Placeholders keep counting across the boundary rather than restarting, and
// the values arrive in the order the statement reads them.
func TestInQuery_NumbersArgumentsAcrossTheBoundary(t *testing.T) {
	db := pg()
	sql, args, err := Authors.With(db).Where(
		Authors.Name.Eq("first"),
		Authors.ID.InQuery(orm.Select(
			Books.With(db).Where(Books.Title.Eq("second")), Books.AuthorID)),
		Authors.Name.NotEq("third"),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	for i, want := range []string{`"name" = $1`, `"books"."title" = $2`, `"name" <> $3`} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL() = %s, want %s at position %d", sql, want, i+1)
		}
	}
	if len(args) != 3 || args[0] != "first" || args[1] != "second" || args[2] != "third" {
		t.Errorf("args = %v, want them in statement order", args)
	}
}

// It is an ordinary predicate, so it goes wherever one goes.
func TestInQuery_Composes(t *testing.T) {
	t.Run("inside Or", func(t *testing.T) {
		db := pg()
		sql, _, err := Authors.With(db).Where(orm.Or(
			Authors.ID.InQuery(authorIDs(db)),
			Authors.Name.Eq("Terry"),
		)).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if !strings.Contains(sql, ` OR `) || !strings.Contains(sql, " IN (SELECT") {
			t.Errorf("SQL() = %s, want both alternatives", sql)
		}
	})

	t.Run("inside Not", func(t *testing.T) {
		db := pg()
		sql, _, err := Authors.With(db).Where(orm.Not(Authors.ID.InQuery(authorIDs(db)))).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if !strings.Contains(sql, `NOT ("id" IN (SELECT`) {
			t.Errorf("SQL() = %s, want the negation around it", sql)
		}
	})

	t.Run("beside a Has", func(t *testing.T) {
		db := pg()
		sql, _, err := Authors.With(db).Where(
			orm.Has(Authors.Books),
			Authors.ID.InQuery(authorIDs(db)),
		).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if !strings.Contains(sql, "EXISTS (") || !strings.Contains(sql, " IN (SELECT") {
			t.Errorf("SQL() = %s, want both conditions", sql)
		}
	})

	t.Run("in front of a write", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.RowsAffected = 2
		db := orm.NewDB(c, postgres.Dialect{})

		n, err := Authors.With(db).Where(Authors.ID.NotInQuery(
			orm.Select(Books.With(db), Books.AuthorID),
		)).DeleteAll(context.Background())
		if err != nil {
			t.Fatalf("DeleteAll() error = %v", err)
		}
		if n != 2 {
			t.Errorf("DeleteAll() = %d, want 2", n)
		}
		want := `DELETE FROM "authors" WHERE "id" NOT IN (` +
			`SELECT "books"."author_id" FROM "books")`
		if got := c.ExecCalls()[0]; got != want {
			t.Errorf("ran  %s\nwant %s", got, want)
		}
	})

	t.Run("inside a Has", func(t *testing.T) {
		// The condition on the related rows is compiled against their table,
		// and a subquery there is a third level of nesting.
		db := pg()
		sql, _, err := Authors.With(db).Where(orm.Has(Authors.Books,
			Books.ID.InQuery(orm.Select(BookTags.With(db), BookTags.BookID)),
		)).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		want := `EXISTS (SELECT 1 FROM "books" WHERE "books"."author_id" = "authors"."id" ` +
			`AND "books"."id" IN (SELECT "book_tags"."book_id" FROM "book_tags"))`
		if !strings.Contains(sql, want) {
			t.Errorf("SQL()  = %s\nwant it to contain %s", sql, want)
		}
	})
}

// A subquery is a query, so one can embed another.
func TestInQuery_Nested(t *testing.T) {
	db := pg()
	inner := orm.Select(BookTags.With(db).Where(BookTags.TagID.Eq(7)), BookTags.BookID)
	outer := orm.Select(Books.With(db).Where(Books.ID.InQuery(inner)), Books.AuthorID)

	sql, args, err := Authors.With(db).Where(Authors.ID.InQuery(outer)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT ` + authorCols + ` FROM "authors" WHERE "id" IN (` +
		`SELECT "books"."author_id" FROM "books" WHERE "books"."id" IN (` +
		`SELECT "book_tags"."book_id" FROM "book_tags" WHERE "book_tags"."tag_id" = $1))`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != 7 {
		t.Errorf("args = %v, want [7]", args)
	}
}

// The same subquery value can be embedded twice, and run on its own, without
// either use disturbing the others.
func TestInQuery_SubqueryIsReusable(t *testing.T) {
	db := pg()
	sub := authorIDs(db)

	first, _, err := Authors.With(db).Where(Authors.ID.InQuery(sub)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	second, _, err := Authors.With(db).Where(Authors.ID.InQuery(sub)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if first != second {
		t.Errorf("embedding twice gave\n%s\nand\n%s", first, second)
	}

	// Run on its own it is unqualified again, since only one table is in scope.
	alone, _, err := sub.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if alone != `SELECT "author_id" FROM "books" WHERE "title" = $1` {
		t.Errorf("SQL() = %s, want the standalone statement unqualified", alone)
	}
}

// A nullable outer column takes a subquery of the underlying type, exactly as
// its Eq takes one.
func TestInQuery_NullableColumn(t *testing.T) {
	db := pg()
	ids := orm.Select(Books.With(db), Books.ID)

	sql, _, err := Reviews.With(db).Where(Reviews.BookID.InQuery(ids)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `"book_id" IN (SELECT "books"."id" FROM "books")`) {
		t.Errorf("SQL() = %s, want the nullable column matched against the subquery", sql)
	}

	sql, _, err = Reviews.With(db).Where(Reviews.BookID.NotInQuery(ids)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `"book_id" NOT IN (SELECT`) {
		t.Errorf("SQL() = %s, want the negated form", sql)
	}
}

// NonNull both retypes the subquery and excludes the NULLs, which is the whole
// reason it has to be called.
func TestNonNull(t *testing.T) {
	t.Run("adds the condition", func(t *testing.T) {
		db := pg()
		sub := orm.Select(Reviews.With(db), Reviews.BookID)

		sql, _, err := Books.With(db).Where(Books.ID.NotInQuery(orm.NonNull(sub))).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		want := `SELECT "id", "author_id", "title" FROM "books" WHERE "id" NOT IN (` +
			`SELECT "reviews"."book_id" FROM "reviews" ` +
			`WHERE "reviews"."book_id" IS NOT NULL)`
		if sql != want {
			t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
		}
	})

	t.Run("keeps the subquery's own conditions", func(t *testing.T) {
		db := pg()
		sub := orm.Select(Reviews.With(db).Where(Reviews.Text.Eq("good")), Reviews.BookID)

		sql, args, err := Books.With(db).Where(Books.ID.InQuery(orm.NonNull(sub))).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		want := `("reviews"."text" = $1 AND "reviews"."book_id" IS NOT NULL)`
		if !strings.Contains(sql, want) {
			t.Errorf("SQL()  = %s\nwant it to contain %s", sql, want)
		}
		if len(args) != 1 || args[0] != "good" {
			t.Errorf("args = %v, want [good]", args)
		}
	})

	t.Run("leaves the subquery it was given alone", func(t *testing.T) {
		db := pg()
		sub := orm.Select(Reviews.With(db), Reviews.BookID)

		if _, _, err := Books.With(db).Where(Books.ID.InQuery(orm.NonNull(sub))).SQL(); err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		sql, _, err := sub.SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if strings.Contains(sql, "IS NOT NULL") {
			t.Errorf("SQL() = %s, want the original query unchanged", sql)
		}
	})

	t.Run("narrowing twice adds the condition once per use", func(t *testing.T) {
		db := pg()
		sub := orm.Select(Reviews.With(db), Reviews.BookID)
		narrowed := orm.NonNull(sub)

		first, _, err := Books.With(db).Where(Books.ID.InQuery(narrowed)).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		second, _, err := Books.With(db).Where(Books.ID.InQuery(narrowed)).SQL()
		if err != nil {
			t.Fatalf("SQL() error = %v", err)
		}
		if first != second {
			t.Errorf("embedding twice gave\n%s\nand\n%s", first, second)
		}
		if strings.Count(first, "IS NOT NULL") != 1 {
			t.Errorf("SQL() = %s, want the condition exactly once", first)
		}
	})
}

// Nothing about it may assume Postgres's spelling.
func TestInQuery_AsksTheDialect(t *testing.T) {
	db := fake()
	sql, _, err := Authors.With(db).Where(Authors.ID.InQuery(authorIDs(db))).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT [id], [name] FROM [authors] WHERE [id] IN (` +
		`SELECT [books].[author_id] FROM [books] WHERE [books].[title] = ?)`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// The statement is one round trip: the subquery is never run on its own.
func TestInQuery_RunsOneStatement(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(author(1, "alice"))
	db := orm.NewDB(c, postgres.Dialect{})

	got, err := Authors.With(db).Where(Authors.ID.InQuery(
		orm.Select(Books.With(db), Books.AuthorID),
	)).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "alice" {
		t.Errorf("All() = %+v, want the one row", got)
	}
	if n := len(c.QueryCalls()); n != 1 {
		t.Errorf("ran %d statements, want 1", n)
	}
}

func TestInQuery_Rejected(t *testing.T) {
	tests := map[string]struct {
		pred func(*orm.DB) orm.Predicate
		want string
	}{
		"no subquery at all": {
			pred: func(*orm.DB) orm.Predicate { return Authors.ID.InQuery(nil) },
			want: "no subquery to match against",
		},
		"a nil query value": {
			pred: func(*orm.DB) orm.Predicate {
				return Authors.ID.InQuery((*orm.Scalars[int])(nil))
			},
			want: "the subquery is nil",
		},
		"a nil query value inside NonNull": {
			pred: func(*orm.DB) orm.Predicate {
				return Authors.ID.InQuery(orm.NonNull[int](nil))
			},
			want: "NonNull was given no subquery",
		},
		"a subquery selecting no column": {
			pred: func(db *orm.DB) orm.Predicate {
				return Authors.ID.InQuery(orm.Select[int](Books.With(db), nil))
			},
			want: "Select was given no column",
		},
		"a subquery that failed to build": {
			pred: func(db *orm.DB) orm.Predicate {
				return Authors.ID.InQuery(orm.Select(Books.With(db).Limit(-1), Books.AuthorID))
			},
			want: "Limit(-1) is negative",
		},
		"a condition on another table's column": {
			pred: func(db *orm.DB) orm.Predicate {
				return Books.ID.InQuery(orm.Select(BookTags.With(db), BookTags.BookID))
			},
			want: `belongs to table "books"`,
		},
		"a subquery selecting another table's column": {
			pred: func(db *orm.DB) orm.Predicate {
				return Authors.ID.InQuery(orm.Select(Books.With(db), Tags.ID))
			},
			want: `belongs to table "tags"`,
		},
		"a subquery filtered on another table's column": {
			pred: func(db *orm.DB) orm.Predicate {
				return Authors.ID.InQuery(orm.Select(
					Books.With(db).Where(Tags.Name.Eq("go")), Books.AuthorID))
			},
			want: `belongs to table "tags"`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			db := pg()
			_, _, err := Authors.With(db).Where(tt.pred(db)).SQL()
			if err == nil {
				t.Fatal("SQL() error = nil, want the subquery rejected")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

// A subquery narrowed by NonNull is rejected for the same reasons an ordinary
// one is, since it is the same query underneath.
func TestNonNull_RejectsWhatItWraps(t *testing.T) {
	db := pg()
	_, _, err := Books.With(db).Where(Books.ID.InQuery(orm.NonNull(
		orm.Select(Reviews.With(db).Limit(-1), Reviews.BookID),
	))).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the failed subquery reported")
	}
	if !strings.Contains(err.Error(), "Limit(-1) is negative") {
		t.Errorf("error %q does not name the failure", err)
	}
}
