package postgres_test

import (
	"testing"

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

func TestSupportsReturning(t *testing.T) {
	if !(postgres.Dialect{}).SupportsReturning() {
		t.Error("SupportsReturning() = false, want true: Postgres has RETURNING")
	}
}
