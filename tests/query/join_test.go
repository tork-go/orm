package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// A BelongsTo joins onto the table its foreign key references.
func TestJoin_InnerRenders(t *testing.T) {
	sql, _, err := Books.With(pg()).Join(Books.Author).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "books"."id", "books"."author_id", "books"."title" FROM "books" ` +
		`JOIN "authors" ON "authors"."id" = "books"."author_id"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// A HasMany joins the other direction: the foreign key lives on the
// related table, not the declaring one.
func TestJoin_HasMany(t *testing.T) {
	sql, _, err := Authors.With(pg()).Join(Authors.Books).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "authors"."id", "authors"."name" FROM "authors" ` +
		`JOIN "books" ON "books"."author_id" = "authors"."id"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

func TestJoin_LeftRenders(t *testing.T) {
	sql, _, err := Authors.With(pg()).LeftJoin(Authors.Desk).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "authors"."id", "authors"."name" FROM "authors" ` +
		`LEFT JOIN "desks" ON "desks"."author_id" = "authors"."id"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// Filtering on the joined table's own column, referenced through its own
// model var, is what makes Join useful for anything beyond existence.
func TestJoin_WhereOnJoinedColumn(t *testing.T) {
	sql, args, err := Books.With(pg()).Join(Books.Author).Where(Authors.Name.Eq("Le Guin")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "books"."id", "books"."author_id", "books"."title" FROM "books" ` +
		`JOIN "authors" ON "authors"."id" = "books"."author_id" WHERE "authors"."name" = $1`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != "Le Guin" {
		t.Errorf("args = %v, want [Le Guin]", args)
	}
}

func TestJoin_OrderByJoinedColumn(t *testing.T) {
	sql, _, err := Books.With(pg()).Join(Books.Author).OrderBy(Authors.Name.Asc()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `ORDER BY "authors"."name" ASC`) {
		t.Errorf("SQL() = %s, want it ordered by the joined column", sql)
	}
}

// JoinOn's extra conditions land in the ON clause, not the WHERE clause.
func TestJoinOn_ExtraConditionsLandOnON(t *testing.T) {
	sql, args, err := Books.With(pg()).JoinOn(Books.Author, Authors.Name.Eq("Le Guin")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "books"."id", "books"."author_id", "books"."title" FROM "books" ` +
		`JOIN "authors" ON "authors"."id" = "books"."author_id" AND "authors"."name" = $1`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != "Le Guin" {
		t.Errorf("args = %v, want [Le Guin]", args)
	}
}

// LeftJoinOn is where JoinOn's own ON-vs-WHERE distinction actually
// matters: the extra condition lands on the ON clause, so a primary row
// with no matching related row still comes back rather than being dropped
// the way a WHERE condition would drop it.
func TestLeftJoinOn_ExtraConditionsLandOnON(t *testing.T) {
	sql, args, err := Authors.With(pg()).LeftJoinOn(Authors.Desk, Desks.Colour.Eq("red")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "authors"."id", "authors"."name" FROM "authors" ` +
		`LEFT JOIN "desks" ON "desks"."author_id" = "authors"."id" AND "desks"."colour" = $1`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != "red" {
		t.Errorf("args = %v, want [red]", args)
	}
}

// LeftJoinOn off an unfiltered query goes through the same Query forwarder
// every other join call does.
func TestLeftJoinOn_FromQuery(t *testing.T) {
	sql, _, err := Authors.With(pg()).LeftJoinOn(Authors.Desk, Desks.Colour.Eq("red")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `LEFT JOIN "desks" ON "desks"."author_id" = "authors"."id" AND "desks"."colour" = $1`) {
		t.Errorf("SQL() = %s, want the extra condition on the ON clause of a LEFT JOIN", sql)
	}
}

// A many to many needs two joins through a join table, which Join does not
// attempt: Has/HasNone already answer the question without the row
// multiplication a join brings.
func TestJoin_ManyToManyRejected(t *testing.T) {
	_, _, err := Books.With(pg()).Join(Books.Tags).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a many to many join rejected")
	}
	if !strings.Contains(err.Error(), "many to many") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestJoin_NilRelationship(t *testing.T) {
	_, _, err := Books.With(pg()).Join(nil).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a nil relationship rejected")
	}
	if !strings.Contains(err.Error(), "no relationship") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// A relationship that cannot be resolved at all — nothing references the
// declaring table, so there is no key to join on — is reported the same
// way Has already reports one.
func TestJoin_UnresolvableRelationshipRejected(t *testing.T) {
	_, _, err := Unjoinable.With(pg()).Join(Unjoinable.Books).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the unresolvable relationship rejected")
	}
	if !strings.Contains(err.Error(), "no column on") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// JoinOn's extra conditions are ordinary predicates, checked the same way
// any other predicate over this statement's tables is.
func TestJoinOn_ExtraConditionForeignColumnRejected(t *testing.T) {
	_, _, err := Books.With(pg()).JoinOn(Books.Author, Users.Username.Eq("x")).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign condition rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "users"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

// A column with no owner — never bound to a table by DefineTable — is
// still accepted inside a joined statement, the same as it already is
// outside one. Qualified, it has nothing more specific to qualify against
// than the primary table, so that is what it gets.
func TestJoin_UnboundColumnStillRenders(t *testing.T) {
	unbound := orm.NewColumn[string]("nickname")
	sql, _, err := Books.With(pg()).Join(Books.Author).
		Where(orm.Comparison{Col: unbound, Op: orm.OpEq, Value: "x"}).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `WHERE "books"."nickname" = $1`) {
		t.Errorf("SQL() = %s, want the unbound column qualified against the primary table", sql)
	}
}

// A relationship belonging to a different table than the one being queried
// is rejected the same way Has already rejects one.
func TestJoin_ForeignRelationshipRejected(t *testing.T) {
	_, _, err := Authors.With(pg()).Join(Books.Author).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the mismatched relationship rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "books"`) {
		t.Errorf("error %q does not name the relationship's real table", err)
	}
}

// Once a Join is present, Select is restricted to the primary table's own
// columns: a foreign one has no entry in this row type's field mapping,
// and scanning into it would corrupt the read rather than merely fail.
func TestJoin_SelectRejectsForeignColumn(t *testing.T) {
	_, _, err := Books.With(pg()).Join(Books.Author).Select(Authors.Name).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign column rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "authors"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

// The pre-existing rejection of another table's column in Select, from
// before Join existed, is unaffected: it never went through a Join.
func TestSelect_StillRejectsForeignColumnWithoutJoin(t *testing.T) {
	_, _, err := Users.With(pg()).Select(Posts.Title).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign column rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "posts"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

// Count after a Join counts joined rows, documented rather than silently
// fixed with an implicit DISTINCT.
func TestJoin_CountFansOut(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{int64(3)})
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Books.With(db).Join(Books.Author).Count(context.Background()); err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	want := `SELECT COUNT(*) FROM "books" JOIN "authors" ON "authors"."id" = "books"."author_id"`
	if got := c.QueryCalls()[0]; got != want {
		t.Errorf("Count ran %s\nwant       %s", got, want)
	}
}

// Count builds its own compiler independently of SQL/All, and rejects a
// bad Join the same way they do.
func TestJoin_CountManyToManyRejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := Books.With(db).Join(Books.Tags).Count(context.Background())
	if err == nil {
		t.Fatal("Count() error = nil, want the many to many join rejected")
	}
	if !strings.Contains(err.Error(), "many to many") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestJoin_CountDistinctFansOutToo(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{int64(3)})
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Books.With(db).Join(Books.Author).Distinct().Count(context.Background()); err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	want := `SELECT COUNT(*) FROM (SELECT DISTINCT "books"."id", "books"."author_id", "books"."title" ` +
		`FROM "books" JOIN "authors" ON "authors"."id" = "books"."author_id") AS "t"`
	if got := c.QueryCalls()[0]; got != want {
		t.Errorf("Count ran %s\nwant       %s", got, want)
	}
}

// UpdateAll and DeleteAll reject a Join outright: no dialect Tork targets
// writes a portable UPDATE or DELETE with a JOIN in it.
func TestJoin_UpdateAllRejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := Books.With(db).Join(Books.Author).UpdateAll(context.Background(), Books.Title.Set("x"))
	if err == nil {
		t.Fatal("UpdateAll() error = nil, want the Join rejected")
	}
	if !strings.Contains(err.Error(), "a Join or LeftJoin") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestJoin_DeleteAllRejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := Books.With(db).Join(Books.Author).DeleteAll(context.Background())
	if err == nil {
		t.Fatal("DeleteAll() error = nil, want the Join rejected")
	}
	if !strings.Contains(err.Error(), "a Join or LeftJoin") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// orm.Select, CountBy/SumBy/etc, and the scalar aggregates all reject a
// Join too: SelectAs is the extension point for reading a joined
// statement's columns, not these.
func TestJoin_ScalarSelectRejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, _, err := orm.Select(Books.With(db).Join(Books.Author), Books.Title).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the Join rejected")
	}
	if !strings.Contains(err.Error(), "a Join or LeftJoin") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestJoin_ScalarCountRejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := orm.Select(Books.With(db).Join(Books.Author), Books.Title).Count(context.Background())
	if err == nil {
		t.Fatal("Count() error = nil, want the Join rejected")
	}
	if !strings.Contains(err.Error(), "a Join or LeftJoin") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestJoin_GroupedRejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, _, err := orm.CountBy(Books.With(db).Join(Books.Author), Books.Title).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the Join rejected")
	}
	if !strings.Contains(err.Error(), "a Join or LeftJoin") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestJoin_AggregateRejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := orm.Max(context.Background(), Books.With(db).Join(Books.Author), Books.Title)
	if err == nil {
		t.Fatal("Max() error = nil, want the Join rejected")
	}
	if !strings.Contains(err.Error(), "a Join or LeftJoin") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// Query's forwarders reach Join/LeftJoin/JoinOn the same way they reach
// every other builder.
func TestJoin_FromQuery(t *testing.T) {
	sql, _, err := Books.With(pg()).Join(Books.Author).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `JOIN "authors"`) {
		t.Errorf("SQL() = %s, want a JOIN", sql)
	}
}

func TestLeftJoin_FromQuery(t *testing.T) {
	sql, _, err := Authors.With(pg()).LeftJoin(Authors.Desk).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `LEFT JOIN "desks"`) {
		t.Errorf("SQL() = %s, want a LEFT JOIN", sql)
	}
}

func TestJoinOn_FromQuery(t *testing.T) {
	sql, _, err := Books.With(pg()).JoinOn(Books.Author, Authors.Name.Eq("x")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `AND "authors"."name" = $1`) {
		t.Errorf("SQL() = %s, want the extra condition on the ON clause", sql)
	}
}

// Join is a scalar-shaped builder call (it appends one joinSpec), so the
// ordinary clone already keeps a branch's Join from leaking back to the
// query it was called on.
func TestJoin_LeavesOriginalAlone(t *testing.T) {
	base := Books.With(pg())
	want, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}

	narrowed, _, err := base.Join(Books.Author).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if narrowed == want {
		t.Error("Join did not narrow anything")
	}

	got, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if got != want {
		t.Errorf("Join changed the query it was called on:\n got %s\nwant %s", got, want)
	}
}
