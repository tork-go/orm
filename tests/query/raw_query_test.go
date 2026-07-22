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

func TestRawQuery_ScansTypedRows(t *testing.T) {
	type report struct {
		Country string
		Total   int64
	}

	c := fakedriver.NewConn()
	c.QueueRows([]any{"TR", int64(3)}, []any{"US", int64(5)})
	db := orm.NewDB(c, postgres.Dialect{})

	rows, err := orm.RawQuery[report](context.Background(), db,
		`SELECT country, COUNT(*) FROM users WHERE active = ? GROUP BY country`, true)
	if err != nil {
		t.Fatalf("RawQuery() error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("RawQuery() returned %d rows, want 2", len(rows))
	}
	if rows[0].Country != "TR" || rows[0].Total != 3 {
		t.Errorf("rows[0] = %+v, want {TR 3}", rows[0])
	}
	if rows[1].Country != "US" || rows[1].Total != 5 {
		t.Errorf("rows[1] = %+v, want {US 5}", rows[1])
	}

	want := `SELECT country, COUNT(*) FROM users WHERE active = $1 GROUP BY country`
	if got := c.QueryCalls()[0]; got != want {
		t.Errorf("RawQuery ran %s\nwant        %s", got, want)
	}
	if args := c.QueryArgs(0); len(args) != 1 || args[0] != true {
		t.Errorf("RawQuery bound %v, want [true]", args)
	}
}

func TestRawQuery_NoRows(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	type report struct{ Country string }

	rows, err := orm.RawQuery[report](context.Background(), db, `SELECT country FROM users`)
	if err != nil {
		t.Fatalf("RawQuery() error = %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("RawQuery() = %v, want none", rows)
	}
}

func TestRawQuery_PlaceholderMismatch(t *testing.T) {
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})
	type report struct{ Country string }

	tests := map[string]struct {
		sql  string
		args []any
		want string
	}{
		"too few arguments": {
			sql: "a = ? AND b = ?", args: []any{1}, want: "more ? placeholders",
		},
		"too many arguments": {
			sql: "a = ?", args: []any{1, 2}, want: "argument(s) given",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := orm.RawQuery[report](context.Background(), db, tt.sql, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("RawQuery() error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

// The doubled ?? escape RawQuery shares with orm.Raw still works over a
// whole statement.
func TestRawQuery_EscapesADoubledQuestionMark(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})
	type report struct{ Theme string }

	if _, err := orm.RawQuery[report](context.Background(), db,
		`SELECT prefs ->> 'theme' FROM users WHERE prefs ?? ?`, "theme"); err != nil {
		t.Fatalf("RawQuery() error = %v", err)
	}
	want := `SELECT prefs ->> 'theme' FROM users WHERE prefs ? $1`
	if got := c.QueryCalls()[0]; got != want {
		t.Errorf("RawQuery ran %s\nwant        %s", got, want)
	}
}

func TestRawQuery_NoHandle(t *testing.T) {
	type report struct{ Country string }
	_, err := orm.RawQuery[report](context.Background(), nil, `SELECT 1`)
	if err == nil {
		t.Fatal("RawQuery() error = nil, want a missing database handle to be rejected")
	}
}

func TestRawQuery_ExecFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.FailOn(`SELECT country FROM users`)
	db := orm.NewDB(c, postgres.Dialect{})
	type report struct{ Country string }

	_, err := orm.RawQuery[report](context.Background(), db, `SELECT country FROM users`)
	if err == nil {
		t.Fatal("RawQuery() error = nil, want the driver's failure")
	}
}

func TestRawQuery_RowsErrIsReported(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"TR"})
	c.RowsErr = errors.New("connection lost")
	db := orm.NewDB(c, postgres.Dialect{})
	type report struct{ Country string }

	_, err := orm.RawQuery[report](context.Background(), db, `SELECT country FROM users`)
	if err == nil {
		t.Fatal("RawQuery() error = nil, want rows.Err() reported")
	}
	if !strings.Contains(err.Error(), "reading rows") {
		t.Errorf("error %q does not say what failed", err)
	}
}

// An unexported field is skipped, the same as an unexported struct field is
// everywhere else in this package: there is nothing to address without a
// panic, and nothing a caller could have meant to fill from a query result.
func TestRawQuery_SkipsUnexportedFields(t *testing.T) {
	type report struct {
		Country string
		total   int64
	}

	c := fakedriver.NewConn()
	c.QueueRows([]any{"TR"})
	db := orm.NewDB(c, postgres.Dialect{})

	rows, err := orm.RawQuery[report](context.Background(), db, `SELECT country FROM users`)
	if err != nil {
		t.Fatalf("RawQuery() error = %v", err)
	}
	if len(rows) != 1 || rows[0].Country != "TR" {
		t.Errorf("rows = %+v, want [{TR 0}]", rows)
	}
}

// T must be a struct: scanStruct has nothing to match a non-struct type's
// fields against.
func TestRawQuery_NonStructType(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"TR"})
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := orm.RawQuery[string](context.Background(), db, `SELECT country FROM users`)
	if err == nil {
		t.Fatal("RawQuery() error = nil, want a non-struct type rejected")
	}
	if !strings.Contains(err.Error(), "is not a struct") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// A scan failure — here, too few destinations for the row fakedriver
// queued — is reported rather than silently dropped.
func TestRawQuery_ScanFailure(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"TR", int64(3)})
	db := orm.NewDB(c, postgres.Dialect{})
	type report struct{ Country string } // only one field for a two-column row

	_, err := orm.RawQuery[report](context.Background(), db, `SELECT country, total FROM users`)
	if err == nil {
		t.Fatal("RawQuery() error = nil, want the scan failure reported")
	}
}
