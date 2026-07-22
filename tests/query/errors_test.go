package query_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// A predicate the compiler does not recognise reaches the fallback. The
// Predicate interface is sealed, so no outside type can implement it, but
// a nil inside a group is a Predicate that matches no case.
func TestCompile_UnknownPredicate(t *testing.T) {
	_, _, err := Users.With(pg()).Where(orm.Group{Preds: []orm.Predicate{nil}}).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want an unknown predicate error")
	}
	if !strings.Contains(err.Error(), "unknown predicate") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// Every clause that takes a caller's column has to reject one from another
// table, not just the WHERE.
func TestCompile_ForeignColumnInEveryClause(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{"in list", func() error {
			_, _, err := Users.With(pg()).Where(Posts.ID.In(1, 2)).SQL()
			return err
		}},
		{"between", func() error {
			_, _, err := Users.With(pg()).Where(Posts.ID.Between(1, 2)).SQL()
			return err
		}},
		{"pattern", func() error {
			_, _, err := Users.With(pg()).Where(Posts.Title.Contains("x")).SQL()
			return err
		}},
		{"is null", func() error {
			_, _, err := Users.With(pg()).Where(orm.Nullness{Col: Posts.Title}).SQL()
			return err
		}},
		{"negation", func() error {
			_, _, err := Users.With(pg()).Where(orm.Not(Posts.ID.Equals(1))).SQL()
			return err
		}},
		{"order by", func() error {
			_, _, err := Users.With(pg()).OrderBy(Posts.ID.Desc()).SQL()
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil {
				t.Fatal("no error, want the column rejected")
			}
			if !strings.Contains(err.Error(), `table "posts"`) {
				t.Errorf("error %q does not name the other table", err)
			}
		})
	}
}

// docNoCodec is a document column with no codec. Embedding ColumnMeta
// carries the interface across without ValueCodec, since embedding an
// interface promotes that interface's methods and nothing else.
type docNoCodec struct{ orm.ColumnMeta }

func TestCompile_DocumentColumnWithoutCodec(t *testing.T) {
	col := docNoCodec{orm.NewJSONColumn[Prefs]("prefs")}
	_, _, err := Users.With(pg()).Where(orm.Comparison{
		Col: col, Op: orm.OpEquals, Value: Prefs{},
	}).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a missing codec error")
	}
	if !strings.Contains(err.Error(), "cannot encode") {
		t.Errorf("error %q does not report the missing codec", err)
	}
}

// A document value inside an IN list encodes like any other.
func TestCompile_DocumentColumnInList(t *testing.T) {
	_, args, err := Users.With(pg()).Where(orm.InList{
		Col: Users.Prefs, Values: []any{Prefs{Theme: "dark"}},
	}).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if _, ok := args[0].([]byte); !ok {
		t.Errorf("bound %T, want []byte", args[0])
	}

	_, _, err = Users.With(pg()).Where(orm.InList{
		Col: Users.Prefs, Values: []any{"not a Prefs"},
	}).SQL()
	if err == nil {
		t.Error("a wrongly typed document value in an IN list produced no error")
	}
}

// Both ends of a BETWEEN go through the same encoding.
func TestCompile_DocumentColumnRange(t *testing.T) {
	_, _, err := Users.With(pg()).Where(orm.Range{
		Col: Users.Prefs, Lo: Prefs{}, Hi: "not a Prefs",
	}).SQL()
	if err == nil {
		t.Error("a wrongly typed document value in a BETWEEN produced no error")
	}
	_, _, err = Users.With(pg()).Where(orm.Range{
		Col: Users.Prefs, Lo: "not a Prefs", Hi: Prefs{},
	}).SQL()
	if err == nil {
		t.Error("a wrongly typed low bound produced no error")
	}
}

// A NOT wrapping a pattern negates the whole comparison, which the dialect
// wrote.
func TestCompile_NotPattern(t *testing.T) {
	sql, _, err := Users.With(pg()).Where(orm.Pattern{
		Col: Users.Username, Value: "a%", Not: true,
	}).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `NOT ("username" LIKE $1`) {
		t.Errorf("SQL() = %s, want the whole comparison negated", sql)
	}
}

func TestCompile_NotBetween(t *testing.T) {
	sql, _, err := Users.With(pg()).Where(orm.Range{
		Col: Users.Age, Lo: 1, Hi: 2, Not: true,
	}).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, "NOT BETWEEN") {
		t.Errorf("SQL() = %s, want NOT BETWEEN", sql)
	}
}

// A row whose values do not match the entity's fields fails the scan, and
// the failure names the table.
func TestAll_ScanFailureIsNamed(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"not an int", "alice", nil, 30, nil, time.Time{}})
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := Users.With(db).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want a scan failure")
	}
	if !strings.Contains(err.Error(), `table "users"`) {
		t.Errorf("error %q does not name the table", err)
	}
}

func TestCount_ReportsFailures(t *testing.T) {
	countSQL := `SELECT COUNT(*) FROM "users"`

	t.Run("builder error", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		if _, err := Users.With(db).Limit(-1).Count(context.Background()); err == nil {
			t.Error("Count() produced no error")
		}
	})

	t.Run("driver failure", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.FailOn(countSQL)
		db := orm.NewDB(c, postgres.Dialect{})
		if _, err := Users.With(db).Count(context.Background()); err == nil {
			t.Error("Count() produced no error")
		}
	})

	t.Run("no row", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		_, err := Users.With(db).Count(context.Background())
		if err == nil {
			t.Fatal("Count() produced no error")
		}
		if !strings.Contains(err.Error(), "COUNT returned no row") {
			t.Errorf("error %q does not report the empty result", err)
		}
	})

	t.Run("unscannable count", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{"not a number"})
		db := orm.NewDB(c, postgres.Dialect{})
		_, err := Users.With(db).Count(context.Background())
		if err == nil {
			t.Fatal("Count() produced no error")
		}
		if !strings.Contains(err.Error(), "scanning count") {
			t.Errorf("error %q does not report the scan failure", err)
		}
	})

	t.Run("foreign column", func(t *testing.T) {
		db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
		if _, err := Users.With(db).Where(Posts.ID.Equals(1)).Count(context.Background()); err == nil {
			t.Error("Count() produced no error")
		}
	})
}

func TestExists_ReportsFailures(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	if _, err := Users.With(db).Limit(-1).Exists(context.Background()); err == nil {
		t.Error("Exists() produced no error")
	}
}

// The unfiltered entry points delegate, so each is worth one call to prove
// it reaches the same place.
func TestQuery_UnfilteredTerminals(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{int64(3)})
	db := orm.NewDB(c, postgres.Dialect{})
	if n, err := Users.With(db).Count(context.Background()); err != nil || n != 3 {
		t.Errorf("Count() = %d, %v, want 3, nil", n, err)
	}

	c2 := fakedriver.NewConn()
	c2.QueueRows([]any{1, "a", nil, 1, nil, time.Time{}})
	db2 := orm.NewDB(c2, postgres.Dialect{})
	if _, err := Users.With(db2).First(context.Background()); err != nil {
		t.Errorf("First() error = %v", err)
	}

	c3 := fakedriver.NewConn()
	c3.QueueRows([]any{1, "a", nil, 1, nil, time.Time{}})
	db3 := orm.NewDB(c3, postgres.Dialect{})
	if ok, err := Users.With(db3).Exists(context.Background()); err != nil || !ok {
		t.Errorf("Exists() = %v, %v, want true, nil", ok, err)
	}

	if _, _, err := Users.With(db).SQL(); err != nil {
		t.Errorf("SQL() error = %v", err)
	}
	if _, _, err := Users.With(db).OrderBy(Users.ID.Asc()).SQL(); err != nil {
		t.Errorf("OrderBy().SQL() error = %v", err)
	}
	if _, _, err := Users.With(db).Offset(1).SQL(); err != nil {
		t.Errorf("Offset().SQL() error = %v", err)
	}
}

// ScanRow on a zero Table has no state at all to scan through.
func TestScanRow_ZeroTable(t *testing.T) {
	var tbl orm.Table[User]
	if _, err := tbl.ScanRow(nil); err == nil {
		t.Error("ScanRow() on a zero table produced no error")
	}
}

// A result set can fail partway through, which real drivers report from
// Err rather than from Next. A caller must not be handed the rows read
// before the failure as though they were the whole answer.
func TestAll_RowsErrorIsReported(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "a", nil, 1, nil, time.Time{}})
	c.RowsErr = errTruncated
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := Users.With(db).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want the iteration failure")
	}
	if !strings.Contains(err.Error(), "reading rows") {
		t.Errorf("error %q does not report the iteration failure", err)
	}
}

func TestCount_RowsErrorIsReported(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsErr = errTruncated
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := Users.With(db).Count(context.Background())
	if err == nil {
		t.Fatal("Count() error = nil, want the iteration failure")
	}
	if !strings.Contains(err.Error(), "counting") {
		t.Errorf("error %q does not report the failure", err)
	}
}

var errTruncated = errors.New("fakedriver: connection dropped mid result")

// A zero Table has no state at all, so a query over one cannot even name
// the table it failed on.
func TestQuery_ZeroTable(t *testing.T) {
	var tbl orm.Table[User]
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	if _, err := tbl.With(db).All(context.Background()); err == nil {
		t.Error("All() on a zero table produced no error")
	}
	if _, err := tbl.With(db).Find(context.Background(), 1); err == nil {
		t.Error("Find() on a zero table produced no error")
	}
}

// Find needs the entity mapping for the same reason scanning does.
func TestFind_WithoutEntityMapping(t *testing.T) {
	type model struct {
		orm.Table[orm.NoEntity]
		ID *orm.IntColumn
	}
	m := &model{Table: orm.NewTable[orm.NoEntity]("legacy"), ID: orm.NewIntColumn("id")}
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	_, err := m.Table.With(db).Find(context.Background(), 1)
	if err == nil {
		t.Fatal("Find() error = nil, want a missing mapping error")
	}
	if !strings.Contains(err.Error(), "DefineTable") {
		t.Errorf("error %q does not point at DefineTable", err)
	}
}
