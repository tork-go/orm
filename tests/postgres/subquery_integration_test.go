//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

type qAuthor struct {
	ID   int
	Name string
}

type qAuthorModel struct {
	orm.Table[qAuthor]
	ID   *orm.IntColumn
	Name *orm.StringColumn
}

var qAuthors = orm.DefineTable[qAuthor]("q_authors", func(t *orm.TableBuilder[qAuthor]) *qAuthorModel {
	return &qAuthorModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
	}
})

// EditorID is nullable on purpose: it is what makes NOT IN's NULL trap
// reachable, and so what NonNull has to be proved against.
type qBook struct {
	ID       int
	AuthorID int
	EditorID *int
	Title    string
}

type qBookModel struct {
	orm.Table[qBook]
	ID       *orm.IntColumn
	AuthorID *orm.IntColumn
	EditorID *orm.NullableIntColumn
	Title    *orm.StringColumn
}

var qBooks = orm.DefineTable[qBook]("q_books", func(t *orm.TableBuilder[qBook]) *qBookModel {
	return &qBookModel{
		Table:    t.Table(),
		ID:       t.Int("id").PrimaryKey(),
		AuthorID: t.Int("author_id").NotNull().References(qAuthors.ID),
		EditorID: t.NullableInt("editor_id").References(qAuthors.ID),
		Title:    t.String("title").NotNull(),
	}
})

// The statements a subquery builds are checked against a fake dialect
// elsewhere. This runs them against the database they were written for, which
// is the only place the semantics can be proved rather than asserted: that a
// correlated-looking IN is not correlated, that Postgres accepts ORDER BY and
// LIMIT inside one, and above all that NOT IN really does return nothing when
// the subquery yields a NULL, which is what NonNull exists to prevent.
func TestSubqueries_AgainstPostgres(t *testing.T) {
	ctx := context.Background()
	dialect := postgres.Dialect{}

	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	const drop = `DROP TABLE IF EXISTS q_books, q_authors CASCADE`
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), drop) })
	if _, err := conn.Exec(ctx, drop); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	desired, err := schema.ExtractSchema(qAuthors, qBooks)
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

	// alice and bob write; carol does not. bob edits, and two of the three
	// books have no editor, so the editor subquery yields a NULL.
	authors := []*qAuthor{{Name: "alice"}, {Name: "bob"}, {Name: "carol"}}
	if err := qAuthors.With(db).InsertMany(ctx, authors...); err != nil {
		t.Fatalf("InsertMany authors: %v", err)
	}
	alice, bob := authors[0], authors[1]

	books := []*qBook{
		{AuthorID: alice.ID, EditorID: &bob.ID, Title: "first"},
		{AuthorID: alice.ID, Title: "second"},
		{AuthorID: bob.ID, Title: "third"},
	}
	if err := qBooks.With(db).InsertMany(ctx, books...); err != nil {
		t.Fatalf("InsertMany books: %v", err)
	}

	names := func(t *testing.T, rows []*qAuthor) []string {
		t.Helper()
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = r.Name
		}
		return out
	}
	equal := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	t.Run("in", func(t *testing.T) {
		got, err := qAuthors.With(db).
			Where(qAuthors.ID.InQuery(orm.Select(qBooks.With(db), qBooks.AuthorID))).
			OrderBy(qAuthors.Name.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if want := []string{"alice", "bob"}; !equal(names(t, got), want) {
			t.Errorf("All() = %v, want %v", names(t, got), want)
		}
	})

	t.Run("not in", func(t *testing.T) {
		got, err := qAuthors.With(db).
			Where(qAuthors.ID.NotInQuery(orm.Select(qBooks.With(db), qBooks.AuthorID))).
			OrderBy(qAuthors.Name.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if want := []string{"carol"}; !equal(names(t, got), want) {
			t.Errorf("All() = %v, want %v", names(t, got), want)
		}
	})

	t.Run("a narrowed subquery", func(t *testing.T) {
		got, err := qAuthors.With(db).Where(qAuthors.ID.InQuery(orm.Select(
			qBooks.With(db).Where(qBooks.Title.Equals("third")), qBooks.AuthorID))).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if want := []string{"bob"}; !equal(names(t, got), want) {
			t.Errorf("All() = %v, want %v", names(t, got), want)
		}
	})

	// Postgres accepts both inside an IN, which is worth knowing rather than
	// assuming: neither is meaningful in the set the subquery denotes, but a
	// LIMIT genuinely changes which rows reach it.
	t.Run("an ordered and capped subquery", func(t *testing.T) {
		got, err := qAuthors.With(db).Where(qAuthors.ID.InQuery(orm.Select(
			qBooks.With(db).OrderBy(qBooks.ID.Asc()).Limit(1), qBooks.AuthorID))).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if want := []string{"alice"}; !equal(names(t, got), want) {
			t.Errorf("All() = %v, want %v", names(t, got), want)
		}
	})

	t.Run("a distinct subquery", func(t *testing.T) {
		got, err := qAuthors.With(db).Where(qAuthors.ID.InQuery(
			orm.Select(qBooks.With(db), qBooks.AuthorID).Distinct())).
			OrderBy(qAuthors.Name.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if want := []string{"alice", "bob"}; !equal(names(t, got), want) {
			t.Errorf("All() = %v, want %v", names(t, got), want)
		}
	})

	// The trap, demonstrated rather than described: two of the three books
	// have no editor, so the subquery yields a NULL and NOT IN is never true.
	// Every author is excluded, including the two who plainly are not editors.
	t.Run("NOT IN over a nullable column really does match nothing", func(t *testing.T) {
		rows, err := conn.Query(ctx,
			`SELECT count(*) FROM q_authors WHERE id NOT IN (SELECT editor_id FROM q_books)`)
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatal("count returned no row")
		}
		var n int64
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		if n != 0 {
			t.Fatalf("count = %d, want 0; the NULL trap this test documents is gone, "+
				"and NonNull's reason for existing with it", n)
		}
	})

	t.Run("NonNull disarms it", func(t *testing.T) {
		editors := orm.Select(qBooks.With(db), qBooks.EditorID)

		got, err := qAuthors.With(db).Where(qAuthors.ID.NotInQuery(orm.NonNull(editors))).
			OrderBy(qAuthors.Name.Asc()).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if want := []string{"alice", "carol"}; !equal(names(t, got), want) {
			t.Errorf("All() = %v, want everyone who is not an editor, %v",
				names(t, got), want)
		}

		// And the positive form agrees about who the editors are.
		got, err = qAuthors.With(db).Where(qAuthors.ID.InQuery(orm.NonNull(editors))).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if want := []string{"bob"}; !equal(names(t, got), want) {
			t.Errorf("All() = %v, want %v", names(t, got), want)
		}
	})

	t.Run("a nullable column matched against a subquery", func(t *testing.T) {
		got, err := qBooks.With(db).Where(qBooks.EditorID.InQuery(
			orm.Select(qAuthors.With(db).Where(qAuthors.Name.Equals("bob")), qAuthors.ID))).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 1 || got[0].Title != "first" {
			t.Errorf("All() = %+v, want the one book bob edited", got)
		}
	})

	t.Run("nested subqueries", func(t *testing.T) {
		// The books of the authors who wrote something titled "third".
		inner := orm.Select(qBooks.With(db).Where(qBooks.Title.Equals("third")), qBooks.AuthorID)
		got, err := qBooks.With(db).Where(qBooks.AuthorID.InQuery(
			orm.Select(qAuthors.With(db).Where(qAuthors.ID.InQuery(inner)), qAuthors.ID))).
			All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if len(got) != 1 || got[0].Title != "third" {
			t.Errorf("All() = %+v, want bob's one book", got)
		}
	})

	t.Run("beside other conditions, with the arguments in order", func(t *testing.T) {
		got, err := qAuthors.With(db).Where(
			qAuthors.Name.NotEquals("bob"),
			qAuthors.ID.InQuery(orm.Select(
				qBooks.With(db).Where(qBooks.Title.NotEquals("nothing")), qBooks.AuthorID)),
		).All(ctx)
		if err != nil {
			t.Fatalf("All() error = %v", err)
		}
		if want := []string{"alice"}; !equal(names(t, got), want) {
			t.Errorf("All() = %v, want %v", names(t, got), want)
		}
	})

	// Last, since it changes what the rows are.
	t.Run("in front of a write", func(t *testing.T) {
		n, err := qAuthors.With(db).Where(qAuthors.ID.NotInQuery(
			orm.Select(qBooks.With(db), qBooks.AuthorID))).DeleteAll(ctx)
		if err != nil {
			t.Fatalf("DeleteAll() error = %v", err)
		}
		if n != 1 {
			t.Fatalf("DeleteAll() = %d, want carol alone", n)
		}
		left, err := qAuthors.With(db).Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if left != 2 {
			t.Errorf("Count() = %d, want the two authors who write", left)
		}
	})
}
