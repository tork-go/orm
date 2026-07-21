//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
	"github.com/tork-go/orm/tests/fixtures"
)

// Each feature has a test of its own. This one asks the different
// question of whether they still compose, by taking a model that uses all
// of them at once all the way to a real database and back.
//
// The round trip is what makes it worth running: the schema is extracted
// from the models, diffed against an empty database, rendered, applied,
// and then read back out of Postgres. Diffing the introspected result
// against the desired one has to come out empty, which it only does if
// every feature survived being written as SQL and recognised again on the
// way back.
func TestRichModel_RoundTripsThroughPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS rich_books, rich_authors CASCADE;
		DROP TYPE IF EXISTS rich_book_status`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(fixtures.Authors, fixtures.Books)
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}

	ops, err := migrate.Diff(schema.Schema{}, desired)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	sql, err := migrate.Generate(dialect, ops)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("applying generated SQL failed: %v\n%s", err, sql)
	}

	got, err := dialect.Introspect(ctx, conn, []string{"rich_authors", "rich_books"})
	if err != nil {
		t.Fatalf("Introspect failed: %v", err)
	}

	// Nothing left to do means the database now matches the models. Any
	// feature lost on the way out or misread on the way back shows up here
	// as an operation.
	back, err := migrate.Diff(got, desired)
	if err != nil {
		t.Fatalf("Diff after introspection failed: %v", err)
	}
	if len(back) != 0 {
		t.Errorf("re-diffing the applied schema produced %d operations, want none:", len(back))
		for _, op := range back {
			t.Errorf("  %#v", op)
		}
	}
}

// The features are worth asserting individually as well, so a failure says
// which one drifted rather than only that something did.
func TestRichModel_FeaturesSurviveIntrospection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS rich_books, rich_authors CASCADE;
		DROP TYPE IF EXISTS rich_book_status`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, _ := schema.ExtractSchema(fixtures.Authors, fixtures.Books)
	ops, _ := migrate.Diff(schema.Schema{}, desired)
	sql, err := migrate.Generate(dialect, ops)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("applying generated SQL failed: %v\n%s", err, sql)
	}

	got, err := dialect.Introspect(ctx, conn, []string{"rich_authors", "rich_books"})
	if err != nil {
		t.Fatalf("Introspect failed: %v", err)
	}

	var books *schema.Table
	for i := range got.Tables {
		if got.Tables[i].Name == "rich_books" {
			books = &got.Tables[i]
		}
	}
	if books == nil {
		t.Fatal("rich_books missing from the introspected schema")
	}

	byName := map[string]schema.Column{}
	for _, c := range books.Columns {
		byName[c.Name] = c
	}

	t.Run("enum column", func(t *testing.T) {
		if got := byName["status"].Type.Kind; got != schema.KindEnum {
			t.Errorf("status kind = %v, want KindEnum", got)
		}
		if got := byName["status"].Type.TypeName; got != "rich_book_status" {
			t.Errorf("status type name = %q, want rich_book_status", got)
		}
	})

	t.Run("array column", func(t *testing.T) {
		typ := byName["tags"].Type
		if typ.Kind != schema.KindArray || typ.Elem == nil || typ.Elem.Kind != schema.KindText {
			t.Errorf("tags type = %+v, want an array of text", typ)
		}
	})

	t.Run("varchar length", func(t *testing.T) {
		typ := byName["title"].Type
		if typ.Kind != schema.KindVarchar || typ.Length != 200 {
			t.Errorf("title type = %+v, want VARCHAR(200)", typ)
		}
	})

	t.Run("field promoted from an embedded struct", func(t *testing.T) {
		if _, ok := byName["book_created_at"]; !ok {
			t.Error("book_created_at missing: the embedded entity field did not reach the database")
		}
	})

	t.Run("nullability", func(t *testing.T) {
		if !byName["pages"].NotNull {
			t.Error("pages is nullable, want NOT NULL")
		}
		if byName["price"].NotNull {
			t.Error("price is NOT NULL, want nullable")
		}
	})

	t.Run("compound unique index", func(t *testing.T) {
		if len(books.Uniques) != 1 {
			t.Fatalf("rich_books has %d unique constraints, want 1", len(books.Uniques))
		}
		u := books.Uniques[0]
		if len(u.Columns) != 2 || u.Columns[0] != "author_id" || u.Columns[1] != "title" {
			t.Errorf("unique columns = %v, want [author_id title]", u.Columns)
		}
	})

	t.Run("check constraint", func(t *testing.T) {
		if len(books.Checks) != 1 {
			t.Fatalf("rich_books has %d checks, want 1", len(books.Checks))
		}
	})

	t.Run("foreign key with a referential action", func(t *testing.T) {
		if len(books.ForeignKeys) != 1 {
			t.Fatalf("rich_books has %d foreign keys, want 1", len(books.ForeignKeys))
		}
		fk := books.ForeignKeys[0]
		if fk.ReferencedTable != "rich_authors" {
			t.Errorf("fk references %q, want rich_authors", fk.ReferencedTable)
		}
		if fk.OnDelete != schema.ActionCascade {
			t.Errorf("fk OnDelete = %v, want ActionCascade", fk.OnDelete)
		}
	})
}
