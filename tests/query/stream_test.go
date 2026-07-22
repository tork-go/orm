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

// Each streams the same rows All would return, one at a time, running the
// same statement.
func TestEach_YieldsEveryRowInOrder(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "a", "a"}, []any{2, "b", "b"}, []any{3, "c", "c"})
	db := orm.NewDB(c, postgres.Dialect{})

	var got []int
	for p, err := range Posts.With(db).Each(context.Background()) {
		if err != nil {
			t.Fatalf("Each yielded error = %v", err)
		}
		got = append(got, p.ID)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("streamed ids %v, want [1 2 3] in order", got)
	}
	if want := `SELECT "id", "title", "slug" FROM "posts"`; c.QueryCalls()[0] != want {
		t.Errorf("ran  %s\nwant %s", c.QueryCalls()[0], want)
	}
}

// The AfterLoad hook All runs per row runs per streamed row too, since a *E
// handed out has been through it however it was read.
func TestEach_RunsAfterLoadPerRow(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "a", "a"}, []any{2, "b", "b"})
	db := orm.NewDB(c, postgres.Dialect{})

	n := 0
	for p, err := range Posts.With(db).Each(context.Background()) {
		if err != nil {
			t.Fatalf("Each yielded error = %v", err)
		}
		if strings.Join(p.fired, ",") != "AfterLoad" {
			t.Errorf("row %d fired %v, want AfterLoad once", p.ID, p.fired)
		}
		n++
	}
	if n != 2 {
		t.Errorf("streamed %d rows, want 2", n)
	}
}

// An empty result set runs the loop body zero times rather than erroring.
func TestEach_EmptyResultYieldsNothing(t *testing.T) {
	c := fakedriver.NewConn() // nothing queued
	db := orm.NewDB(c, postgres.Dialect{})

	for _, err := range Users.With(db).Each(context.Background()) {
		if err != nil {
			t.Fatalf("Each yielded error = %v", err)
		}
		t.Fatal("loop body ran, want no rows")
	}
}

// Breaking out of the loop closes the result set, so an early exit leaks no
// cursor.
func TestEach_EarlyBreakClosesTheCursor(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "a", "a"}, []any{2, "b", "b"}, []any{3, "c", "c"})
	db := orm.NewDB(c, postgres.Dialect{})

	seen := 0
	for p, err := range Posts.With(db).Each(context.Background()) {
		if err != nil {
			t.Fatalf("Each yielded error = %v", err)
		}
		seen++
		if p.ID == 1 {
			break
		}
	}
	if seen != 1 {
		t.Errorf("saw %d rows before break, want 1", seen)
	}
	res := c.Results()
	if len(res) != 1 || !res[0].Closed() {
		t.Errorf("cursor was not closed after an early break")
	}
}

// A scan that cannot decode a row surfaces as the error value and stops the
// stream.
func TestEach_ScanErrorSurfaces(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"not an int", "t", "s"})
	db := orm.NewDB(c, postgres.Dialect{})

	var gotErr error
	rows := 0
	for p, err := range Posts.With(db).Each(context.Background()) {
		if err != nil {
			gotErr = err
			break
		}
		_ = p
		rows++
	}
	if rows != 0 {
		t.Errorf("streamed %d rows, want none before the scan error", rows)
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "scanning row") {
		t.Errorf("error = %v, want it to name the scan", gotErr)
	}
}

// A connection lost partway through iterating is reported from Err, exactly
// as All reports it, after the rows that did come back.
func TestEach_MidIterationErrorSurfacesAfterTheRows(t *testing.T) {
	boom := errors.New("connection lost")
	c := fakedriver.NewConn()
	c.QueueFailingRows(boom, []any{1, "a", "a"}, []any{2, "b", "b"})
	db := orm.NewDB(c, postgres.Dialect{})

	var got []int
	var gotErr error
	for p, err := range Posts.With(db).Each(context.Background()) {
		if err != nil {
			gotErr = err
			break
		}
		got = append(got, p.ID)
	}
	if len(got) != 2 {
		t.Errorf("streamed %d rows, want both before the error", len(got))
	}
	if !errors.Is(gotErr, boom) {
		t.Errorf("error = %v, want it to wrap the driver's", gotErr)
	}
}

// An AfterLoad that refuses stops the stream at that row rather than handing
// it back, and names the hook.
func TestEach_AfterLoadRefusalStopsTheStream(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "a"}, []any{2, "b"})
	db := orm.NewDB(c, postgres.Dialect{})

	rows := 0
	var gotErr error
	for r, err := range Refusing.With(db).Each(context.Background()) {
		if err != nil {
			gotErr = err
			break
		}
		_ = r
		rows++
	}
	if rows != 0 {
		t.Errorf("streamed %d rows, want none once the hook refused", rows)
	}
	if !errors.Is(gotErr, errRefused) || !strings.Contains(gotErr.Error(), "AfterLoad") {
		t.Errorf("error = %v, want the AfterLoad refusal", gotErr)
	}
}

// A statement failure is the first thing yielded, with no rows.
func TestEach_QueryFailureSurfaces(t *testing.T) {
	c := fakedriver.NewConn()
	c.FailOn(`SELECT "id", "title", "slug" FROM "posts"`)
	db := orm.NewDB(c, postgres.Dialect{})

	rows := 0
	var gotErr error
	for p, err := range Posts.With(db).Each(context.Background()) {
		if err != nil {
			gotErr = err
			break
		}
		_ = p
		rows++
	}
	if rows != 0 || gotErr == nil {
		t.Errorf("streamed %d rows and error = %v, want the query failure and no rows", rows, gotErr)
	}
}

// An error a builder call held is surfaced as the first yield, since Each
// returns a sequence and has nowhere to report it before iteration.
func TestEach_HeldBuilderErrorSurfaces(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	var gotErr error
	for _, err := range Users.With(db).Limit(-1).Each(context.Background()) {
		gotErr = err
		break
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "Limit(-1)") {
		t.Errorf("error = %v, want the held Limit error", gotErr)
	}
	if len(c.QueryCalls()) != 0 {
		t.Errorf("ran %v, want nothing run for a query that never compiled", c.QueryCalls())
	}
}

// A query asking for eager loading cannot stream: it names the conflict
// rather than running and returning rows with empty relationships.
func TestEach_RefusesLoad(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	var gotErr error
	for _, err := range Authors.With(db).Load(Authors.Books).Each(context.Background()) {
		gotErr = err
		break
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "Load") {
		t.Errorf("error = %v, want the Load refusal", gotErr)
	}
	if len(c.QueryCalls()) != 0 {
		t.Errorf("ran %v, want nothing run when Load is refused", c.QueryCalls())
	}
}

// Each is on both Query and Filtered, exactly as All is.
func TestEach_OnQueryAndFiltered(t *testing.T) {
	t.Run("query", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{1, "a", "a"})
		db := orm.NewDB(c, postgres.Dialect{})

		n := 0
		for _, err := range Posts.With(db).Each(context.Background()) {
			if err != nil {
				t.Fatalf("Each error = %v", err)
			}
			n++
		}
		if n != 1 {
			t.Errorf("streamed %d rows, want 1", n)
		}
	})

	t.Run("filtered", func(t *testing.T) {
		c := fakedriver.NewConn()
		c.QueueRows([]any{1, "a", "a"})
		db := orm.NewDB(c, postgres.Dialect{})

		n := 0
		for _, err := range Posts.With(db).Where(Posts.ID.GreaterThan(0)).Each(context.Background()) {
			if err != nil {
				t.Fatalf("Each error = %v", err)
			}
			n++
		}
		if n != 1 {
			t.Errorf("streamed %d rows, want 1", n)
		}
		if got := c.QueryCalls()[0]; !strings.Contains(got, "WHERE") {
			t.Errorf("ran %s, want the filter carried into the stream", got)
		}
	})
}

// A lock composes with a stream: the clause is on the statement, and reading a
// batch at a time under it is the work-queue shape locking exists for.
func TestEach_ComposesWithALock(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "a", "a"})
	db := orm.NewDB(c, postgres.Dialect{})

	for _, err := range Posts.With(db).Where(Posts.ID.GreaterThan(0)).
		ForUpdate().SkipLocked().Each(context.Background()) {
		if err != nil {
			t.Fatalf("Each error = %v", err)
		}
	}
	if got := c.QueryCalls()[0]; !strings.HasSuffix(got, "FOR UPDATE SKIP LOCKED") {
		t.Errorf("ran %s, want the lock clause on the streamed statement", got)
	}
}

// orm.Select streams its one column, the scalar counterpart to entity Each.
func TestEach_Scalars(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice"}, []any{"bob"})
	db := orm.NewDB(c, postgres.Dialect{})

	var got []string
	for name, err := range orm.Select(Users.With(db), Users.Username).Each(context.Background()) {
		if err != nil {
			t.Fatalf("Each yielded error = %v", err)
		}
		got = append(got, name)
	}
	if strings.Join(got, ",") != "alice,bob" {
		t.Errorf("streamed %v, want [alice bob]", got)
	}
	if want := `SELECT "username" FROM "users"`; c.QueryCalls()[0] != want {
		t.Errorf("ran  %s\nwant %s", c.QueryCalls()[0], want)
	}
}

// A scalar stream surfaces an error the same way, with the zero value.
func TestEach_ScalarsErrorSurfaces(t *testing.T) {
	boom := errors.New("connection lost")
	c := fakedriver.NewConn()
	c.QueueFailingRows(boom, []any{"alice"})
	db := orm.NewDB(c, postgres.Dialect{})

	var got []string
	var gotErr error
	for name, err := range orm.Select(Users.With(db), Users.Username).Each(context.Background()) {
		if err != nil {
			gotErr = err
			break
		}
		got = append(got, name)
	}
	if len(got) != 1 {
		t.Errorf("streamed %d values, want the one before the error", len(got))
	}
	if !errors.Is(gotErr, boom) {
		t.Errorf("error = %v, want it to wrap the driver's", gotErr)
	}
}

// A scalar stream releases its cursor on an early break, exactly as the
// entity one does.
func TestEach_ScalarsEarlyBreak(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"alice"}, []any{"bob"}, []any{"carol"})
	db := orm.NewDB(c, postgres.Dialect{})

	seen := 0
	for range orm.Select(Users.With(db), Users.Username).Each(context.Background()) {
		seen++
		break
	}
	if seen != 1 {
		t.Errorf("saw %d values before break, want 1", seen)
	}
	if res := c.Results(); len(res) != 1 || !res[0].Closed() {
		t.Errorf("cursor was not closed after an early break")
	}
}

// An error the query already carried surfaces as the first yield, with the
// zero value, before any statement runs.
func TestEach_ScalarsCompileErrorSurfaces(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	var gotErr error
	for _, err := range orm.Select[string](Users.With(db), nil).Each(context.Background()) {
		gotErr = err
		break
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "no column") {
		t.Errorf("error = %v, want the missing-column error", gotErr)
	}
	if len(c.QueryCalls()) != 0 {
		t.Errorf("ran %v, want nothing run for a query that never compiled", c.QueryCalls())
	}
}

// A statement failure on a scalar stream is the first thing yielded.
func TestEach_ScalarsQueryFailureSurfaces(t *testing.T) {
	c := fakedriver.NewConn()
	c.FailOn(`SELECT "username" FROM "users"`)
	db := orm.NewDB(c, postgres.Dialect{})

	var gotErr error
	values := 0
	for name, err := range orm.Select(Users.With(db), Users.Username).Each(context.Background()) {
		if err != nil {
			gotErr = err
			break
		}
		_ = name
		values++
	}
	if values != 0 || gotErr == nil {
		t.Errorf("streamed %d values and error = %v, want the query failure and none", values, gotErr)
	}
}

// A value that cannot decode into the column's type surfaces as the error and
// stops the scalar stream.
func TestEach_ScalarsScanErrorSurfaces(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{123}) // an int where the column is a string
	db := orm.NewDB(c, postgres.Dialect{})

	var gotErr error
	for _, err := range orm.Select(Users.With(db), Users.Username).Each(context.Background()) {
		if err != nil {
			gotErr = err
			break
		}
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "scanning") {
		t.Errorf("error = %v, want it to name the scan", gotErr)
	}
}

// Nothing here hard-codes Postgres: a scalar stream compiled against the fake
// dialect wears its bracket-and-? spelling.
func TestEach_AsksTheDialect(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "a", "a"})
	db := orm.NewDB(c, fakedriver.NewDialect())

	for _, err := range Posts.With(db).Where(Posts.ID.GreaterThan(0)).Each(context.Background()) {
		if err != nil {
			t.Fatalf("Each error = %v", err)
		}
	}
	if want := `SELECT [id], [title], [slug] FROM [posts] WHERE [id] > ?`; c.QueryCalls()[0] != want {
		t.Errorf("ran  %s\nwant %s", c.QueryCalls()[0], want)
	}
}
