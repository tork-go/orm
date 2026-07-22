package query_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/tests/fakedriver"
)

// The four strengths, in two pairs: the whole row, and the row's key alone.
func TestLock_EveryMode(t *testing.T) {
	tests := []struct {
		name string
		q    *orm.Filtered[User]
		want string
	}{
		{"ForUpdate", Users.With(pg()).ForUpdate(), "FOR UPDATE"},
		{"ForShare", Users.With(pg()).ForShare(), "FOR SHARE"},
		{"ForNoKeyUpdate", Users.With(pg()).ForNoKeyUpdate(), "FOR NO KEY UPDATE"},
		{"ForKeyShare", Users.With(pg()).ForKeyShare(), "FOR KEY SHARE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := tt.q.SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if !strings.HasSuffix(sql, tt.want) {
				t.Errorf("SQL() = %s, want it to end with %s", sql, tt.want)
			}
		})
	}
}

// The narrow modes take SkipLocked and NoWait like the wide ones.
func TestLock_NarrowModesTakeAWaitClause(t *testing.T) {
	sql, _, err := Users.With(pg()).ForNoKeyUpdate().SkipLocked().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, "FOR NO KEY UPDATE SKIP LOCKED") {
		t.Errorf("SQL() = %s", sql)
	}
}

// LockOf narrows the lock to one table of a joined read, which is what stops
// a reader holding rows it never meant to.
func TestLockOf_NarrowsToOneTable(t *testing.T) {
	sql, _, err := Books.With(pg()).Join(Books.Author).ForUpdate().LockOf(Books).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `FOR UPDATE OF "books"`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// Several tables, in the order given, and accumulating across calls.
func TestLockOf_SeveralTables(t *testing.T) {
	sql, _, err := Books.With(pg()).Join(Books.Author).
		ForUpdate().LockOf(Books).LockOf(Authors).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `FOR UPDATE OF "books", "authors"`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// An aliased table is named by its alias, since that is what the statement
// reads it as.
func TestLockOf_NamesAnAliasByItsAlias(t *testing.T) {
	mgr := orm.Alias(Employees, "mgr")
	sql, _, err := Employees.With(pg()).
		JoinAs(Employees.Manager, mgr).
		ForUpdate().LockOf(mgr).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `FOR UPDATE OF "mgr"`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// The parts can be written in any order: a mode after a narrowing keeps it,
// and a wait after either keeps both.
func TestLock_PartsComposeInAnyOrder(t *testing.T) {
	a, _, err := Books.With(pg()).Join(Books.Author).ForUpdate().LockOf(Books).NoWait().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	b, _, err := Books.With(pg()).Join(Books.Author).ForShare().LockOf(Books).ForUpdate().NoWait().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if a != b {
		t.Errorf("order changed the statement:\n%s\n%s", a, b)
	}
	if !strings.HasSuffix(a, `FOR UPDATE OF "books" NOWAIT`) {
		t.Errorf("SQL() = %s", a)
	}
}

// A table the statement never reads is refused here, where the tables it
// does read can be listed.
func TestLockOf_UnreadTable(t *testing.T) {
	_, _, err := Books.With(pg()).ForUpdate().LockOf(Authors).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the unread table refused")
	}
	if !strings.Contains(err.Error(), `does not read`) ||
		!strings.Contains(err.Error(), "books") {
		t.Errorf("error = %v, want it to name what the statement reads", err)
	}
}

func TestLockOf_NeedsALock(t *testing.T) {
	_, _, err := Books.With(pg()).Where(Books.ID.GreaterThan(0)).LockOf(Books).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want LockOf without a lock refused")
	}
	if !strings.Contains(err.Error(), "ForUpdate or ForShare") {
		t.Errorf("error = %v", err)
	}
}

func TestLockOf_NoTables(t *testing.T) {
	_, _, err := Books.With(pg()).ForUpdate().LockOf().SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the empty call refused")
	}
	if !strings.Contains(err.Error(), "no tables") {
		t.Errorf("error = %v", err)
	}
}

func TestLockOf_UndeclaredModel(t *testing.T) {
	var missing *LoginModel
	_, _, err := Users.With(pg()).ForUpdate().LockOf(missing).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the undeclared model refused")
	}
	if !strings.Contains(err.Error(), "DefineTable") {
		t.Errorf("error = %v", err)
	}
}

// A lock names rows. An outer join returns rows with nothing there to lock,
// which Postgres rejects — so it is refused here, naming the fix.
func TestLock_OverALeftJoin(t *testing.T) {
	_, _, err := Authors.With(pg()).LeftJoin(Authors.Books).ForUpdate().SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the locking outer join refused")
	}
	if !strings.Contains(err.Error(), "LockOf") || !strings.Contains(err.Error(), `"books"`) {
		t.Errorf("error = %v, want it to name the table and the fix", err)
	}
}

// Narrowing the lock away from the nullable side makes it legal again,
// which is what the error points at.
func TestLock_OverALeftJoinNarrowedAway(t *testing.T) {
	sql, _, err := Authors.With(pg()).LeftJoin(Authors.Books).
		ForUpdate().LockOf(Authors).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `FOR UPDATE OF "authors"`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// An inner join has a row on both sides of every row it returns, so it locks
// without narrowing.
func TestLock_OverAnInnerJoin(t *testing.T) {
	sql, _, err := Books.With(pg()).Join(Books.Author).ForUpdate().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, "FOR UPDATE") {
		t.Errorf("SQL() = %s", sql)
	}
}

// A table brought in by JoinTo is named the same way, since the statement
// reads it under its own name.
func TestLockOf_OverAJoinTo(t *testing.T) {
	sql, _, err := Users.With(pg()).
		JoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)).
		ForUpdate().LockOf(Logins).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `FOR UPDATE OF "logins"`) {
		t.Errorf("SQL() = %s", sql)
	}
}

// Naming the nullable side of an outer join is the case the guard is for:
// narrowing to it locks exactly the rows that may not be there.
func TestLockOf_NamesTheNullableSide(t *testing.T) {
	_, _, err := Authors.With(pg()).LeftJoin(Authors.Books).
		ForUpdate().LockOf(Books).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want locking the unmatched side refused")
	}
	if !strings.Contains(err.Error(), `"books"`) {
		t.Errorf("error = %v, want it to name the table", err)
	}
}

// The dialect writes the clause, so another spells the modes its own way.
func TestLock_DialectSpellsTheModes(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), fakedriver.NewDialect())
	sql, _, err := Users.With(db).ForKeyShare().SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, "LOCK SHARED KEEPING KEYS") {
		t.Errorf("SQL() = %s, want the fake dialect's own spelling", sql)
	}
}
