package query_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// userRow is one row in the order a SELECT of every column reads them, so a
// queued result lines up with what the scanner expects.
func userRow(id int, name string) []any {
	return []any{id, name, nil, 0, nil, time.Time{}}
}

func TestUpdateAllReturning_Statement(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(userRow(1, "minor"), userRow(2, "minor"))
	db := orm.NewDB(c, postgres.Dialect{})

	got, err := Users.With(db).Where(Users.Age.LessThan(18)).
		UpdateAllReturning(context.Background(), Users.Username.Set("minor"))
	if err != nil {
		t.Fatalf("UpdateAllReturning() error = %v", err)
	}

	want := `UPDATE "users" SET "username" = $1 WHERE "age" < $2 RETURNING ` + userCols
	if ran := c.QueryCalls()[0]; ran != want {
		t.Errorf("ran  %s\nwant %s", ran, want)
	}
	if args := c.QueryArgs(0); len(args) != 2 || args[0] != "minor" || args[1] != 18 {
		t.Errorf("bound %v, want the SET value before the WHERE's", args)
	}
	if len(got) != 2 || got[0].ID != 1 || got[1].Username != "minor" {
		t.Errorf("UpdateAllReturning() = %+v, want both rows as they now are", got)
	}
}

func TestDeleteAllReturning_Statement(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(userRow(7, "gone"))
	db := orm.NewDB(c, postgres.Dialect{})

	got, err := Users.With(db).Where(Users.Age.LessThan(18)).DeleteAllReturning(context.Background())
	if err != nil {
		t.Fatalf("DeleteAllReturning() error = %v", err)
	}

	want := `DELETE FROM "users" WHERE "age" < $1 RETURNING ` + userCols
	if ran := c.QueryCalls()[0]; ran != want {
		t.Errorf("ran  %s\nwant %s", ran, want)
	}
	if len(got) != 1 || got[0].ID != 7 || got[0].Username != "gone" {
		t.Errorf("DeleteAllReturning() = %+v, want the row that was removed", got)
	}
}

// The unfiltered forms are on Query, as the counting ones are.
func TestReturning_UnfilteredForms(t *testing.T) {
	t.Run("update", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(userRow(1, "a"))
		db := orm.NewDB(c, postgres.Dialect{})

		got, err := Users.With(db).UpdateAllReturning(context.Background(),
			Users.Username.Set("a"))
		if err != nil {
			t.Fatalf("UpdateAllReturning() error = %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d rows, want 1", len(got))
		}
		if ran := c.QueryCalls()[0]; strings.Contains(ran, "WHERE") {
			t.Errorf("ran %s, want no filter", ran)
		}
	})

	t.Run("delete", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows(userRow(1, "a"))
		db := orm.NewDB(c, postgres.Dialect{})

		got, err := Users.With(db).DeleteAllReturning(context.Background())
		if err != nil {
			t.Fatalf("DeleteAllReturning() error = %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d rows, want 1", len(got))
		}
	})
}

// Matching nothing is an ordinary answer, as it is for the counting forms.
func TestReturning_NoRowsIsNotAnError(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	got, err := Users.With(db).Where(Users.Age.LessThan(0)).DeleteAllReturning(context.Background())
	if err != nil {
		t.Fatalf("DeleteAllReturning() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d rows, want none", len(got))
	}
}

// A *E handed to a caller has been through AfterLoad however it was read, and
// a row returned by a write is read.
func TestReturning_RunsAfterLoad(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, 1, "  MORT  "})
	db := orm.NewDB(c, postgres.Dialect{})

	got, err := Books.With(db).Where(Books.ID.Equals(1)).
		UpdateAllReturning(context.Background(), Books.Title.Set("  MORT  "))
	if err != nil {
		t.Fatalf("UpdateAllReturning() error = %v", err)
	}
	if len(got) != 1 || got[0].Title != "mort" {
		t.Errorf("title = %q, want the one AfterLoad normalised", got[0].Title)
	}
}

// A driver that cannot hand back the rows a write touched says so, and points
// at the operation that works there.
func TestReturning_UnsupportedByTheDriver(t *testing.T) {
	tests := map[string]struct {
		run  func(*orm.DB) error
		want string
	}{
		"update": {
			run: func(db *orm.DB) error {
				_, err := Users.With(db).Where(Users.Age.LessThan(18)).
					UpdateAllReturning(context.Background(), Users.Username.Set("x"))
				return err
			},
			want: "use UpdateAll",
		},
		"delete": {
			run: func(db *orm.DB) error {
				_, err := Users.With(db).Where(Users.Age.LessThan(18)).
					DeleteAllReturning(context.Background())
				return err
			},
			want: "use DeleteAll",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// The fake reports no RETURNING support unless a test sets it.
			db := orm.NewDB(fakedriver.NewConn(), fakedriver.NewDialect())
			err := tt.run(db)
			if err == nil {
				t.Fatal("error = nil, want the driver's lack of support reported")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not point at %q", err, tt.want)
			}
		})
	}
}

// Nothing about it may assume Postgres's spelling.
func TestReturning_AsksTheDialect(t *testing.T) {
	c := fakedriver.NewConn()
	d := fakedriver.NewDialect()
	d.CanReturn = true
	db := orm.NewDB(c, d)

	if _, err := Events.With(db).Where(Events.Name.Equals("x")).
		DeleteAllReturning(context.Background()); err != nil {
		t.Fatalf("DeleteAllReturning() error = %v", err)
	}
	want := `DELETE FROM [events] WHERE [name] = ? RETURNING [name]`
	if ran := c.QueryCalls()[0]; ran != want {
		t.Errorf("ran  %s\nwant %s", ran, want)
	}
}

// Everything a counting set operation refuses, the returning form refuses
// too, and names itself while doing it.
func TestReturning_RefusesWhatASetOperationCannotCarry(t *testing.T) {
	tests := map[string]struct {
		narrow func(*orm.Query[User]) *orm.Filtered[User]
		want   string
	}{
		"an OrderBy": {
			narrow: func(q *orm.Query[User]) *orm.Filtered[User] {
				return q.Where(Users.Age.LessThan(18)).OrderBy(Users.ID.Asc())
			},
			want: "an OrderBy",
		},
		"a Limit": {
			narrow: func(q *orm.Query[User]) *orm.Filtered[User] {
				return q.Where(Users.Age.LessThan(18)).Limit(5)
			},
			want: "a Limit",
		},
		"a Select": {
			narrow: func(q *orm.Query[User]) *orm.Filtered[User] {
				return q.Where(Users.Age.LessThan(18)).Select(Users.ID)
			},
			want: "a Select",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

			_, err := tt.narrow(Users.With(db)).DeleteAllReturning(context.Background())
			if err == nil {
				t.Fatal("DeleteAllReturning() error = nil, want the clause rejected")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not name %s", err, tt.want)
			}
			if !strings.Contains(err.Error(), "DeleteAllReturning") {
				t.Errorf("error %q names the wrong operation", err)
			}
		})
	}
}

// A Where that narrowed nothing is rejected here too: a returning form that
// wrote every row would hand back every row, which is not the reassurance it
// looks like.
func TestReturning_RefusesAFilterThatNarrowedNothing(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	t.Run("update", func(t *testing.T) {
		_, err := Users.With(db).Where().UpdateAllReturning(context.Background(),
			Users.Username.Set("x"))
		if err == nil || !strings.Contains(err.Error(), "added no condition") {
			t.Errorf("error = %v, want the empty filter rejected", err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		_, err := Users.With(db).Where().DeleteAllReturning(context.Background())
		if err == nil || !strings.Contains(err.Error(), "added no condition") {
			t.Errorf("error = %v, want the empty filter rejected", err)
		}
	})
}

// An update still needs something to write, whichever spelling runs it.
func TestUpdateAllReturning_NeedsAnAssignment(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	_, err := Users.With(db).Where(Users.Age.LessThan(18)).
		UpdateAllReturning(context.Background())
	if err == nil {
		t.Fatal("UpdateAllReturning() error = nil, want the empty SET rejected")
	}
	if !strings.Contains(err.Error(), "UpdateAllReturning has nothing to write") {
		t.Errorf("error %q does not name the operation and what it lacks", err)
	}
}

// A model with no row type has nowhere to put the rows a write hands back.
func TestReturning_NeedsAnEntityMapping(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	_, err := unmapped().With(db).Where(orm.Comparison{}).DeleteAllReturning(context.Background())
	if err == nil {
		t.Fatal("DeleteAllReturning() error = nil, want the missing mapping reported")
	}
	if !strings.Contains(err.Error(), "no entity mapping") {
		t.Errorf("error %q does not name the missing mapping", err)
	}
}
