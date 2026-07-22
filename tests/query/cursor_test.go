package query_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// The worked example from the seek predicate's own doc comment: an
// ordering of (CreatedAt DESC, ID ASC) seeks past a row by comparing
// CreatedAt strictly, and breaking a tie on it by comparing ID strictly,
// each in the direction that means "later in this order".
func TestAfter_SeekPredicateShape_TwoColumns(t *testing.T) {
	t0 := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	base := Users.With(pg()).OrderBy(Users.CreatedAt.Desc(), Users.ID.Asc())
	cursor, err := base.Cursor(&User{ID: 7, CreatedAt: t0})
	if err != nil {
		t.Fatalf("Cursor() error = %v", err)
	}

	sql, args, err := base.After(cursor).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT ` + userCols + ` FROM "users" ` +
		`WHERE ("created_at" < $1 OR ("created_at" = $2 AND "id" > $3)) ` +
		`ORDER BY "created_at" DESC, "id" ASC`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	// created_at binds twice: once for the strict compare, once more for
	// the tie-break equality. That is the standard row-value expansion, not
	// a bug in how many times the cursor's value was read.
	if len(args) != 3 {
		t.Fatalf("args = %v, want 3", args)
	}
	if ct, ok := args[0].(time.Time); !ok || !ct.Equal(t0) {
		t.Errorf("args[0] = %v, want %v", args[0], t0)
	}
	if ct, ok := args[1].(time.Time); !ok || !ct.Equal(t0) {
		t.Errorf("args[1] = %v, want %v", args[1], t0)
	}
	if args[2] != 7 {
		t.Errorf("args[2] = %v, want 7", args[2])
	}
}

// A single column ordering degenerates to a bare comparison: And of one
// predicate unwraps, and Or of one does too.
func TestAfter_SeekPredicateShape_OneColumn(t *testing.T) {
	base := Users.With(pg()).OrderBy(Users.ID.Asc())
	cursor, err := base.Cursor(&User{ID: 7})
	if err != nil {
		t.Fatalf("Cursor() error = %v", err)
	}

	sql, args, err := base.After(cursor).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT ` + userCols + ` FROM "users" WHERE "id" > $1 ORDER BY "id" ASC`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 1 || args[0] != 7 {
		t.Errorf("args = %v, want [7]", args)
	}
}

// Descending order seeks with a strict less-than instead.
func TestAfter_SeekPredicateShape_Descending(t *testing.T) {
	base := Users.With(pg()).OrderBy(Users.ID.Desc())
	cursor, err := base.Cursor(&User{ID: 7})
	if err != nil {
		t.Fatalf("Cursor() error = %v", err)
	}

	sql, _, err := base.After(cursor).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT ` + userCols + ` FROM "users" WHERE "id" < $1 ORDER BY "id" DESC`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// A cursor round tripped through String and ParseCursor produces the exact
// same statement as using it directly: encoding loses nothing After needs.
func TestCursor_RoundTrip(t *testing.T) {
	base := Users.With(pg()).OrderBy(Users.ID.Asc())
	direct, err := base.Cursor(&User{ID: 7})
	if err != nil {
		t.Fatalf("Cursor() error = %v", err)
	}
	want, wantArgs, err := base.After(direct).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}

	token := direct.String()
	if token == "" {
		t.Fatal("String() returned an empty token")
	}
	roundTripped, err := orm.ParseCursor[User](token)
	if err != nil {
		t.Fatalf("ParseCursor() error = %v", err)
	}

	got, gotArgs, err := base.After(roundTripped).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if got != want {
		t.Errorf("round tripped SQL()  = %s\nwant                = %s", got, want)
	}
	if len(gotArgs) != len(wantArgs) || gotArgs[0] != wantArgs[0] {
		t.Errorf("round tripped args = %v, want %v", gotArgs, wantArgs)
	}
}

// MarshalText/UnmarshalText are the encoding.TextMarshaler/TextUnmarshaler
// forms of String/ParseCursor, used the same way json.Marshal would use
// them on a struct field typed Cursor[User].
func TestCursor_MarshalUnmarshalText(t *testing.T) {
	base := Users.With(pg()).OrderBy(Users.ID.Asc())
	cursor, err := base.Cursor(&User{ID: 7})
	if err != nil {
		t.Fatalf("Cursor() error = %v", err)
	}

	text, err := cursor.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}
	if string(text) != cursor.String() {
		t.Errorf("MarshalText() = %s, want the same token String() gives", text)
	}

	var decoded orm.Cursor[User]
	if err := decoded.UnmarshalText(text); err != nil {
		t.Fatalf("UnmarshalText() error = %v", err)
	}
	sql, _, err := base.After(decoded).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if want := `SELECT ` + userCols + ` FROM "users" WHERE "id" > $1 ORDER BY "id" ASC`; sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

func TestParseCursor_InvalidToken(t *testing.T) {
	if _, err := orm.ParseCursor[User]("not valid base64!!!"); err == nil {
		t.Fatal("ParseCursor() error = nil, want a decoding error")
	}
}

func TestParseCursor_ValidBase64InvalidJSON(t *testing.T) {
	// Valid base64 (unpadded, URL alphabet) whose decoded bytes are not JSON.
	if _, err := orm.ParseCursor[User]("bm90IGpzb24"); err == nil {
		t.Fatal("ParseCursor() error = nil, want a decoding error")
	}
}

func TestAfter_RequiresOrderBy(t *testing.T) {
	_, _, err := Users.With(pg()).After(orm.Cursor[User]{}).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want After to need an OrderBy")
	}
	if !strings.Contains(err.Error(), "needs an OrderBy") {
		t.Errorf("error %q does not say After needs an OrderBy", err)
	}
}

func TestCursor_RequiresOrderBy(t *testing.T) {
	_, err := Users.With(pg()).Cursor(&User{ID: 1})
	if err == nil {
		t.Fatal("Cursor() error = nil, want Cursor to need an OrderBy")
	}
	if !strings.Contains(err.Error(), "needs an OrderBy") {
		t.Errorf("error %q does not say Cursor needs an OrderBy", err)
	}
}

func TestCursor_NilRow(t *testing.T) {
	_, err := Users.With(pg()).OrderBy(Users.ID.Asc()).Cursor(nil)
	if err == nil {
		t.Fatal("Cursor() error = nil, want a nil row to be rejected")
	}
}

func TestCursor_NoHandle(t *testing.T) {
	_, err := Users.With(nil).OrderBy(Users.ID.Asc()).Cursor(&User{ID: 1})
	if err == nil {
		t.Fatal("Cursor() error = nil, want a missing database handle to be rejected")
	}
	if !strings.Contains(err.Error(), "no database handle") {
		t.Errorf("error %q does not mention the missing handle", err)
	}
}

// OrderBy does not itself check that a column belongs to the table it
// narrows, the same way Where does not: only compiling the statement does.
// Cursor has to make the same check independently, since it never compiles
// anything on the way to reading the row's field, and reports the mismatch
// the same way keysWereRead does for Load.
func TestCursor_OrderedByForeignColumn(t *testing.T) {
	_, err := Users.With(pg()).OrderBy(Posts.Title.Asc()).Cursor(&User{ID: 1})
	if err == nil {
		t.Fatal("Cursor() error = nil, want ordering by another table's column to be rejected")
	}
	if !strings.Contains(err.Error(), `column "title"`) {
		t.Errorf("error %q does not name the unmapped column", err)
	}
}

// A cursor's value can fail to decode against the query's column type even
// once its shape (column name, direction, arity) already matched: here a
// hand built token carries a JSON string where "id" needs a number.
func TestAfter_CursorValueDecodingFailure(t *testing.T) {
	raw := `{"t":"users","o":[{"c":"id","d":false,"v":"nope"}]}`
	token := base64.RawURLEncoding.EncodeToString([]byte(raw))
	cursor, err := orm.ParseCursor[User](token)
	if err != nil {
		t.Fatalf("ParseCursor() error = %v", err)
	}

	_, _, err = Users.With(pg()).OrderBy(Users.ID.Asc()).After(cursor).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want the malformed cursor value to be rejected")
	}
	if !strings.Contains(err.Error(), `column "id"`) {
		t.Errorf("error %q does not name the column that failed to decode", err)
	}
}

// A cursor's ordering shape must match the query's exactly: which columns,
// how many, and which direction each one is.
func TestAfter_OrderingShapeMismatch(t *testing.T) {
	cursor, err := Users.With(pg()).OrderBy(Users.ID.Asc()).Cursor(&User{ID: 7})
	if err != nil {
		t.Fatalf("Cursor() error = %v", err)
	}

	tests := []struct {
		name  string
		query *orm.Filtered[User]
	}{
		{"wrong column", Users.With(pg()).OrderBy(Users.Age.Asc())},
		{"wrong direction", Users.With(pg()).OrderBy(Users.ID.Desc())},
		{"wrong arity", Users.With(pg()).OrderBy(Users.ID.Asc(), Users.Age.Asc())},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := tt.query.After(cursor).SQL()
			if err == nil {
				t.Fatal("SQL() error = nil, want the ordering mismatch to be rejected")
			}
		})
	}
}

// A cursor's token carries the table it was taken from, checked against
// the query it is handed to regardless of what row type decoded it: here a
// token taken from posts, decoded as a Cursor[User] because nothing about
// the token format stops that, is still rejected once After sees which
// table it names.
func TestAfter_WrongTableRejected(t *testing.T) {
	postCursor, err := Posts.With(pg()).OrderBy(Posts.ID.Asc()).Cursor(&Post{ID: 1})
	if err != nil {
		t.Fatalf("Cursor() error = %v", err)
	}
	asUserCursor, err := orm.ParseCursor[User](postCursor.String())
	if err != nil {
		t.Fatalf("ParseCursor() error = %v", err)
	}

	_, _, err = Users.With(pg()).OrderBy(Users.ID.Asc()).After(asUserCursor).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a cursor from another table to be rejected")
	}
	if !strings.Contains(err.Error(), `table "posts"`) {
		t.Errorf("error %q does not name the cursor's table", err)
	}
}

func TestCursor_MissingColumnFromSelect(t *testing.T) {
	_, err := Users.With(pg()).Select(Users.Username).OrderBy(Users.ID.Asc()).Cursor(&User{ID: 7})
	if err == nil {
		t.Fatal("Cursor() error = nil, want the missing ordering column to be rejected")
	}
	if !strings.Contains(err.Error(), "Cursor needs column") {
		t.Errorf("error %q does not say what Cursor needs", err)
	}
}

func TestAfter_RejectedOnUpdateAllDeleteAll(t *testing.T) {
	cursor, err := Users.With(pg()).OrderBy(Users.ID.Asc()).Cursor(&User{ID: 7})
	if err != nil {
		t.Fatalf("Cursor() error = %v", err)
	}
	db := orm.NewDB(fakedriver.NewConn(), postgres.Dialect{})

	_, err = Users.With(db).OrderBy(Users.ID.Asc()).After(cursor).DeleteAll(context.Background())
	if err == nil {
		t.Fatal("DeleteAll() error = nil, want an OrderBy to be rejected on a set operation")
	}
	if !strings.Contains(err.Error(), "an OrderBy") {
		t.Errorf("error %q does not mention an OrderBy", err)
	}
}

func TestAfter_ComposesWithLimit(t *testing.T) {
	cursor, err := Users.With(pg()).OrderBy(Users.ID.Asc()).Cursor(&User{ID: 7})
	if err != nil {
		t.Fatalf("Cursor() error = %v", err)
	}
	sql, _, err := Users.With(pg()).OrderBy(Users.ID.Asc()).After(cursor).Limit(20).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT ` + userCols + ` FROM "users" WHERE "id" > $1 ORDER BY "id" ASC LIMIT 20`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
}

// After is a scalar-shaped builder call (it appends one predicate), so the
// ordinary clone already keeps a branch's After from leaking back to the
// query it was called on; this pins that down the way
// immutability_test.go does for every other builder.
func TestAfter_LeavesOriginalAlone(t *testing.T) {
	base := Users.With(pg()).OrderBy(Users.ID.Asc())
	cursor, err := base.Cursor(&User{ID: 7})
	if err != nil {
		t.Fatalf("Cursor() error = %v", err)
	}
	want, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}

	narrowed, _, err := base.After(cursor).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if narrowed == want {
		t.Error("After did not narrow anything")
	}

	got, _, err := base.SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if got != want {
		t.Errorf("After changed the query it was called on:\n got %s\nwant %s", got, want)
	}
}

// Query's forwarders reach Cursor and After the same way they reach every
// other builder, off a bare unfiltered query.
func TestCursor_FromQuery(t *testing.T) {
	_, err := Users.With(pg()).Cursor(&User{ID: 7})
	if err == nil {
		t.Fatal("Cursor() error = nil, want Cursor to still need an OrderBy off Query")
	}
}

// After off a bare Query needs an OrderBy just as much as it does off a
// Filtered: nothing about starting from Query changes what After needs, it
// only has nothing of its own to check yet.
func TestAfter_FromQuery(t *testing.T) {
	cursor, err := Users.With(pg()).OrderBy(Users.ID.Asc()).Cursor(&User{ID: 7})
	if err != nil {
		t.Fatalf("Cursor() error = %v", err)
	}
	got, _, err := Users.With(pg()).After(cursor).SQL()
	if err == nil {
		t.Fatalf("SQL() = %s, want After off a bare Query with no OrderBy to fail", got)
	}
}
