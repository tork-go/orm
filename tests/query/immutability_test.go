package query_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// A query is a value worth holding: built once, narrowed differently in
// each branch. That only works if narrowing leaves the original alone.
//
// When it did not, both branches below carried both names and each matched
// nothing, with no error anywhere to say why.
func TestQuery_BranchesDoNotContaminateEachOther(t *testing.T) {
	adults := Users.With(pg()).Where(Users.Age.Gte(18))

	alice, _, err := adults.Where(Users.Username.Eq("alice")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	bob, _, err := adults.Where(Users.Username.Eq("bob")).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}

	// One condition on username per branch, not two. Counting inside the
	// WHERE rather than the whole statement, since the SELECT list names
	// every column too.
	if got := strings.Count(whereOf(alice), `"username"`); got != 1 {
		t.Errorf("a branch carries %d username conditions, want 1:\n%s", got, alice)
	}
	if got := strings.Count(whereOf(bob), `"username"`); got != 1 {
		t.Errorf("a branch carries %d username conditions, want 1:\n%s", got, bob)
	}

	// The query they came from never had one.
	base, _, err := adults.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(whereOf(base), `"username"`) {
		t.Errorf("the original query was narrowed by its branches:\n%s", base)
	}
}

// The same, for every builder method rather than only Where.
func TestQuery_EveryBuilderLeavesTheOriginalAlone(t *testing.T) {
	base := Users.With(pg()).Where(Users.Age.Gt(18))
	want, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}

	branches := map[string]func() *orm.Filtered[User]{
		"Where":   func() *orm.Filtered[User] { return base.Where(Users.ID.Eq(1)) },
		"OrderBy": func() *orm.Filtered[User] { return base.OrderBy(Users.ID.Desc()) },
		"Limit":   func() *orm.Filtered[User] { return base.Limit(5) },
		"Offset":  func() *orm.Filtered[User] { return base.Offset(5) },
	}
	for name, branch := range branches {
		t.Run(name, func(t *testing.T) {
			narrowed, _, err := branch().SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if narrowed == want {
				t.Errorf("%s did not narrow anything", name)
			}
			got, _, err := base.SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			if got != want {
				t.Errorf("%s changed the query it was called on:\n got %s\nwant %s", name, got, want)
			}
		})
	}
}

// Sharing a slice's backing array is worse than sharing the slice: two
// branches appending into spare capacity overwrite each other, which shows
// up only at the lengths where an append happens to have room. Building a
// base with several conditions and branching twice reaches exactly that.
func TestQuery_BranchesDoNotShareBackingArrays(t *testing.T) {
	base := Users.With(pg()).Where(Users.Age.Gt(1), Users.Age.Lt(99), Users.ID.Gt(0))

	first := base.Where(Users.Username.Eq("first"))
	second := base.Where(Users.Username.Eq("second"))

	_, firstArgs, err := first.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	_, secondArgs, err := second.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}

	if firstArgs[len(firstArgs)-1] != "first" {
		t.Errorf("the first branch bound %v, want it to end in \"first\"", firstArgs)
	}
	if secondArgs[len(secondArgs)-1] != "second" {
		t.Errorf("the second branch bound %v, want it to end in \"second\"", secondArgs)
	}
}

// First narrows a copy, so the query it was called on keeps its own limit.
func TestFirst_LeavesTheQueryAlone(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "a", nil, 1, nil, time.Time{}})
	db := orm.NewDB(c, postgres.Dialect{})

	q := Users.With(db).Limit(50)
	if _, err := q.First(context.Background()); err != nil {
		t.Fatalf("First() error = %v", err)
	}
	sql, _, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, "LIMIT 50") {
		t.Errorf("First changed the query's limit: %s", sql)
	}
}

// A terminal can run more than once, and must mean the same thing each
// time rather than accumulating state.
func TestQuery_TerminalsAreRepeatable(t *testing.T) {
	q := Users.With(pg()).Where(Users.Age.Gt(18)).OrderBy(Users.ID.Desc()).Limit(5)

	first, firstArgs, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	second, secondArgs, err := q.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if first != second {
		t.Errorf("SQL() differed between calls:\n  %s\n  %s", first, second)
	}
	if len(firstArgs) != len(secondArgs) {
		t.Errorf("args differed between calls: %v then %v", firstArgs, secondArgs)
	}
}

// A query built from a table is independent of every other query from the
// same table.
func TestQuery_TablesAreNotStateful(t *testing.T) {
	one := Users.With(pg()).Where(Users.ID.Eq(1))
	two := Users.With(pg()).Where(Users.ID.Eq(2))

	s1, a1, _ := one.SQL()
	s2, a2, _ := two.SQL()
	if s1 != s2 {
		t.Errorf("two queries over one table differ in shape:\n  %s\n  %s", s1, s2)
	}
	if a1[0] == a2[0] {
		t.Errorf("two queries over one table share a bound value: %v and %v", a1, a2)
	}
}

// whereOf returns the WHERE clause of a statement, or "" when it has none.
// Assertions about conditions have to look here rather than at the whole
// statement, whose SELECT list names every column.
func whereOf(sql string) string {
	i := strings.Index(sql, " WHERE ")
	if i < 0 {
		return ""
	}
	return sql[i+len(" WHERE "):]
}

// A table and a handle are package level values in any real program, so
// several goroutines build queries from them at once. Nothing in that path
// may write to shared state.
//
// This is worth running under -race, where it is the difference between a
// detected data race and a test that happens to pass.
func TestQuery_ConcurrentBuildersAreIndependent(t *testing.T) {
	base := Users.With(pg()).Where(Users.Age.Gte(18))

	const n = 32
	results := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("user%02d", i)
			sql, args, err := base.Where(Users.Username.Eq(name)).Limit(i).SQL()
			if err != nil {
				results[i] = "error: " + err.Error()
				return
			}
			// The bound name has to be this goroutine's own.
			if len(args) != 2 || args[1] != name {
				results[i] = fmt.Sprintf("bound %v, want %q", args, name)
				return
			}
			if !strings.HasSuffix(sql, fmt.Sprintf("LIMIT %d", i)) {
				results[i] = "limit belongs to another goroutine: " + sql
			}
		}()
	}
	wg.Wait()

	for i, r := range results {
		if r != "" {
			t.Errorf("goroutine %d: %s", i, r)
		}
	}

	// And the query they all branched from is untouched.
	sql, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(whereOf(sql), `"username"`) || strings.Contains(sql, "LIMIT") {
		t.Errorf("the shared query was narrowed by a branch: %s", sql)
	}
}
