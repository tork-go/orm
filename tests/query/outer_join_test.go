package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// A right join keeps rows of the joined table this one has no match for.
func TestRightJoinTo_Renders(t *testing.T) {
	type row struct {
		LoginID  int
		Username *string
	}
	sql, _, err := orm.SelectAs[row](
		Users.With(pg()).RightJoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)),
		Logins.ID, Users.Username,
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT "logins"."id", "users"."username" FROM "users" ` +
		`RIGHT JOIN "logins" ON "logins"."user_id" = "users"."id"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// A full join keeps the unmatched rows of both.
func TestFullJoinTo_Renders(t *testing.T) {
	type row struct {
		LoginID  *int
		Username *string
	}
	sql, _, err := orm.SelectAs[row](
		Users.With(pg()).FullJoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)),
		Logins.ID, Users.Username,
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `FULL JOIN "logins" ON "logins"."user_id" = "users"."id"`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// OUTER is left out: LEFT JOIN and LEFT OUTER JOIN are the same join, and
// the shorter spelling keeps the generated SQL readable.
func TestOuterJoin_KeywordsAreWrittenWithoutOuter(t *testing.T) {
	type row struct{ N *int }
	for _, tt := range []struct {
		name string
		q    *orm.Filtered[User]
		want string
	}{
		{"right", Users.With(pg()).RightJoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)), "RIGHT JOIN"},
		{"full", Users.With(pg()).FullJoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)), "FULL JOIN"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := orm.SelectAs[row](tt.q, Logins.ID).SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if !strings.Contains(sql, tt.want) || strings.Contains(sql, "OUTER") {
				t.Errorf("SQL() = %s, want %q and no OUTER", sql, tt.want)
			}
		})
	}
}

// An outer join takes an alias like any other, so a table can be joined to
// itself and keep the far side's unmatched rows.
func TestRightJoinTo_Alias(t *testing.T) {
	other := orm.Alias(Employees, "other")
	type row struct{ Name *string }
	sql, _, err := orm.SelectAs[row](
		Employees.With(pg()).RightJoinTo(other, other.ManagerID.Value().Equals(Employees.ID)),
		Employees.Name,
	).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `RIGHT JOIN "employees" AS "other"`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// The rows a Filtered read scans into are *E, and an unmatched row has no
// row of this table behind it at all, so reading one is refused rather than
// handed back as a blank.
func TestRightJoinTo_AllIsRefused(t *testing.T) {
	_, err := Users.With(pg()).
		RightJoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)).
		All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want the read refused")
	}
	if !strings.Contains(err.Error(), "SelectAs") {
		t.Errorf("error = %v, want it to point at SelectAs", err)
	}
}

func TestFullJoinTo_AllIsRefused(t *testing.T) {
	_, err := Users.With(pg()).
		FullJoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)).
		All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want the read refused")
	}
}

// First reads through All, so it is refused by the same check.
func TestRightJoinTo_FirstIsRefused(t *testing.T) {
	_, err := Users.With(pg()).
		RightJoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)).
		First(context.Background())
	if err == nil {
		t.Fatal("First() error = nil, want the read refused")
	}
}

// An inner or left join still reads into *E, which is what most joins are
// for: the check is about the two kinds that keep the far side's rows.
func TestLeftJoinTo_AllStillReads(t *testing.T) {
	if _, err := Users.With(pg()).
		LeftJoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)).
		All(context.Background()); err != nil && strings.Contains(err.Error(), "SelectAs") {
		t.Errorf("All() error = %v, want a left join still readable into *E", err)
	}
}

// SQL and Count are not refused: inspecting the statement and counting its
// rows both make sense whichever side is kept.
func TestRightJoinTo_SQLAndCountAllowed(t *testing.T) {
	q := Users.With(pg()).RightJoinTo(Logins, Logins.UserID.Value().Equals(Users.ID))
	if _, _, err := q.SQL(); err != nil {
		t.Errorf("SQL() error = %v", err)
	}
}

// A column is only non-nullable in its own table: an outer join returns
// rows where a whole side is unmatched, so a projection may read any column
// into a pointer field to hold the absence.
func TestOuterJoin_ColumnReadsIntoAPointerField(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, nil}, []any{2, "ada"})
	db := orm.NewDB(c, postgres.Dialect{})

	type row struct {
		LoginID  int
		Username *string
	}
	got, err := orm.SelectAs[row](
		Users.With(db).RightJoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)),
		Logins.ID, Users.Username,
	).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("All() returned %d rows, want 2", len(got))
	}
	if got[0].Username != nil {
		t.Errorf("row 0 username = %v, want nil for the unmatched login", *got[0].Username)
	}
	if got[1].Username == nil || *got[1].Username != "ada" {
		t.Errorf("row 1 username = %v, want ada", got[1].Username)
	}
}

// The reverse is still refused: a nullable value read into a plain field
// would turn a NULL into a zero with no way to tell them apart.
func TestOuterJoin_NullableColumnIntoAPlainFieldRejected(t *testing.T) {
	type row struct{ Email string } // the column is *string
	_, _, err := orm.SelectAs[row](Users.With(pg()), Users.Email).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the narrowing refused")
	}
	if !strings.Contains(err.Error(), `field 0, "Email"`) {
		t.Errorf("error = %v", err)
	}
}

// The conditions are required, as they are for every JoinTo.
func TestRightJoinTo_NoConditions(t *testing.T) {
	_, _, err := Users.With(pg()).RightJoinTo(Logins).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a join on nothing refused")
	}
	if !strings.Contains(err.Error(), "cross join") {
		t.Errorf("error = %v", err)
	}
}

func TestFullJoinTo_NoConditions(t *testing.T) {
	if _, _, err := Users.With(pg()).FullJoinTo(Logins).SQL(); err == nil {
		t.Fatal("SQL() error = nil, want a join on nothing refused")
	}
}

// The duplicate-table guard covers every join kind.
func TestFullJoinTo_TableAlreadyInTheStatement(t *testing.T) {
	_, _, err := Users.With(pg()).
		JoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)).
		FullJoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)).
		SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the second join refused")
	}
	if !strings.Contains(err.Error(), `"logins"`) {
		t.Errorf("error = %v", err)
	}
}
