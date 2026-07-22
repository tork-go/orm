package query_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

func TestLock_Shapes(t *testing.T) {
	tests := []struct {
		name   string
		narrow func(*orm.Query[User]) *orm.Filtered[User]
		want   string
	}{
		{
			name:   "for update",
			narrow: func(q *orm.Query[User]) *orm.Filtered[User] { return q.Where(Users.ID.Equals(1)).ForUpdate() },
			want:   ` FOR UPDATE`,
		},
		{
			name:   "for share",
			narrow: func(q *orm.Query[User]) *orm.Filtered[User] { return q.Where(Users.ID.Equals(1)).ForShare() },
			want:   ` FOR SHARE`,
		},
		{
			name: "for update, skipping locked rows",
			narrow: func(q *orm.Query[User]) *orm.Filtered[User] {
				return q.Where(Users.ID.Equals(1)).ForUpdate().SkipLocked()
			},
			want: ` FOR UPDATE SKIP LOCKED`,
		},
		{
			name: "for update, refusing to wait",
			narrow: func(q *orm.Query[User]) *orm.Filtered[User] {
				return q.Where(Users.ID.Equals(1)).ForUpdate().NoWait()
			},
			want: ` FOR UPDATE NOWAIT`,
		},
		{
			name: "for share, skipping locked rows",
			narrow: func(q *orm.Query[User]) *orm.Filtered[User] {
				return q.Where(Users.ID.Equals(1)).ForShare().SkipLocked()
			},
			want: ` FOR SHARE SKIP LOCKED`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := tt.narrow(Users.With(pg())).SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			want := `SELECT ` + userCols + ` FROM "users" WHERE "id" = $1` + tt.want
			if sql != want {
				t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
			}
		})
	}
}

// The clause goes last, after the paging, which is where every dialect that
// has one puts it.
func TestLock_ComesAfterLimitAndOffset(t *testing.T) {
	sql, _, err := Users.With(pg()).Where(Users.Age.GreaterThan(18)).
		OrderBy(Users.ID.Asc()).Limit(10).Offset(5).ForUpdate().SkipLocked().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT ` + userCols + ` FROM "users" WHERE "age" > $1 ` +
		`ORDER BY "id" ASC LIMIT 10 OFFSET 5 FOR UPDATE SKIP LOCKED`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// The two halves are one clause, so writing them in either order says the
// same thing.
func TestLock_HalvesComposeInEitherOrder(t *testing.T) {
	first, _, err := Users.With(pg()).Where(Users.ID.Equals(1)).ForUpdate().SkipLocked().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	second, _, err := Users.With(pg()).Where(Users.ID.Equals(1)).ForUpdate().SkipLocked().ForShare().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(first, "FOR UPDATE SKIP LOCKED") {
		t.Errorf("SQL() = %s, want the pair", first)
	}
	// Naming a mode again keeps what SkipLocked said rather than resetting it.
	if !strings.HasSuffix(second, "FOR SHARE SKIP LOCKED") {
		t.Errorf("SQL() = %s, want the later mode and the same wait", second)
	}
}

// Locking a read narrows a copy, as every other builder method does.
func TestLock_DoesNotChangeTheQueryItCameFrom(t *testing.T) {
	base := Users.With(pg()).Where(Users.ID.Equals(1))
	if _, _, err := base.ForUpdate().SkipLocked().SQL(); err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	sql, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(sql, "FOR ") {
		t.Errorf("SQL() = %s, want the query it branched from unlocked", sql)
	}
}

// It reads rows, so everything a read does still works.
func TestLock_RunsAnOrdinaryRead(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(userRow(1, "alice"))
	db := orm.NewDB(c, postgres.Dialect{})

	got, err := Users.With(db).Where(Users.ID.Equals(1)).ForUpdate().All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(got) != 1 || got[0].Username != "alice" {
		t.Errorf("All() = %+v, want the row", got)
	}
	if ran := c.QueryCalls()[0]; !strings.HasSuffix(ran, "FOR UPDATE") {
		t.Errorf("ran %s, want the lock on the statement that read", ran)
	}
}

// First narrows to one row, and the lock rides along with it.
func TestLock_First(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows(userRow(1, "alice"))
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).Where(Users.ID.Equals(1)).ForUpdate().NoWait().
		First(context.Background()); err != nil {
		t.Fatalf("First() error = %v", err)
	}
	if ran := c.QueryCalls()[0]; !strings.HasSuffix(ran, "LIMIT 1 FOR UPDATE NOWAIT") {
		t.Errorf("ran %s, want the lock after the limit First added", ran)
	}
}

// Reading one column is still reading rows, so it can lock them.
func TestLock_ScalarRead(t *testing.T) {
	sql, _, err := orm.Select(Users.With(pg()).Where(Users.ID.Equals(1)).ForUpdate(),
		Users.Username).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, "FOR UPDATE") {
		t.Errorf("SQL() = %s, want the lock", sql)
	}
}

// Nothing about it may assume Postgres's spelling.
func TestLock_AsksTheDialect(t *testing.T) {
	sql, _, err := Users.With(fake()).Where(Users.ID.Equals(1)).ForShare().SkipLocked().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, "LOCK SHARED PASSING OVER") {
		t.Errorf("SQL() = %s, want the fake's own spelling", sql)
	}
}

// A dialect whose database locks something coarser than a row says so, rather
// than reading rows it has not locked.
func TestLock_UnsupportedByTheDialect(t *testing.T) {
	d := fakedriver.NewDialect()
	d.NoLocking = true
	db := orm.NewDB(fakedriver.NewConn(), d)

	_, _, err := Users.With(db).Where(Users.ID.Equals(1)).ForUpdate().SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the dialect's refusal")
	}
	if !strings.Contains(err.Error(), "no way to lock a row") {
		t.Errorf("error %q is not the dialect's own", err)
	}
	if !strings.Contains(err.Error(), `table "users"`) {
		t.Errorf("error %q does not name the table it came from", err)
	}
}

// A mode has to have been chosen: SQL has no way to say what to do about a
// locked row on a read that is not locking any.
func TestLock_WaitWithoutAMode(t *testing.T) {
	for name, narrow := range map[string]func(*orm.Query[User]) *orm.Filtered[User]{
		"SkipLocked": func(q *orm.Query[User]) *orm.Filtered[User] { return q.Where().SkipLocked() },
		"NoWait":     func(q *orm.Query[User]) *orm.Filtered[User] { return q.Where().NoWait() },
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := narrow(Users.With(pg())).SQL()
			if err == nil {
				t.Fatal("SQL() error = nil, want the half clause rejected")
			}
			if !strings.Contains(err.Error(), "call ForUpdate or ForShare first") {
				t.Errorf("error %q does not say what is missing", err)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error %q does not name the call that was made", err)
			}
		})
	}
}

// Two rows that collapsed into one have no single row to lock, and Postgres
// rejects the pair outright.
func TestLock_WithDistinct(t *testing.T) {
	_, _, err := Users.With(pg()).Where(Users.ID.Equals(1)).Distinct().ForUpdate().SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the pair rejected")
	}
	if !strings.Contains(err.Error(), "cannot also be Distinct") {
		t.Errorf("error %q does not name the clash", err)
	}
}

// Collapsing the rows into a value leaves nothing to lock, and dropping the
// clause silently would run an unlocked statement where one was written.
func TestLock_RejectedWhereThereIsNothingToLock(t *testing.T) {
	tests := map[string]func(*orm.Filtered[User]) error{
		"Count": func(f *orm.Filtered[User]) error {
			_, err := f.Count(context.Background())
			return err
		},
		"a column count": func(f *orm.Filtered[User]) error {
			_, err := orm.Select(f, Users.Username).Count(context.Background())
			return err
		},
		"Sum": func(f *orm.Filtered[User]) error {
			_, err := orm.Sum(context.Background(), f, Users.Age)
			return err
		},
		"Max": func(f *orm.Filtered[User]) error {
			_, err := orm.Max(context.Background(), f, Users.Age)
			return err
		},
		"CountBy": func(f *orm.Filtered[User]) error {
			_, err := orm.CountBy(f, Users.Username).All(context.Background())
			return err
		},
		"SumBy": func(f *orm.Filtered[User]) error {
			_, _, err := orm.SumBy(f, Users.Username, Users.Age).SQL()
			return err
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			err := run(Users.With(pg()).Where(Users.ID.Equals(1)).ForUpdate())
			if err == nil {
				t.Fatal("error = nil, want the lock rejected")
			}
			if !strings.Contains(err.Error(), "nothing left for ForUpdate or ForShare to lock") {
				t.Errorf("error %q does not explain why", err)
			}
		})
	}
}

// A write locks the rows it touches by writing them, and no dialect accepts
// the clause on an UPDATE or a DELETE.
func TestLock_RejectedOnASetOperation(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	locked := Users.With(db).Where(Users.ID.Equals(1)).ForUpdate()

	if _, err := locked.DeleteAll(context.Background()); err == nil {
		t.Error("DeleteAll() error = nil, want the lock rejected")
	} else if !strings.Contains(err.Error(), "a ForUpdate or ForShare") {
		t.Errorf("error %q does not name the clause", err)
	}

	if _, err := locked.UpdateAll(context.Background(), Users.Username.Set("x")); err == nil {
		t.Error("UpdateAll() error = nil, want the lock rejected")
	}
}
