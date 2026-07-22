package query_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
)

func TestExprOrder_Renders(t *testing.T) {
	tests := map[string]struct {
		ord  orm.Ordering
		want string
	}{
		"ascending":  {Users.Age.Times(2).Asc(), `ORDER BY ("age" * $1) ASC`},
		"descending": {Users.Age.Times(2).Desc(), `ORDER BY ("age" * $1) DESC`},
		"lifted column": {
			Users.Age.Value().Desc(), `ORDER BY "age" DESC`,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sql, _, err := Users.With(pg()).OrderBy(tt.ord).SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if !strings.HasSuffix(sql, tt.want) {
				t.Errorf("SQL() = %s\nwant it to end with %s", sql, tt.want)
			}
		})
	}
}

// A computed ordering sits beside a column one, in the order given.
func TestExprOrder_BesideAColumnOrdering(t *testing.T) {
	sql, _, err := Users.With(pg()).
		OrderBy(Users.Age.Times(2).Desc(), Users.ID.Asc()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `ORDER BY ("age" * $1) DESC, "id" ASC`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// The ordering's own placeholders number after the WHERE's, matching where
// each appears in the statement.
func TestExprOrder_PlaceholdersFollowTheFilter(t *testing.T) {
	sql, args, err := Users.With(pg()).
		Where(Users.Username.Equals("alice")).
		OrderBy(Users.Age.Times(3).Desc()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `"username" = $1`) || !strings.Contains(sql, `("age" * $2) DESC`) {
		t.Errorf("SQL() = %s", sql)
	}
	if len(args) != 2 || args[0] != "alice" || args[1] != 3 {
		t.Errorf("args = %v, want [alice 3]", args)
	}
}

func TestExprOrder_ForeignColumnRejected(t *testing.T) {
	_, _, err := Users.With(pg()).OrderBy(Posts.ID.Times(2).Asc()).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the foreign column rejected")
	}
	if !strings.Contains(err.Error(), `belongs to table "posts"`) {
		t.Errorf("error %q does not name the problem", err)
	}
}

// Cursor paging reads its ordering columns back out of a row to seek from.
// A computed ordering has no field to read, so it is refused rather than
// paged from a zero value.
func TestExprOrder_CursorRejects(t *testing.T) {
	_, err := Users.With(pg()).OrderBy(Users.Age.Times(2).Asc()).Cursor(&User{ID: 7})
	if err == nil {
		t.Fatal("Cursor() error = nil, want the computed ordering rejected")
	}
	if !strings.Contains(err.Error(), "computed by the database") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// A cursor can only have been taken over columns, so After meets a computed
// ordering when the query grew one after the cursor was made.
func TestExprOrder_AfterRejects(t *testing.T) {
	cursor, err := Users.With(pg()).OrderBy(Users.ID.Asc()).Cursor(&User{ID: 7})
	if err != nil {
		t.Fatalf("Cursor() error = %v", err)
	}
	_, _, err = Users.With(pg()).OrderBy(Users.Age.Times(2).Asc()).After(cursor).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the computed ordering rejected")
	}
	if !strings.Contains(err.Error(), "computed by the database") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// A grouped query can only order by its key or its aggregate, which a
// computed ordering is neither of.
func TestExprOrder_GroupedRejects(t *testing.T) {
	_, _, err := orm.CountBy(Users.With(pg()), Users.Username).
		OrderBy(Users.Age.Times(2).Asc()).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the computed ordering rejected")
	}
	if !strings.Contains(err.Error(), "can only be ordered by its key") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// A projection orders by an expression the same way a plain read does.
func TestExprOrder_InSelectAs(t *testing.T) {
	type row struct {
		Name string
		N    int
	}
	sql, _, err := orm.SelectAs[row](Users.With(pg()), Users.Username, Users.Age.Times(2)).
		OrderBy(Users.Age.Times(2).Desc()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `ORDER BY ("age" * $2) DESC`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// A window function's own OrderBy shares the same renderer, so it takes an
// expression too.
func TestExprOrder_InAWindow(t *testing.T) {
	type row struct {
		Name string
		Rank int64
	}
	sql, _, err := orm.SelectAs[row](
		Users.With(pg()), Users.Username,
		orm.RowNumber().OrderBy(Users.Age.Times(2).Desc()),
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `ROW_NUMBER() OVER (ORDER BY ("age" * $1) DESC)`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// A combined query orders its result as a whole, through the same renderer.
func TestExprOrder_InAUnion(t *testing.T) {
	db := pg()
	a := Users.With(db).Select(Users.ID)
	b := Users.With(db).Select(Users.ID)
	sql, _, err := orm.Union(a, b).OrderBy(Users.Age.Times(2).Desc()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `ORDER BY ("age" * $1) DESC`) {
		t.Errorf("SQL() = %s", sql)
	}
}
