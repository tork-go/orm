package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// The query DISTINCT ON exists for: the first row of each group, in the
// statement's own order.
func TestDistinctOn_Renders(t *testing.T) {
	sql, _, err := Users.With(pg()).
		DistinctOn(Users.Age).
		OrderBy(Users.Age.Asc(), Users.CreatedAt.Desc()).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT DISTINCT ON ("age") ` + userCols + ` FROM "users" ` +
		`ORDER BY "age" ASC, "created_at" DESC`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// Several keys, and terms that accumulate across calls.
func TestDistinctOn_SeveralColumns(t *testing.T) {
	sql, _, err := Users.With(pg()).
		DistinctOn(Users.Age).DistinctOn(Users.Username).
		OrderBy(Users.Age.Asc(), Users.Username.Asc(), Users.ID.Desc()).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasPrefix(sql, `SELECT DISTINCT ON ("age", "username") `) {
		t.Errorf("SQL() = %s", sql)
	}
}

// Ordering by nothing is allowed: with no order asked for, there is no wrong
// first row of each group.
func TestDistinctOn_WithoutAnOrdering(t *testing.T) {
	sql, _, err := Users.With(pg()).DistinctOn(Users.Age).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasPrefix(sql, `SELECT DISTINCT ON ("age") `) {
		t.Errorf("SQL() = %s", sql)
	}
}

// The dialect writes the clause, so another spells it its own way.
func TestDistinctOn_DialectSpellsIt(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), fakedriver.NewDialect())
	sql, _, err := Users.With(db).DistinctOn(Users.Age).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasPrefix(sql, "SELECT DISTINCT FIRST BY [age] ") {
		t.Errorf("SQL() = %s, want the fake dialect's own spelling", sql)
	}
}

// Every database but Postgres has no such clause, and says so rather than
// returning rows that are not what was asked for.
func TestDistinctOn_DialectWithout(t *testing.T) {
	d := fakedriver.NewDialect()
	d.NoDistinctOn = true
	db := orm.NewDB(fakedriver.NewConn(), d)

	_, _, err := Users.With(db).DistinctOn(Users.Age).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the unsupported clause reported")
	}
	if !strings.Contains(err.Error(), "DISTINCT ON") {
		t.Errorf("error = %v, want it to name the operation", err)
	}
}

// The ordering decides which row of each group survives, so it has to start
// with the same columns.
func TestDistinctOn_OrderingMustStartWithTheKeys(t *testing.T) {
	_, _, err := Users.With(pg()).
		DistinctOn(Users.Age).
		OrderBy(Users.CreatedAt.Desc()).
		SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the mismatched ordering refused")
	}
	if !strings.Contains(err.Error(), `"age" is DistinctOn column 0`) {
		t.Errorf("error = %v, want it to name the column and its position", err)
	}
}

// The keys have to come first, not merely appear somewhere.
func TestDistinctOn_OrderingKeyOutOfPosition(t *testing.T) {
	_, _, err := Users.With(pg()).
		DistinctOn(Users.Age, Users.Username).
		OrderBy(Users.Username.Asc(), Users.Age.Asc()).
		SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the reordered keys refused")
	}
	if !strings.Contains(err.Error(), "has to start with the same columns") {
		t.Errorf("error = %v", err)
	}
}

// An ordering shorter than the key list cannot decide the later groups.
func TestDistinctOn_OrderingTooShort(t *testing.T) {
	_, _, err := Users.With(pg()).
		DistinctOn(Users.Age, Users.Username).
		OrderBy(Users.Age.Asc()).
		SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the short ordering refused")
	}
	if !strings.Contains(err.Error(), `"username" is DistinctOn column 1`) {
		t.Errorf("error = %v", err)
	}
}

// An expression ordering names no column, so it cannot be one of the keys.
func TestDistinctOn_OrderedByAnExpression(t *testing.T) {
	_, _, err := Users.With(pg()).
		DistinctOn(Users.Username).
		OrderBy(orm.Lower(Users.Username).Asc()).
		SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the computed ordering refused")
	}
	if !strings.Contains(err.Error(), "has to start with the same columns") {
		t.Errorf("error = %v", err)
	}
}

// Distinct and DistinctOn are different questions.
func TestDistinctOn_WithDistinct(t *testing.T) {
	_, _, err := Users.With(pg()).Distinct().DistinctOn(Users.Age).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the pair refused")
	}
	if !strings.Contains(err.Error(), "different questions") {
		t.Errorf("error = %v", err)
	}
}

func TestDistinctOn_NoColumns(t *testing.T) {
	_, _, err := Users.With(pg()).DistinctOn().SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the empty call refused")
	}
	if !strings.Contains(err.Error(), "no columns") {
		t.Errorf("error = %v", err)
	}
}

func TestDistinctOn_NilColumn(t *testing.T) {
	_, _, err := Users.With(pg()).DistinctOn(nil).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the nil column refused")
	}
	if !strings.Contains(err.Error(), "column 0 is nil") {
		t.Errorf("error = %v", err)
	}
}

// A foreign column is refused here as it is in every other clause.
func TestDistinctOn_ForeignColumn(t *testing.T) {
	_, _, err := Users.With(pg()).DistinctOn(Posts.Title).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign column refused")
	}
	if !strings.Contains(err.Error(), `belongs to table "posts"`) {
		t.Errorf("error = %v", err)
	}
}

// Counting a DISTINCT ON read counts the rows it returns, which is one per
// key rather than one per row.
func TestDistinctOn_Count(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{int64(2)})
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := Users.With(db).DistinctOn(Users.Age).Count(context.Background())
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if n != 2 {
		t.Errorf("Count() = %d, want 2", n)
	}
	want := `SELECT COUNT(*) FROM (SELECT DISTINCT ON ("age") ` + userCols +
		` FROM "users") AS "t"`
	if got := c.QueryCalls(); len(got) != 1 || got[0] != want {
		t.Errorf("QueryCalls() = %v\nwant   = [%s]", got, want)
	}
}

// A projection carries the source query's own keyword, rather than quietly
// dropping what the caller asked for.
func TestDistinctOn_InAProjection(t *testing.T) {
	type row struct {
		Age      int
		Username string
	}
	sql, _, err := orm.SelectAs[row](
		Users.With(pg()).DistinctOn(Users.Age).OrderBy(Users.Age.Asc()),
		Users.Age, Users.Username,
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasPrefix(sql, `SELECT DISTINCT ON ("age") "age", "username"`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// Distinct in a projection was silently dropped before the keyword became
// the source query's to decide.
func TestDistinct_InAProjection(t *testing.T) {
	type row struct{ Age int }
	sql, _, err := orm.SelectAs[row](Users.With(pg()).Distinct(), Users.Age).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasPrefix(sql, `SELECT DISTINCT "age"`) {
		t.Errorf("SQL() = %s, want the projection distinct", sql)
	}
}

// The reads that return a single value have no row to keep, and say so.
func TestDistinctOn_RejectedByValueReads(t *testing.T) {
	ctx := context.Background()
	db := pg()

	t.Run("Scalars.Count", func(t *testing.T) {
		_, err := orm.Select(Users.With(db).DistinctOn(Users.Age), Users.Username).Count(ctx)
		assertDistinctOnRefused(t, err)
	})
	t.Run("Sum", func(t *testing.T) {
		_, err := orm.Sum(ctx, Users.With(db).DistinctOn(Users.Age), Users.Age)
		assertDistinctOnRefused(t, err)
	})
	t.Run("CountBy", func(t *testing.T) {
		_, err := orm.CountBy(Users.With(db).DistinctOn(Users.Age), Users.Username).All(ctx)
		assertDistinctOnRefused(t, err)
	})
	t.Run("UpdateAll", func(t *testing.T) {
		_, err := Users.With(db).DistinctOn(Users.Age).Where(Users.ID.GreaterThan(0)).
			UpdateAll(ctx, Users.Age.Set(1))
		if err == nil || !strings.Contains(err.Error(), "DistinctOn") {
			t.Errorf("error = %v, want the write refused", err)
		}
	})
}

func assertDistinctOnRefused(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want the DistinctOn refused")
	}
	if !strings.Contains(err.Error(), "DistinctOn") {
		t.Errorf("error = %v, want it to name the clause", err)
	}
}

// A locking read has nothing in particular to lock once rows collapse.
func TestDistinctOn_WithLock(t *testing.T) {
	_, _, err := Users.With(pg()).DistinctOn(Users.Age).ForUpdate().SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the pair refused")
	}
	if !strings.Contains(err.Error(), "DistinctOn") {
		t.Errorf("error = %v, want it to name the clause", err)
	}
}

func TestDistinctOn_PostgresDialectRejectsNoColumns(t *testing.T) {
	if _, err := (postgres.Dialect{}).RenderDistinctOn(nil); err == nil {
		t.Error("RenderDistinctOn(nil) error = nil, want at least one column required")
	}
}

// A query branched after DistinctOn does not leak it into its sibling.
func TestDistinctOn_DoesNotLeak(t *testing.T) {
	base := Users.With(pg()).Where(Users.Age.GreaterThan(1))
	narrowed := base.DistinctOn(Users.Age)

	plain, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(plain, "DISTINCT") {
		t.Errorf("SQL() = %s, want the branch it was taken from unchanged", plain)
	}
	if _, _, err := narrowed.SQL(); err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
}
