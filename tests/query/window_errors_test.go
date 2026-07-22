package query_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
)

// A frame reaching from where it ends back to where it starts describes no
// rows at all, so it is named here rather than left to the database.
func TestWindowFrame_EndsBeforeItStarts(t *testing.T) {
	type row struct{ N int }
	_, _, err := orm.SelectAs[row](Users.With(pg()),
		orm.SumOf(Users.Age).OrderBy(Users.ID.Asc()).Rows(orm.CurrentRow(), orm.Preceding(1)),
	).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the reversed frame refused")
	}
	if !strings.Contains(err.Error(), "before it starts") {
		t.Errorf("error = %v, want it to say the frame runs backwards", err)
	}
}

// The end of the window cannot be where a frame begins.
func TestWindowFrame_StartsAtUnboundedFollowing(t *testing.T) {
	type row struct{ N int }
	_, _, err := orm.SelectAs[row](Users.With(pg()),
		orm.SumOf(Users.Age).OrderBy(Users.ID.Asc()).
			Rows(orm.UnboundedFollowing(), orm.UnboundedFollowing()),
	).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the frame refused")
	}
	if !strings.Contains(err.Error(), "cannot be where it begins") {
		t.Errorf("error = %v, want it to name the bound", err)
	}
}

// Nor the start of the window where one stops.
func TestWindowFrame_EndsAtUnboundedPreceding(t *testing.T) {
	type row struct{ N int }
	_, _, err := orm.SelectAs[row](Users.With(pg()),
		orm.SumOf(Users.Age).OrderBy(Users.ID.Asc()).
			Rows(orm.UnboundedPreceding(), orm.UnboundedPreceding()),
	).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the frame refused")
	}
	if !strings.Contains(err.Error(), "cannot be where it stops") {
		t.Errorf("error = %v, want it to name the bound", err)
	}
}

// OVER belongs to a call. Arithmetic and a CASE are windowed by putting
// them inside one, not by asking them for a window.
func TestWindow_OnArithmetic(t *testing.T) {
	type row struct{ N int }
	_, _, err := orm.SelectAs[row](Users.With(pg()),
		Users.Age.Times(2).Over().OrderBy(Users.ID.Asc()),
	).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a windowed arithmetic expression refused")
	}
	if !strings.Contains(err.Error(), "not a function call") {
		t.Errorf("error = %v, want it to say what a window belongs to", err)
	}
}

func TestWindow_OnACase(t *testing.T) {
	type row struct{ N int }
	_, _, err := orm.SelectAs[row](Users.With(pg()),
		orm.Case[int]().When(Users.Age.GreaterThan(18), 1).Else(0).PartitionBy(Users.Age),
	).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a windowed CASE refused")
	}
	if !strings.Contains(err.Error(), "not a function call") {
		t.Errorf("error = %v, want it to say what a window belongs to", err)
	}
}

// DISTINCT on a plain expression was already refused; the same check now
// covers one that is not a call at all.
func TestDistinct_OnArithmetic(t *testing.T) {
	type row struct{ N int }
	_, _, err := orm.SelectAs[row](Users.With(pg()), Users.Age.Times(2).Distinct()).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want DISTINCT on arithmetic refused")
	}
	if !strings.Contains(err.Error(), "nowhere to go") {
		t.Errorf("error = %v, want it to say DISTINCT has no place there", err)
	}
}

// A window naming a column the statement does not read is refused from
// inside the OVER clause, the same way it is anywhere else.
func TestWindow_ForeignColumnInPartition(t *testing.T) {
	type row struct{ N int64 }
	_, _, err := orm.SelectAs[row](Users.With(pg()), orm.RowNumber().PartitionBy(Posts.Title)).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign column refused")
	}
	if !strings.Contains(err.Error(), `belongs to table "posts"`) {
		t.Errorf("error = %v, want it to name the table", err)
	}
}

func TestWindow_ForeignColumnInOrder(t *testing.T) {
	type row struct{ N int64 }
	_, _, err := orm.SelectAs[row](Users.With(pg()), orm.RowNumber().OrderBy(Posts.Title.Asc())).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign column refused")
	}
	if !strings.Contains(err.Error(), `belongs to table "posts"`) {
		t.Errorf("error = %v, want it to name the table", err)
	}
}
