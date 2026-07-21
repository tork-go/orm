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

// unmapped is a model with no row type, so it describes a table but cannot
// back a read or a write. Built with NewTable rather than DefineTable, which
// is what leaves the entity mapping absent.
func unmapped() orm.Table[orm.NoEntity] { return orm.NewTable[orm.NoEntity]("unmapped") }

func TestUpdateAll_Statement(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 4
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := Users.With(db).
		Where(Users.Age.Lt(18)).
		UpdateAll(context.Background(), Users.Username.Set("minor"), Users.Email.SetNull())
	if err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	if n != 4 {
		t.Errorf("UpdateAll() = %d, want the 4 rows the driver reported", n)
	}

	got := c.ExecCalls()[0]
	want := `UPDATE "users" SET "username" = $1, "email" = $2 WHERE "age" < $3`
	if got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}
	// The SET values bind before the WHERE's, which is the order they appear
	// in the statement.
	args := c.ExecArgs(0)
	if len(args) != 3 || args[0] != "minor" || args[1] != nil || args[2] != 18 {
		t.Errorf("UpdateAll bound %v, want [minor <nil> 18]", args)
	}
}

func TestDeleteAll_Statement(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 2
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := Users.With(db).
		Where(Users.Email.IsNull(), Users.Age.Gt(90)).
		DeleteAll(context.Background())
	if err != nil {
		t.Fatalf("DeleteAll() error = %v", err)
	}
	if n != 2 {
		t.Errorf("DeleteAll() = %d, want 2", n)
	}

	got := c.ExecCalls()[0]
	want := `DELETE FROM "users" WHERE ("email" IS NULL AND "age" > $1)`
	if got != want {
		t.Errorf("DeleteAll ran  %s\nwant           %s", got, want)
	}
}

// Nothing about a set operation may assume Postgres's spelling.
func TestSetOps_AskTheDialect(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, fakedriver.NewDialect())

	if _, err := Users.With(db).Where(Users.Age.Lt(18)).
		UpdateAll(context.Background(), Users.Username.Set("x")); err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	want := `UPDATE [users] SET [username] = ? WHERE [age] < ?`
	if got := c.ExecCalls()[0]; got != want {
		t.Errorf("UpdateAll ran  %s\nwant           %s", got, want)
	}

	if _, err := Users.With(db).Where(Users.Age.Lt(18)).DeleteAll(context.Background()); err != nil {
		t.Fatalf("DeleteAll() error = %v", err)
	}
	want = `DELETE FROM [users] WHERE [age] < ?`
	if got := c.ExecCalls()[1]; got != want {
		t.Errorf("DeleteAll ran  %s\nwant           %s", got, want)
	}
}

// Omitting the Where is how a caller says every row, and is allowed.
func TestSetOps_UnfilteredIsAllowed(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 9
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := Users.With(db).DeleteAll(context.Background())
	if err != nil {
		t.Fatalf("DeleteAll() error = %v", err)
	}
	if n != 9 {
		t.Errorf("DeleteAll() = %d, want 9", n)
	}
	if got := c.ExecCalls()[0]; got != `DELETE FROM "users"` {
		t.Errorf("DeleteAll ran %s, want no WHERE at all", got)
	}

	if _, err := Users.With(db).UpdateAll(context.Background(), Users.Age.Set(0)); err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	if got := c.ExecCalls()[1]; got != `UPDATE "users" SET "age" = $1` {
		t.Errorf("UpdateAll ran %s, want no WHERE at all", got)
	}
}

// A Where that narrowed nothing is a filter the caller meant to have and did
// not get. Running it anyway would write the whole table.
func TestSetOps_EmptyWhereIsRejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	var none []orm.Predicate

	tests := []struct {
		name string
		run  func() (int64, error)
		op   string
		verb string
	}{
		{
			name: "DeleteAll with no arguments",
			run:  func() (int64, error) { return Users.With(db).Where().DeleteAll(context.Background()) },
			op:   "DeleteAll", verb: "delete",
		},
		{
			name: "DeleteAll with an empty slice",
			run: func() (int64, error) {
				return Users.With(db).Where(none...).DeleteAll(context.Background())
			},
			op: "DeleteAll", verb: "delete",
		},
		{
			name: "UpdateAll with an empty slice",
			run: func() (int64, error) {
				return Users.With(db).Where(none...).UpdateAll(context.Background(), Users.Age.Set(1))
			},
			op: "UpdateAll", verb: "update",
		},
		{
			// A condition that is always true compiles away to no WHERE at
			// all, so counting predicates would let this through.
			name: "a condition that compiles to nothing",
			run: func() (int64, error) {
				return Users.With(db).Where(orm.And()).DeleteAll(context.Background())
			},
			op: "DeleteAll", verb: "delete",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := tt.run()
			if err == nil {
				t.Fatal("no error, want the empty filter to be rejected")
			}
			if n != 0 {
				t.Errorf("reported %d rows for a statement that never ran", n)
			}
			for _, want := range []string{`table "users"`, "Where added no condition", tt.op, tt.verb} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// The guard is about the filter, not about how many rows match, so a
// condition that narrows to nothing at run time is still a filter.
func TestSetOps_AFilterThatMatchesNoRowsIsNotAnError(t *testing.T) {
	c := fakedriver.NewConn() // RowsAffected stays zero
	db := orm.NewDB(c, postgres.Dialect{})

	n, err := Users.With(db).Where(Users.ID.In()).DeleteAll(context.Background())
	if err != nil {
		t.Fatalf("DeleteAll() error = %v", err)
	}
	if n != 0 {
		t.Errorf("DeleteAll() = %d, want 0", n)
	}
	if got := c.ExecCalls()[0]; !strings.Contains(got, "WHERE (1 = 0)") {
		t.Errorf("DeleteAll ran %s, want the always false condition", got)
	}
}

func TestUpdateAll_NoAssignments(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := Users.With(db).Where(Users.ID.Eq(1)).UpdateAll(context.Background())
	if err == nil {
		t.Fatal("UpdateAll() error = nil, want it to report having nothing to write")
	}
	if !strings.Contains(err.Error(), "nothing to write") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// Ordering and paging change which rows come back, which no dialect can
// express for a write. Dropping them silently would run a statement over
// every matching row when the caller wrote one that appeared to touch ten.
func TestSetOps_RejectOrderingAndPaging(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	base := func() *orm.Filtered[User] { return Users.With(db).Where(Users.Age.Gt(1)) }

	tests := []struct {
		name   string
		query  func() *orm.Filtered[User]
		clause string
	}{
		{"OrderBy", func() *orm.Filtered[User] { return base().OrderBy(Users.ID.Asc()) }, "an OrderBy"},
		{"Limit", func() *orm.Filtered[User] { return base().Limit(5) }, "a Limit"},
		{"Offset", func() *orm.Filtered[User] { return base().Offset(5) }, "an Offset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.query().DeleteAll(context.Background()); err == nil {
				t.Error("DeleteAll() error = nil, want the clause rejected")
			} else if !strings.Contains(err.Error(), tt.clause) {
				t.Errorf("error %q does not name %s", err, tt.clause)
			}
			_, err := tt.query().UpdateAll(context.Background(), Users.Age.Set(1))
			if err == nil {
				t.Error("UpdateAll() error = nil, want the clause rejected")
			} else if !strings.Contains(err.Error(), tt.clause) {
				t.Errorf("error %q does not name %s", err, tt.clause)
			}
		})
	}
}

// A document column's value is encoded on its way into a SET, the same way
// it is on its way into an INSERT and back out of a row.
func TestUpdateAll_EncodesDocumentColumns(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := Users.With(db).Where(Users.ID.Eq(1)).
		UpdateAll(context.Background(), Users.Prefs.Set(Prefs{Theme: "dark"}))
	if err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	args := c.ExecArgs(0)
	b, ok := args[0].([]byte)
	if !ok {
		t.Fatalf("prefs bound as %T, want []byte", args[0])
	}
	if string(b) != `{"theme":"dark"}` {
		t.Errorf("prefs bound as %s, want the encoded document", b)
	}
}

func TestUpdateAll_EncodeFailureIsReported(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := BadDoc.With(db).Where(BadDoc.ID.Eq(1)).
		UpdateAll(context.Background(), BadDoc.Prefs.Set(Prefs{Theme: "dark"}))
	if !errors.Is(err, errCannotEncode) {
		t.Errorf("UpdateAll() error = %v, want it to wrap the codec's own error", err)
	}
}

// A column belonging to another table would compile into a reference to a
// table the statement does not name.
func TestSetOps_ForeignColumnRejected(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	t.Run("in an assignment", func(t *testing.T) {
		_, err := Users.With(db).Where(Users.ID.Eq(1)).
			UpdateAll(context.Background(), Posts.Title.Set("x"))
		if err == nil {
			t.Fatal("UpdateAll() error = nil, want the foreign column rejected")
		}
		for _, want := range []string{`column "title"`, `table "posts"`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		}
	})

	t.Run("in a condition, deleting", func(t *testing.T) {
		_, err := Users.With(db).Where(Posts.Title.Eq("x")).DeleteAll(context.Background())
		if err == nil {
			t.Fatal("DeleteAll() error = nil, want the foreign column rejected")
		}
	})

	t.Run("in a condition, updating", func(t *testing.T) {
		_, err := Users.With(db).Where(Posts.Title.Eq("x")).
			UpdateAll(context.Background(), Users.Age.Set(1))
		if err == nil {
			t.Fatal("UpdateAll() error = nil, want the foreign column rejected")
		}
	})
}

// An assignment naming no column at all can only be built by hand, but it
// must report itself rather than render a bare "= $1".
func TestUpdateAll_AssignmentWithNoColumn(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	_, err := Users.With(db).Where(Users.ID.Eq(1)).
		UpdateAll(context.Background(), orm.Assignment{Value: 1})
	if err == nil {
		t.Fatal("UpdateAll() error = nil, want the empty assignment rejected")
	}
	if !strings.Contains(err.Error(), "names no column") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestSetOps_NoEntityMapping(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	if _, err := unmapped().With(db).DeleteAll(context.Background()); err == nil {
		t.Error("DeleteAll() error = nil, want the missing mapping reported")
	} else if !strings.Contains(err.Error(), "no entity mapping") {
		t.Errorf("error %q does not name the problem", err)
	}
}

func TestSetOps_NoHandle(t *testing.T) {
	if _, err := Users.With(nil).DeleteAll(context.Background()); err == nil {
		t.Error("DeleteAll() error = nil, want the missing handle reported")
	} else if !strings.Contains(err.Error(), "no database handle") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// An error stashed by a builder surfaces from the set operation, the same
// way it does from a read.
func TestSetOps_BuilderErrorSurfaces(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	if _, err := Users.With(db).Limit(-1).DeleteAll(context.Background()); err == nil {
		t.Error("DeleteAll() error = nil, want the negative limit reported")
	} else if !strings.Contains(err.Error(), "negative") {
		t.Errorf("error %q is not the builder's own", err)
	}
}

func TestSetOps_ExecFailure(t *testing.T) {
	t.Run("update", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.FailOn(`UPDATE "users" SET "age" = $1 WHERE "id" = $2`)
		db := orm.NewDB(c, postgres.Dialect{})

		_, err := Users.With(db).Where(Users.ID.Eq(1)).UpdateAll(context.Background(), Users.Age.Set(1))
		if err == nil {
			t.Fatal("UpdateAll() error = nil, want the driver failure")
		}
		if !strings.Contains(err.Error(), "updating") {
			t.Errorf("error %q does not say what failed", err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.FailOn(`DELETE FROM "users" WHERE "id" = $1`)
		db := orm.NewDB(c, postgres.Dialect{})

		_, err := Users.With(db).Where(Users.ID.Eq(1)).DeleteAll(context.Background())
		if err == nil {
			t.Fatal("DeleteAll() error = nil, want the driver failure")
		}
		if !strings.Contains(err.Error(), "deleting") {
			t.Errorf("error %q does not say what failed", err)
		}
	})
}

// A set operation narrows a copy like every other builder, so the query it
// was called on is unchanged and can be used again.
func TestSetOps_LeaveTheQueryAlone(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	q := Users.With(db).Where(Users.Age.Lt(18))
	if _, err := q.DeleteAll(context.Background()); err != nil {
		t.Fatalf("DeleteAll() error = %v", err)
	}
	sql, _, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, "SELECT") {
		t.Errorf("the query was changed by running a set operation: %s", sql)
	}
}

// Every column type carries Set, and only the nullable ones carry SetNull
// and SetPtr, so what may be assigned is decided the same way what may be
// compared is.
func TestSetOps_AssignmentForms(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})
	email := "alice@example.com"

	_, err := Users.With(db).Where(Users.ID.Eq(1)).UpdateAll(context.Background(),
		Users.Email.Set("plain"),
		Users.Email.SetPtr(&email),
		Users.Email.SetPtr(nil),
		Users.Email.SetNull(),
	)
	if err != nil {
		t.Fatalf("UpdateAll() error = %v", err)
	}
	args := c.ExecArgs(0)
	want := []any{"plain", email, nil, nil}
	if len(args) != len(want)+1 {
		t.Fatalf("bound %v, want the four assignments and the key", args)
	}
	for i, w := range want {
		if args[i] != w {
			t.Errorf("args[%d] = %v, want %v", i, args[i], w)
		}
	}
}
