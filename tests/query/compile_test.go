package query_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// pg builds a handle that compiles Postgres SQL. The connection is never
// used: SQL only compiles the statement.
func pg() *orm.DB { return orm.NewDB(fakedriver.NewConn(), postgres.Dialect{}) }

// fake builds a handle whose dialect answers deliberately unlike Postgres,
// with square brackets and a repeated question mark. A compiler test that
// passed only against Postgres would be hard-coding one dialect's
// spelling; asserting both is what proves the compiler asks the dialect.
func fake() *orm.DB { return orm.NewDB(fakedriver.NewConn(), fakedriver.NewDialect()) }

func TestSelect_Simple(t *testing.T) {
	sql, args, err := Users.With(pg()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT ` + userCols + ` FROM "users"`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none", args)
	}
}

func TestSelect_WhereOrderLimitOffset(t *testing.T) {
	sql, args, err := Users.With(pg()).
		Where(Users.Age.GreaterThan(18), Users.Username.Equals("alice")).
		OrderBy(Users.ID.Desc(), Users.Username.Asc()).
		Limit(20).
		Offset(40).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT ` + userCols + ` FROM "users"` +
		` WHERE ("age" > $1 AND "username" = $2)` +
		` ORDER BY "id" DESC, "username" ASC LIMIT 20 OFFSET 40`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 2 || args[0] != 18 || args[1] != "alice" {
		t.Errorf("args = %v, want [18 alice]", args)
	}
}

// The same query against a dialect that quotes and numbers differently.
// Nothing about the compiler may assume Postgres.
func TestSelect_AsksTheDialect(t *testing.T) {
	sql, args, err := Users.With(fake()).
		Where(Users.Age.GreaterThan(18), Users.Username.Equals("alice")).
		Limit(5).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	want := `SELECT [id], [username], [email], [age], [prefs], [created_at] FROM [users]` +
		` WHERE ([age] > ? AND [username] = ?) LIMIT 5`
	if sql != want {
		t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, want two", args)
	}
}

func TestSelect_PredicateShapes(t *testing.T) {
	tests := []struct {
		name string
		pred orm.Predicate
		want string
		args []any
	}{
		{"Equals", Users.ID.Equals(1), `"id" = $1`, []any{1}},
		{"NotEquals", Users.ID.NotEquals(1), `"id" <> $1`, []any{1}},
		{"GreaterThan", Users.ID.GreaterThan(1), `"id" > $1`, []any{1}},
		{"GreaterOrEqual", Users.ID.GreaterOrEqual(1), `"id" >= $1`, []any{1}},
		{"LessThan", Users.ID.LessThan(1), `"id" < $1`, []any{1}},
		{"LessOrEqual", Users.ID.LessOrEqual(1), `"id" <= $1`, []any{1}},
		{"In", Users.ID.In(1, 2), `"id" IN ($1, $2)`, []any{1, 2}},
		{"NotIn", Users.ID.NotIn(1), `"id" NOT IN ($1)`, []any{1}},
		{"Between", Users.Age.Between(18, 65), `"age" BETWEEN $1 AND $2`, []any{18, 65}},
		{"IsNull", Users.Email.IsNull(), `"email" IS NULL`, nil},
		{"IsNotNull", Users.Email.IsNotNull(), `"email" IS NOT NULL`, nil},
		{"Contains", Users.Username.Contains("ali"), `"username" LIKE $1 ESCAPE '\'`, []any{"%ali%"}},
		{"ILike", Users.Username.ILike("A%"), `"username" ILIKE $1 ESCAPE '\'`, []any{"A%"}},
		{"Not", orm.Not(Users.ID.Equals(1)), `NOT ("id" = $1)`, []any{1}},
		{
			"Or",
			orm.Or(Users.ID.Equals(1), Users.ID.Equals(2)),
			`("id" = $1 OR "id" = $2)`,
			[]any{1, 2},
		},
		{
			"nested",
			orm.Or(Users.ID.Equals(1), orm.And(Users.Age.GreaterThan(18), Users.Email.IsNotNull())),
			`("id" = $1 OR ("age" > $2 AND "email" IS NOT NULL))`,
			[]any{1, 18},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := Users.With(pg()).Where(tt.pred).SQL()
			if err != nil {
				t.Fatalf("SQL() error = %v", err)
			}
			want := `SELECT ` + userCols + ` FROM "users" WHERE ` + tt.want
			if sql != want {
				t.Errorf("SQL()  = %s\nwant   = %s", sql, want)
			}
			if len(args) != len(tt.args) {
				t.Fatalf("args = %v, want %v", args, tt.args)
			}
			for i := range args {
				if args[i] != tt.args[i] {
					t.Errorf("args[%d] = %v, want %v", i, args[i], tt.args[i])
				}
			}
		})
	}
}

// IN () is a syntax error everywhere, so an empty list compiles to the
// condition its set semantics mean: nothing is in the empty set, and
// everything is outside it.
func TestSelect_EmptyInList(t *testing.T) {
	in, _, err := Users.With(pg()).Where(Users.ID.In()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(in, `WHERE (1 = 0)`) {
		t.Errorf("IN () compiled to %s, want the always false condition", in)
	}

	// NOT IN () matches every row, so the filter says nothing and the
	// clause is dropped rather than emitted as an always true condition.
	notIn, _, err := Users.With(pg()).Where(Users.ID.NotIn()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(notIn, "WHERE") {
		t.Errorf("NOT IN () compiled to %s, want no WHERE clause", notIn)
	}
}

// And() and Or() with no arguments are documented as their identities.
func TestSelect_EmptyGroup(t *testing.T) {
	// An always true filter says nothing, so the clause is dropped.
	sql, _, err := Users.With(pg()).Where(orm.And()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if strings.Contains(sql, "WHERE") {
		t.Errorf("SQL() = %s, want no WHERE clause for an always true filter", sql)
	}

	sql, _, err = Users.With(pg()).Where(orm.Or()).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, `WHERE (1 = 0)`) {
		t.Errorf("SQL() = %s, want the always false condition", sql)
	}
}

// A document column's value travels as encoded bytes, the same way it
// does coming back out of a row. No typed column offers a comparison over
// one, deliberately, so this reaches the encoding path the way the
// predicate types allow: by constructing the comparison directly.
func TestSelect_DocumentColumnValueIsEncoded(t *testing.T) {
	_, args, err := Users.With(pg()).Where(orm.Comparison{
		Col:   Users.Prefs,
		Op:    orm.OpEquals,
		Value: Prefs{Theme: "dark"},
	}).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("args = %v, want one", args)
	}
	b, ok := args[0].([]byte)
	if !ok {
		t.Fatalf("bound %T, want []byte: a document value must be encoded", args[0])
	}
	if string(b) != `{"theme":"dark"}` {
		t.Errorf("bound %s, want the encoded document", b)
	}
}

// A value the column's codec cannot encode is reported rather than passed
// to the driver as something it has no way to write.
func TestSelect_DocumentColumnWrongType(t *testing.T) {
	_, _, err := Users.With(pg()).Where(orm.Comparison{
		Col:   Users.Prefs,
		Op:    orm.OpEquals,
		Value: "not a Prefs",
	}).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want an encode failure")
	}
	if !strings.Contains(err.Error(), `column "prefs"`) {
		t.Errorf("error %q does not name the column", err)
	}
}

// Nothing stops a caller naming another table's column, so the compiler
// has to, or the database reports it in terms of SQL the caller never
// wrote.
func TestSelect_ForeignColumnRejected(t *testing.T) {
	_, _, err := Users.With(pg()).Where(Posts.Title.Equals("x")).SQL()
	if err == nil {
		t.Fatal("SQL() error = nil, want a foreign column error")
	}
	for _, want := range []string{`column "title"`, `table "posts"`, "does not select from"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestSelect_NegativeLimitAndOffset(t *testing.T) {
	if _, _, err := Users.With(pg()).Limit(-1).SQL(); err == nil {
		t.Error("Limit(-1) produced no error")
	}
	if _, _, err := Users.With(pg()).Offset(-1).SQL(); err == nil {
		t.Error("Offset(-1) produced no error")
	}
}

// Limit(0) means LIMIT 0, which is different from never calling Limit.
func TestSelect_ZeroLimit(t *testing.T) {
	sql, _, err := Users.With(pg()).Limit(0).SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.HasSuffix(sql, "LIMIT 0") {
		t.Errorf("SQL() = %s, want LIMIT 0", sql)
	}
}

// Conditions accumulate rather than replace, so a query built up in
// branches means what it reads as.
func TestSelect_WhereAccumulates(t *testing.T) {
	sql, args, err := Users.With(pg()).
		Where(Users.Age.GreaterThan(18)).
		Where(Users.Username.Equals("alice")).
		SQL()
	if err != nil {
		t.Fatalf("SQL() error = %v", err)
	}
	if !strings.Contains(sql, `WHERE ("age" > $1 AND "username" = $2)`) {
		t.Errorf("SQL() = %s, want both conditions", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, want two", args)
	}
}
