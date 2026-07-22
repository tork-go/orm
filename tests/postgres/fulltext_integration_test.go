//go:build integration

package postgres_test

import (
	"context"
	"sort"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type ftArticle struct {
	ID    int
	Title string
	Body  string
}

type ftArticleModel struct {
	orm.Table[ftArticle]
	ID    *orm.IntColumn
	Title *orm.StringColumn
	Body  *orm.StringColumn
}

var ftArticles = orm.DefineTable[ftArticle]("ft_articles", func(t *orm.TableBuilder[ftArticle]) *ftArticleModel {
	return &ftArticleModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Title: t.String("title").NotNull(),
		Body:  t.String("body").NotNull(),
	}
})

// What Matches renders is checked against the fakes. That it runs — to_tsvector
// @@ websearch_to_tsquery matching real text, and websearch never erroring on
// input that would make to_tsquery raise a syntax error — is only knowable
// against a real server.
func TestFullText_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}

	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS ft_articles CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(ftArticles)
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
	if err := ftArticles.With(db).InsertMany(ctx,
		&ftArticle{Title: "Go", Body: "golang orm query builder"},
		&ftArticle{Title: "Rust", Body: "rust systems programming"},
		&ftArticle{Title: "Draft", Body: "golang draft notes"},
	); err != nil {
		t.Fatalf("InsertMany failed: %v", err)
	}

	titles := func(as []*ftArticle) []string {
		out := make([]string, len(as))
		for i, a := range as {
			out[i] = a.Title
		}
		sort.Strings(out)
		return out
	}
	match := func(t *testing.T, query string) []string {
		t.Helper()
		got, err := ftArticles.With(db).Where(ftArticles.Body.Matches(query)).All(ctx)
		if err != nil {
			t.Fatalf("Matches(%q) error = %v", query, err)
		}
		return titles(got)
	}
	eq := func(t *testing.T, got, want []string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("matched %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("matched %v, want %v", got, want)
			}
		}
	}

	t.Run("a single term", func(t *testing.T) {
		eq(t, match(t, "golang"), []string{"Draft", "Go"})
	})
	t.Run("two terms are both required", func(t *testing.T) {
		eq(t, match(t, "golang orm"), []string{"Go"})
	})
	t.Run("a negated term is excluded", func(t *testing.T) {
		eq(t, match(t, "golang -draft"), []string{"Go"})
	})
	t.Run("a quoted phrase is adjacent", func(t *testing.T) {
		eq(t, match(t, `"query builder"`), []string{"Go"})
	})

	// The whole reason for websearch over to_tsquery: input that is not valid
	// tsquery syntax is parsed leniently rather than raising an error. A
	// dangling operator would make to_tsquery fail; here it just matches
	// nothing.
	t.Run("malformed input does not error", func(t *testing.T) {
		got, err := ftArticles.With(db).Where(ftArticles.Body.Matches("the quick & )(")).All(ctx)
		if err != nil {
			t.Fatalf("Matches with malformed input error = %v, want it handled", err)
		}
		if len(got) != 0 {
			t.Errorf("matched %v, want nothing for tokens no ftArticle has", titles(got))
		}
	})
}
