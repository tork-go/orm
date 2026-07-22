package postgres_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
)

// A dialect has to satisfy the query side as well as the DDL side, and
// driver.Dialect embeds orm.QueryDialect, so this fails to compile if the
// four methods drift.
var _ orm.QueryDialect = postgres.Dialect{}

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "users", `"users"`},
		{"reserved word", "order", `"order"`},
		{"embedded quote", `we"ird`, `"we""ird"`},
		{"mixed case preserved", "UserID", `"UserID"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (postgres.Dialect{}).QuoteIdent(tt.in); got != tt.want {
				t.Errorf("QuoteIdent(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

// Postgres numbers its parameters, so the compiler has to count rather
// than emit a fixed marker.
func TestPlaceholder(t *testing.T) {
	d := postgres.Dialect{}
	for n, want := range map[int]string{1: "$1", 2: "$2", 10: "$10"} {
		if got := d.Placeholder(n); got != want {
			t.Errorf("Placeholder(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestRenderLike(t *testing.T) {
	d := postgres.Dialect{}

	got := d.RenderLike(`"users"."name"`, "$1", false)
	want := `"users"."name" LIKE $1 ESCAPE '\'`
	if got != want {
		t.Errorf("RenderLike(sensitive) = %s, want %s", got, want)
	}

	got = d.RenderLike(`"users"."name"`, "$1", true)
	want = `"users"."name" ILIKE $1 ESCAPE '\'`
	if got != want {
		t.Errorf("RenderLike(insensitive) = %s, want %s", got, want)
	}
}

// Postgres will not guess a bare parameter's type where nothing beside it
// settles one, so a CASE arm has to say. A type with no column spelling is
// left untyped rather than cast to a guess.
func TestRenderTypedPlaceholder(t *testing.T) {
	d := postgres.Dialect{}

	tests := map[string]struct {
		goType reflect.Type
		want   string
	}{
		"int":     {reflect.TypeFor[int](), "CAST($1 AS INTEGER)"},
		"int64":   {reflect.TypeFor[int64](), "CAST($1 AS BIGINT)"},
		"float64": {reflect.TypeFor[float64](), "CAST($1 AS DOUBLE PRECISION)"},
		"string":  {reflect.TypeFor[string](), "CAST($1 AS TEXT)"},
		"bool":    {reflect.TypeFor[bool](), "CAST($1 AS BOOLEAN)"},
		"time":    {reflect.TypeFor[time.Time](), "CAST($1 AS TIMESTAMP WITHOUT TIME ZONE)"},

		// Nothing to say, so nothing is said.
		"no type": {nil, "$1"},
		// A kind this driver has no column type for: a struct is not a
		// column, so it is sent untyped rather than wrapped in a guess.
		"unmappable kind": {reflect.TypeFor[struct{ A int }](), "$1"},
		// A slice is an array kind, but nothing here says of what, and an
		// array type cannot be spelled without its element. Untyped again
		// rather than a cast that would be wrong.
		"array without an element type": {reflect.TypeFor[[]int](), "$1"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := d.RenderTypedPlaceholder("$1", tt.goType); got != tt.want {
				t.Errorf("RenderTypedPlaceholder() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestSupportsReturning(t *testing.T) {
	if !(postgres.Dialect{}).SupportsReturning() {
		t.Error("SupportsReturning() = false, want true: Postgres has RETURNING")
	}
}

func TestRenderLock(t *testing.T) {
	d := postgres.Dialect{}

	tests := []struct {
		name string
		mode orm.LockMode
		wait orm.LockWait
		want string
	}{
		{"for update", orm.LockUpdate, orm.LockBlock, "FOR UPDATE"},
		{"for share", orm.LockShare, orm.LockBlock, "FOR SHARE"},
		{"skipping locked rows", orm.LockUpdate, orm.LockSkip, "FOR UPDATE SKIP LOCKED"},
		{"refusing to wait", orm.LockUpdate, orm.LockNoWait, "FOR UPDATE NOWAIT"},
		{"share, skipping", orm.LockShare, orm.LockSkip, "FOR SHARE SKIP LOCKED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := d.RenderLock(tt.mode, tt.wait)
			if err != nil {
				t.Fatalf("RenderLock() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("RenderLock() = %s, want %s", got, tt.want)
			}
		})
	}
}

// A value outside the two enums cannot arrive through the query API, which
// only ever passes the constants. It is rejected rather than rendered as
// whichever branch happened to be last, so a mode added to orm without a
// spelling here is a failure that names itself.
func TestRenderLock_UnknownValues(t *testing.T) {
	d := postgres.Dialect{}

	if _, err := d.RenderLock(orm.LockMode(99), orm.LockBlock); err == nil {
		t.Error("RenderLock() error = nil, want the unknown mode rejected")
	}
	if _, err := d.RenderLock(orm.LockUpdate, orm.LockWait(99)); err == nil {
		t.Error("RenderLock() error = nil, want the unknown wait rejected")
	}
}
