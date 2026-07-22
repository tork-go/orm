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
