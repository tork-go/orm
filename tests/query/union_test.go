package query_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

func TestUnion_Renders(t *testing.T) {
	db := pg()
	a := Users.With(db).Select(Users.ID, Users.Username).Where(Users.Age.GreaterThan(18))
	b := Users.With(db).Select(Users.ID, Users.Username).Where(Users.Age.LessThan(13))

	sql, args, err := orm.Union(a, b).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `(SELECT "id", "username" FROM "users" WHERE "age" > $1) UNION ` +
		`(SELECT "id", "username" FROM "users" WHERE "age" < $2)`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 2 || args[0] != 18 || args[1] != 13 {
		t.Errorf("args = %v, want [18 13]", args)
	}
}

func TestUnionAll_Renders(t *testing.T) {
	db := pg()
	a := Users.With(db).Select(Users.ID)
	b := Users.With(db).Select(Users.ID)

	sql, _, err := orm.UnionAll(a, b).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, ") UNION ALL (") {
		t.Errorf("SQL() = %s, want UNION ALL joining the two operands", sql)
	}
}

func TestIntersect_Renders(t *testing.T) {
	db := pg()
	a := Users.With(db).Select(Users.ID)
	b := Users.With(db).Select(Users.ID)

	sql, _, err := orm.Intersect(a, b).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, ") INTERSECT (") {
		t.Errorf("SQL() = %s, want INTERSECT joining the two operands", sql)
	}
}

func TestExcept_Renders(t *testing.T) {
	db := pg()
	a := Users.With(db).Select(Users.ID)
	b := Users.With(db).Select(Users.ID)

	sql, _, err := orm.Except(a, b).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, ") EXCEPT (") {
		t.Errorf("SQL() = %s, want EXCEPT joining the two operands", sql)
	}
}

// The bug this design exists to avoid: each operand compiles with its own
// fresh argBuilder if they are not made to share one, and both would then
// bind their first value as $1, producing a statement where the second
// operand's $1 actually means the first operand's placeholder.
func TestUnion_PlaceholderNumberingContinuesAcrossOperands(t *testing.T) {
	db := pg()
	a := Users.With(db).Where(Users.Age.GreaterThan(18))
	b := Users.With(db).Where(Users.Username.Equals("bob"))

	sql, args, err := orm.Union(a, b).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `"age" > $1`) || !strings.Contains(sql, `"username" = $2`) {
		t.Errorf("SQL() = %s, want placeholders numbered $1 then $2", sql)
	}
	if len(args) != 2 || args[0] != 18 || args[1] != "bob" {
		t.Errorf("args = %v, want [18 bob]", args)
	}
}

// OrderBy and Limit apply once, after both operands are combined, rather
// than to either side.
func TestUnion_OrderByAndLimitAppliedOnce(t *testing.T) {
	db := pg()
	a := Users.With(db).Select(Users.ID, Users.Username)
	b := Users.With(db).Select(Users.ID, Users.Username)

	sql, _, err := orm.Union(a, b).OrderBy(Users.Username.Asc()).Limit(5).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `(SELECT "id", "username" FROM "users") UNION (SELECT "id", "username" FROM "users") ` +
		`ORDER BY "username" ASC LIMIT 5`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// OrderBy is rendered against the left operand's own table, so a column
// belonging to neither operand is rejected the same way Where's is.
func TestUnion_OrderByForeignColumnRejected(t *testing.T) {
	db := pg()
	a := Users.With(db).Where()
	b := Users.With(db).Where()
	_, _, err := orm.Union(a, b).OrderBy(Books.Title.Asc()).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign OrderBy column rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "books"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestUnion_NegativeLimit(t *testing.T) {
	db := pg()
	a := Users.With(db).Where()
	b := Users.With(db).Where()
	_, _, err := orm.Union(a, b).Limit(-1).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the negative limit rejected")
	}
}

func TestUnion_ColumnCountMismatchRejected(t *testing.T) {
	db := pg()
	a := Users.With(db).Select(Users.ID)
	b := Users.With(db).Select(Users.ID, Users.Username)

	_, _, err := orm.Union(a, b).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the column count mismatch rejected")
	}
	if !strings.Contains(err.Error(), "1 column(s)") {
		t.Errorf("error %q does not name the left count", err)
	}
}

func TestUnion_NilQueryRejected(t *testing.T) {
	a := Users.With(pg()).Where()
	_, _, err := orm.Union[User](a, nil).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the nil query rejected")
	}
	if !strings.Contains(err.Error(), "nil query") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// pg() builds a fresh *DB on every call, so two queries each built with
// their own pg() carry different handles and are rejected: a combined
// statement must run as one round trip on one connection.
func TestUnion_DifferentDatabaseHandlesRejected(t *testing.T) {
	a := Users.With(pg()).Where()
	b := Users.With(pg()).Where()
	_, _, err := orm.Union(a, b).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the mismatched database handles rejected")
	}
	if !strings.Contains(err.Error(), "database handle") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestUnion_JoinRejected(t *testing.T) {
	db := pg()
	a := Books.With(db).Join(Books.Author)
	b := Books.With(db).Where()
	_, _, err := orm.Union(a, b).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the Join rejected")
	}
	if !strings.Contains(err.Error(), "Join") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// The right operand is checked the same way the left one is, not only
// the left: compile inspects each in turn.
func TestUnion_RightJoinRejected(t *testing.T) {
	db := pg()
	a := Books.With(db).Where()
	b := Books.With(db).Join(Books.Author)
	_, _, err := orm.Union(a, b).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the right operand's Join rejected")
	}
	if !strings.Contains(err.Error(), "Join") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// readiness is checked for both operands, not only the left.
func TestUnion_RightMissingEntityMappingRejected(t *testing.T) {
	db := pg()
	a := Users.With(db).Where()
	b := orm.NewTable[User]("users_unmapped").With(db).Where()
	_, _, err := orm.Union(a, b).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the right operand's missing entity mapping rejected")
	}
}

func TestUnion_LockRejected(t *testing.T) {
	db := pg()
	a := Users.With(db).Where(Users.ID.GreaterThan(0)).ForUpdate()
	b := Users.With(db).Where()
	_, _, err := orm.Union(a, b).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the lock rejected")
	}
	if !strings.Contains(err.Error(), "lock") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestUnion_PreloadRejected(t *testing.T) {
	db := pg()
	a := Authors.With(db).Load(Authors.Books)
	b := Authors.With(db).Where()
	_, _, err := orm.Union(a, b).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the Preload rejected")
	}
	if !strings.Contains(err.Error(), "Preload") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestUnion_NoEntityMapping(t *testing.T) {
	db := pg()
	a := unmapped().With(db).Where()
	b := unmapped().With(db).Where()
	_, _, err := orm.Union(a, b).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the missing entity mapping rejected")
	}
}

func TestUnion_All(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "alice"}, []any{2, "bob"})
	db := orm.NewDB(c, postgres.Dialect{})

	a := Users.With(db).Select(Users.ID, Users.Username)
	b := Users.With(db).Select(Users.ID, Users.Username)

	rows, err := orm.Union(a, b).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(rows) != 2 || rows[0].Username != "alice" || rows[1].Username != "bob" {
		t.Errorf("rows = %+v, want [{1 alice} {2 bob}]", rows)
	}
}

func TestUnion_First(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "alice"})
	db := orm.NewDB(c, postgres.Dialect{})

	a := Users.With(db).Select(Users.ID, Users.Username)
	b := Users.With(db).Select(Users.ID, Users.Username)

	row, err := orm.Union(a, b).First(context.Background())
	if err != nil {
		t.Fatalf("First() error = %v", err)
	}
	if row.Username != "alice" {
		t.Errorf("row = %+v, want {1 alice}", row)
	}
	if got := c.QueryCalls()[0]; !strings.HasSuffix(got, "LIMIT 1") {
		t.Errorf("First ran %s, want a LIMIT 1", got)
	}
}

func TestUnion_First_NoRows(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	a := Users.With(db).Where()
	b := Users.With(db).Where()

	_, err := orm.Union(a, b).First(context.Background())
	if !errors.Is(err, orm.ErrNoRows) {
		t.Errorf("First() error = %v, want ErrNoRows", err)
	}
}

func TestUnion_All_CompileErrorSurfaces(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	a := Users.With(db).Select(Users.ID)
	b := Users.With(db).Select(Users.ID, Users.Username)

	_, err := orm.Union(a, b).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want the column count mismatch rejected")
	}
}

// First surfaces whatever error All (built on compile) hits, the same way
// Filtered.First and Projection.First already do.
func TestUnion_First_CompileErrorSurfaces(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	a := Users.With(db).Select(Users.ID)
	b := Users.With(db).Select(Users.ID, Users.Username)

	_, err := orm.Union(a, b).First(context.Background())
	if err == nil {
		t.Fatal("First() error = nil, want the column count mismatch rejected")
	}
}

func TestUnion_ExecFailure(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})
	a := Users.With(db).Select(Users.ID)
	b := Users.With(db).Select(Users.ID)
	c.FailOn(`(SELECT "id" FROM "users") UNION (SELECT "id" FROM "users")`)

	_, err := orm.Union(a, b).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want the driver's failure")
	}
}

// OrderBy and Limit both clone rather than narrow in place, the same as
// every other builder in the package.
func TestUnion_LeavesOriginalAlone(t *testing.T) {
	db := pg()
	a := Users.With(db).Select(Users.ID, Users.Username)
	b := Users.With(db).Select(Users.ID, Users.Username)
	base := orm.Union(a, b)

	want, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}

	if _, _, err := base.OrderBy(Users.Username.Asc()).SQL(); err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if _, _, err := base.Limit(5).SQL(); err != nil {
		t.Fatalf("SQL() error = %v", err)
	}

	got, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if got != want {
		t.Errorf("base was changed by a branch:\n got %s\nwant %s", got, want)
	}
}
