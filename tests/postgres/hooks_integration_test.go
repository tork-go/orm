//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type article struct {
	ID    int
	Title string
	Slug  string
	Views int
}

type articleModel struct {
	orm.Table[article]
	ID    *orm.IntColumn
	Title *orm.StringColumn
	Slug  *orm.StringColumn
	Views *orm.IntColumn
}

var articles = orm.DefineTable[article]("h_articles", func(t *orm.TableBuilder[article]) *articleModel {
	return &articleModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Title: t.String("title").NotNull().MaxLen(200),
		Slug:  t.String("slug").NotNull().MaxLen(200),
		Views: t.Int("views").NotNull(),
	}
})

// The hook this whole query API was built to make possible.
func (a *article) BeforeCreate(context.Context) error {
	a.Slug = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(a.Title), " ", "-"))
	return nil
}

func (a *article) BeforeUpdate(context.Context) error {
	a.Slug = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(a.Title), " ", "-"))
	return nil
}

// A hook mutating the row in memory proves nothing on its own. What
// matters is that the mutation reached the database, which only reading
// the stored row back through a separate query can show.
func TestHooks_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS h_articles CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(articles)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	ops, _ := migrate.Diff(schema.Schema{}, desired)
	ddl, err := migrate.Generate(dialect, ops)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if _, err := conn.Exec(ctx, ddl); err != nil {
		t.Fatalf("applying schema failed: %v", err)
	}

	db := orm.NewDB(conn, dialect)

	t.Run("the slug is derived and stored", func(t *testing.T) {
		a := &article{Title: "Hello There World"}
		if err := articles.With(db).Insert(ctx, a); err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
		if a.Slug != "hello-there-world" {
			t.Fatalf("Slug = %q in memory, want the derived slug", a.Slug)
		}

		// Read it back rather than trust the row in hand: this is what
		// proves the hook ran before the values were bound.
		stored, err := articles.With(db).Find(ctx, a.ID)
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}
		if stored.Slug != "hello-there-world" {
			t.Errorf("stored slug = %q, want the derived slug", stored.Slug)
		}
	})

	t.Run("the slug is rederived on update", func(t *testing.T) {
		a := &article{Title: "First Title"}
		if err := articles.With(db).Insert(ctx, a); err != nil {
			t.Fatalf("Insert() error = %v", err)
		}

		a.Title = "A Different Title"
		if err := articles.With(db).Update(ctx, a); err != nil {
			t.Fatalf("Update() error = %v", err)
		}

		stored, err := articles.With(db).Find(ctx, a.ID)
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}
		if stored.Slug != "a-different-title" {
			t.Errorf("stored slug = %q, want it rederived from the new title", stored.Slug)
		}
	})

	t.Run("save fires the operation it ran", func(t *testing.T) {
		a := &article{Title: "Saved Article"}
		if err := articles.With(db).Save(ctx, a); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		stored, err := articles.With(db).Find(ctx, a.ID)
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}
		if stored.Slug != "saved-article" {
			t.Errorf("stored slug = %q, want the create hook to have run", stored.Slug)
		}
	})
}
