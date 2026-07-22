//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type cDoc struct {
	ID      int
	Title   string
	Version int
}

type cDocModel struct {
	orm.Table[cDoc]
	ID      *orm.IntColumn
	Title   *orm.StringColumn
	Version *orm.IntColumn
}

var cDocs = orm.DefineTable[cDoc]("c_docs", func(t *orm.TableBuilder[cDoc]) *cDocModel {
	return &cDocModel{
		Table:   t.Table(),
		ID:      t.Int("id").PrimaryKey(),
		Title:   t.String("title").NotNull(),
		Version: t.Int("version").NotNull(),
	}
})

// What a conditional write renders as is checked against a fake dialect. What
// it is for can only be shown against a real database: that two writers who
// read the same row do not both get to write it, and that the one who loses
// finds out rather than being quietly overwritten.
func TestConditionalWrites_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}

	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS c_docs CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(cDocs)
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

	seed := func(t *testing.T) *cDoc {
		t.Helper()
		if _, err := conn.Exec(ctx, `TRUNCATE c_docs RESTART IDENTITY`); err != nil {
			t.Fatalf("truncate failed: %v", err)
		}
		doc := &cDoc{Title: "original", Version: 1}
		if err := cDocs.With(db).Insert(ctx, doc); err != nil {
			t.Fatalf("Insert failed: %v", err)
		}
		return doc
	}

	t.Run("the condition holding writes the row", func(t *testing.T) {
		doc := seed(t)

		doc.Title = "edited"
		doc.Version = 2
		if err := cDocs.With(db).UpdateIf(ctx, doc, cDocs.Version.Equals(1)); err != nil {
			t.Fatalf("UpdateIf() error = %v", err)
		}
		got, err := cDocs.With(db).Find(ctx, doc.ID)
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}
		if got.Title != "edited" || got.Version != 2 {
			t.Errorf("stored row is %+v, want the edit", got)
		}
	})

	t.Run("the condition failing writes nothing", func(t *testing.T) {
		doc := seed(t)

		doc.Title = "should not land"
		err := cDocs.With(db).UpdateIf(ctx, doc, cDocs.Version.Equals(99))
		if !errors.Is(err, orm.ErrNoRows) {
			t.Fatalf("UpdateIf() error = %v, want ErrNoRows", err)
		}
		got, err := cDocs.With(db).Find(ctx, doc.ID)
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}
		if got.Title != "original" {
			t.Errorf("stored row is %+v, want it untouched", got)
		}
	})

	// The lost update, and the whole reason for the feature: two writers read
	// version 1, both edit, and only one may win. Without the condition the
	// second write silently replaces the first.
	t.Run("the second writer of a stale row loses and is told", func(t *testing.T) {
		doc := seed(t)

		first, err := cDocs.With(db).Find(ctx, doc.ID)
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}
		second, err := cDocs.With(db).Find(ctx, doc.ID)
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}

		first.Title, first.Version = "first wins", first.Version+1
		if err := cDocs.With(db).UpdateIf(ctx, first, cDocs.Version.Equals(1)); err != nil {
			t.Fatalf("the first writer failed: %v", err)
		}

		second.Title, second.Version = "second wins", second.Version+1
		err = cDocs.With(db).UpdateIf(ctx, second, cDocs.Version.Equals(1))
		if !errors.Is(err, orm.ErrNoRows) {
			t.Fatalf("the second writer got %v, want ErrNoRows", err)
		}

		got, err := cDocs.With(db).Find(ctx, doc.ID)
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}
		if got.Title != "first wins" || got.Version != 2 {
			t.Errorf("stored row is %+v, want the first writer's edit kept", got)
		}
	})

	t.Run("a conditional delete", func(t *testing.T) {
		doc := seed(t)

		if err := cDocs.With(db).DeleteIf(ctx, doc, cDocs.Version.Equals(99)); !errors.Is(err, orm.ErrNoRows) {
			t.Fatalf("DeleteIf() error = %v, want ErrNoRows", err)
		}
		if n, err := cDocs.With(db).Count(ctx); err != nil || n != 1 {
			t.Fatalf("Count() = %d, %v; want the row still there", n, err)
		}
		if err := cDocs.With(db).DeleteIf(ctx, doc, cDocs.Version.Equals(1)); err != nil {
			t.Fatalf("DeleteIf() error = %v", err)
		}
		if n, err := cDocs.With(db).Count(ctx); err != nil || n != 0 {
			t.Fatalf("Count() = %d, %v; want the row gone", n, err)
		}
	})

	// Under real contention: many writers all read version 1 and all try to
	// write version 2. Exactly one may succeed, and the rest must be told.
	t.Run("exactly one of many concurrent writers wins", func(t *testing.T) {
		doc := seed(t)

		const writers = 8
		var (
			mu    sync.Mutex
			won   int
			lost  int
			other error
		)
		var wg sync.WaitGroup
		for w := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mine := &cDoc{ID: doc.ID, Title: "writer", Version: 2}
				err := cDocs.With(db).UpdateIf(ctx, mine, cDocs.Version.Equals(1))

				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					won++
				case errors.Is(err, orm.ErrNoRows):
					lost++
				default:
					other = err
				}
				_ = w
			}()
		}
		wg.Wait()

		if other != nil {
			t.Fatalf("a writer failed for an unexpected reason: %v", other)
		}
		if won != 1 || lost != writers-1 {
			t.Errorf("%d writers won and %d lost, want exactly 1 and %d",
				won, lost, writers-1)
		}
	})
}
