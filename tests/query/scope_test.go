package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// scopeCalls counts DefaultScope calls on ScopeCountingModel. The package
// level declaration below already ran by the time any test starts, so a
// nonzero count at the start of the test proves nothing: what matters is
// that it is still zero then, which is what
// TestDefineTable_ScoperStashedNotCalledDuringDeclaration checks.
var scopeCalls int

type ScopeCounting struct{ ID int }

type ScopeCountingModel struct {
	orm.Table[ScopeCounting]
	ID *orm.IntColumn
}

func (m *ScopeCountingModel) DefaultScope() orm.Predicate {
	scopeCalls++
	return m.ID.Gt(0)
}

var ScopeCountingTable = orm.DefineTable[ScopeCounting]("scope_counting",
	func(t *orm.TableBuilder[ScopeCounting]) *ScopeCountingModel {
		return &ScopeCountingModel{
			Table: t.Table(),
			ID:    t.Int("id").PrimaryKey(),
		}
	})

// DefaultScope is read lazily, the same way Relations is, so a model whose
// scope reaches for another table is safe to declare regardless of package
// level variable initialisation order.
func TestDefineTable_ScoperStashedNotCalledDuringDeclaration(t *testing.T) {
	if scopeCalls != 0 {
		t.Fatalf("DefaultScope was called %d times before any query ran; "+
			"DefineTable must stash Scoper rather than call it", scopeCalls)
	}

	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	if _, err := ScopeCountingTable.With(db).All(context.Background()); err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if scopeCalls != 1 {
		t.Errorf("DefaultScope was called %d times by the first query, want 1", scopeCalls)
	}

	if _, err := ScopeCountingTable.With(db).All(context.Background()); err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if scopeCalls != 1 {
		t.Errorf("DefaultScope was called %d times after a second query, want the "+
			"cached predicate to be reused rather than DefaultScope called again", scopeCalls)
	}
}

func TestScoper_AppliesToSelect(t *testing.T) {
	sql, args, err := ScopedPosts.With(pg()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "author_id", "title", "published" FROM "scoped_posts" WHERE "published" = $1`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != true {
		t.Errorf("args = %v, want [true]", args)
	}
}

func TestScoper_AppliesToCount(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{int64(2)})
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := ScopedPosts.With(db).Count(context.Background())
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if n != 2 {
		t.Errorf("Count() = %d, want 2", n)
	}
	want := `SELECT COUNT(*) FROM "scoped_posts" WHERE "published" = $1`
	if got := c.QueryCalls()[0]; got != want {
		t.Errorf("Count ran  %s\nwant       %s", got, want)
	}
}

func TestScoper_AppliesToExists(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, 1, "hi", true})
	db := orm.NewDB(c, postgres.Dialect{})

	ok, err := ScopedPosts.With(db).Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !ok {
		t.Error("Exists() = false, want true")
	}
	got := c.QueryCalls()[0]
	if !strings.Contains(got, `WHERE "published" = $1`) {
		t.Errorf("Exists ran %s, want the default scope in its WHERE", got)
	}
}

func TestScoper_AppliesToUserFilterToo(t *testing.T) {
	sql, args, err := ScopedPosts.With(pg()).Where(ScopedPosts.Title.Eq("hello")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "author_id", "title", "published" FROM "scoped_posts" ` +
		`WHERE ("title" = $1 AND "published" = $2)`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 2 || args[0] != "hello" || args[1] != true {
		t.Errorf("args = %v, want [hello true]", args)
	}
}

func TestScoper_Unscoped(t *testing.T) {
	sql, args, err := ScopedPosts.With(pg()).Unscoped().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "author_id", "title", "published" FROM "scoped_posts"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none", args)
	}
}

func TestScoper_UnscopedKeepsUserFilter(t *testing.T) {
	sql, args, err := ScopedPosts.With(pg()).Where(ScopedPosts.Title.Eq("hi")).Unscoped().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "author_id", "title", "published" FROM "scoped_posts" WHERE "title" = $1`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != "hi" {
		t.Errorf("args = %v, want [hi]", args)
	}
}

func TestScoper_UpdateAllRespectsScope(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := ScopedPosts.With(db).Where(ScopedPosts.Title.Eq("x")).
		UpdateAll(context.Background(), ScopedPosts.Title.Set("y")); err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	want := `UPDATE "scoped_posts" SET "title" = $1 WHERE ("title" = $2 AND "published" = $3)`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
}

func TestScoper_DeleteAllRespectsScope(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := ScopedPosts.With(db).Where(ScopedPosts.Title.Eq("x")).
		DeleteAll(context.Background()); err != nil {
		t.Fatalf("DeleteAll() error = %v", err)
	}
	want := `DELETE FROM "scoped_posts" WHERE ("title" = $1 AND "published" = $2)`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("DeleteAll ran  %s\nwant           %s", got, want)
	}
}

func TestScoper_UnscopedSetOps(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := ScopedPosts.With(db).Where(ScopedPosts.Title.Eq("x")).Unscoped().
		DeleteAll(context.Background()); err != nil {
		t.Fatalf("DeleteAll() error = %v", err)
	}
	want := `DELETE FROM "scoped_posts" WHERE "title" = $1`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("DeleteAll ran  %s\nwant           %s", got, want)
	}
}

// The guard against writing a whole table by accident looks only at what
// the caller wrote. The table's own default scope must never be able to
// satisfy it on the caller's behalf: that would turn "I forgot to filter"
// into a silent success on exactly the tables where a mass write is worst.
func TestScoper_RequireFilterIgnoresImplicitScope(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	tests := []struct {
		name string
		run  func() (int64, error)
		verb string
	}{
		{
			name: "DeleteAll with no arguments",
			run:  func() (int64, error) { return ScopedPosts.With(db).Where().DeleteAll(context.Background()) },
			verb: "delete",
		},
		{
			name: "a condition that compiles to nothing",
			run: func() (int64, error) {
				return ScopedPosts.With(db).Where(orm.And()).DeleteAll(context.Background())
			},
			verb: "delete",
		},
		{
			name: "UpdateAll with an empty slice",
			run: func() (int64, error) {
				var none []orm.Predicate
				return ScopedPosts.With(db).Where(none...).
					UpdateAll(context.Background(), ScopedPosts.Title.Set("y"))
			},
			verb: "update",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := tt.run()
			if err == nil {
				t.Fatal("no error, want the empty filter to be rejected " +
					"despite the table's own default scope")
			}
			if n != 0 {
				t.Errorf("reported %d rows for a statement that never ran", n)
			}
			if !strings.Contains(err.Error(), "Where added no condition") {
				t.Errorf("error %q does not say the filter added no condition", err)
			}
			if !strings.Contains(err.Error(), tt.verb) {
				t.Errorf("error %q does not mention %q", err, tt.verb)
			}
		})
	}
}

// Unscoped is a scalar flag, so the ordinary shallow clone already keeps a
// branch's Unscoped from leaking back to the query it was called on; this
// pins that down the same way immutability_test.go does for every other
// builder, on a fixture where the change is actually observable in SQL.
func TestScoper_UnscopedLeavesOriginalAlone(t *testing.T) {
	base := ScopedPosts.With(pg()).Where(ScopedPosts.Title.Eq("x"))
	want, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}

	narrowed, _, err := base.Unscoped().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if narrowed == want {
		t.Error("Unscoped did not narrow anything")
	}

	got, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if got != want {
		t.Errorf("Unscoped changed the query it was called on:\n got %s\nwant %s", got, want)
	}
}

// scopedAuthor and scopedPost build the queued rows in each table's column
// order, matching author and book in load_test.go.
func scopedAuthor(id int, name string) []any { return []any{id, name} }
func scopedPost(id, authorID int, title string, published bool) []any {
	return []any{id, authorID, title, published}
}

// A related table's default scope applies inside an eager load exactly as
// it would if the related table were queried directly: an unpublished post
// never arrives in Posts, the same as it would never arrive from
// ScopedPosts.With(db).All(ctx).
func TestScoper_AppliesInsideLoad(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(scopedAuthor(1, "alice"))
	c.QueueRows(scopedPost(10, 1, "first", true))
	db := orm.NewDB(c, postgres.Dialect{})

	authors, err := ScopedAuthors.With(db).Load(ScopedAuthors.Posts).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(authors) != 1 {
		t.Fatalf("All() returned %d rows, want 1", len(authors))
	}

	calls := c.QueryCalls()
	if len(calls) != 2 {
		t.Fatalf("ran %d statements, want 2:\n%v", len(calls), calls)
	}
	want := `SELECT "id", "author_id", "title", "published" FROM "scoped_posts" ` +
		`WHERE ("author_id" IN ($1) AND "published" = $2)`
	if calls[1] != want {
		t.Errorf("children ran  %s\nwant          %s", calls[1], want)
	}
	if args := c.QueryArgs(1); len(args) != 2 || args[0] != 1 || args[1] != true {
		t.Errorf("children bound %v, want [1 true]", args)
	}
}

// Unscoped on the query that starts a Load reaches into it too, the same
// way it reaches into the table it was called on directly.
func TestScoper_UnscopedAppliesInsideLoad(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(scopedAuthor(1, "alice"))
	c.QueueRows(scopedPost(10, 1, "first", false))
	db := orm.NewDB(c, postgres.Dialect{})

	authors, err := ScopedAuthors.With(db).Unscoped().Load(ScopedAuthors.Posts).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(authors) != 1 {
		t.Fatalf("All() returned %d rows, want 1", len(authors))
	}

	want := `SELECT "id", "author_id", "title", "published" FROM "scoped_posts" WHERE "author_id" IN ($1)`
	if got := c.QueryCalls()[1]; got != want {
		t.Errorf("children ran  %s\nwant          %s", got, want)
	}
	if len(authors[0].Posts) != 1 {
		t.Errorf("alice has %d posts, want the unpublished one included", len(authors[0].Posts))
	}
}

// Has and HasNone ask about the related table's rows, so they see the same
// rows a direct query over it would: the default scope applies even when
// the caller gave Has no conditions of its own.
func TestScoper_AppliesInsideHas(t *testing.T) {
	sql, args, err := ScopedAuthors.With(pg()).Where(orm.Has(ScopedAuthors.Posts)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "name" FROM "scoped_authors" WHERE EXISTS (SELECT 1 FROM "scoped_posts" ` +
		`WHERE "scoped_posts"."author_id" = "scoped_authors"."id" AND "scoped_posts"."published" = $1)`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != true {
		t.Errorf("args = %v, want [true]", args)
	}
}

func TestScoper_UnscopedAppliesInsideHas(t *testing.T) {
	sql, args, err := ScopedAuthors.With(pg()).Unscoped().Where(orm.Has(ScopedAuthors.Posts)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "name" FROM "scoped_authors" WHERE EXISTS (SELECT 1 FROM "scoped_posts" ` +
		`WHERE "scoped_posts"."author_id" = "scoped_authors"."id")`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none", args)
	}
}

// HasNone asks the same question about the related rows as Has, so it
// carries the same scope.
func TestScoper_AppliesInsideHasNone(t *testing.T) {
	sql, _, err := ScopedAuthors.With(pg()).Where(orm.HasNone(ScopedAuthors.Posts)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "name" FROM "scoped_authors" WHERE NOT EXISTS (SELECT 1 FROM "scoped_posts" ` +
		`WHERE "scoped_posts"."author_id" = "scoped_authors"."id" AND "scoped_posts"."published" = $1)`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// A many to many's far table carries its own scope into Has the same way a
// direct relationship's does, even with no conditions of the caller's own:
// this is what exercises existsThrough's far EXISTS being written for the
// scope alone.
//
// The join table's own hop carries no scope of its own, by design (see
// existsThrough's comment): only its correlation to the outer row appears
// in its EXISTS, nothing from ScopedPostTags. Whatever scope the far table
// declares is what reaches through, never the join table's.
func TestScoper_AppliesInsideHasThroughManyToMany(t *testing.T) {
	sql, args, err := ScopedPosts.With(pg()).Where(orm.Has(ScopedPosts.Tags)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "author_id", "title", "published" FROM "scoped_posts" WHERE (` +
		`EXISTS (SELECT 1 FROM "scoped_post_tags" WHERE "scoped_post_tags"."post_id" = "scoped_posts"."id" ` +
		`AND EXISTS (SELECT 1 FROM "scoped_tags" WHERE "scoped_tags"."id" = "scoped_post_tags"."tag_id" ` +
		`AND "scoped_tags"."active" = $1)) AND "published" = $2)`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 2 || args[0] != true || args[1] != true {
		t.Errorf("args = %v, want [true true]", args)
	}
}

// Unscoped on the outer query reaches through the many to many too.
func TestScoper_UnscopedAppliesInsideHasThroughManyToMany(t *testing.T) {
	sql, _, err := ScopedPosts.With(pg()).Unscoped().Where(orm.Has(ScopedPosts.Tags)).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "id", "author_id", "title", "published" FROM "scoped_posts" WHERE ` +
		`EXISTS (SELECT 1 FROM "scoped_post_tags" WHERE "scoped_post_tags"."post_id" = "scoped_posts"."id")`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}
