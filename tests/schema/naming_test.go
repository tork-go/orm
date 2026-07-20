package schema_test

import (
	"testing"

	"github.com/tork-go/orm/schema"
)

func TestPrimaryKeyConstraintName(t *testing.T) {
	if got, want := schema.PrimaryKeyConstraintName("users"), "pk_users"; got != want {
		t.Errorf("PrimaryKeyConstraintName() = %q, want %q", got, want)
	}
}

func TestUniqueConstraintName(t *testing.T) {
	tests := []struct {
		table   string
		columns []string
		want    string
	}{
		{table: "users", columns: []string{"username"}, want: "uq_users_username"},
		{table: "users", columns: []string{"first_name", "last_name"}, want: "uq_users_first_name_last_name"},
	}
	for _, tt := range tests {
		if got := schema.UniqueConstraintName(tt.table, tt.columns); got != tt.want {
			t.Errorf("UniqueConstraintName(%q, %v) = %q, want %q", tt.table, tt.columns, got, tt.want)
		}
	}
}

func TestForeignKeyConstraintName(t *testing.T) {
	tests := []struct {
		table   string
		columns []string
		want    string
	}{
		{table: "posts", columns: []string{"author_id"}, want: "fk_posts_author_id"},
		{table: "posts", columns: []string{"author_id", "org_id"}, want: "fk_posts_author_id_org_id"},
	}
	for _, tt := range tests {
		if got := schema.ForeignKeyConstraintName(tt.table, tt.columns); got != tt.want {
			t.Errorf("ForeignKeyConstraintName(%q, %v) = %q, want %q", tt.table, tt.columns, got, tt.want)
		}
	}
}

func TestIndexName(t *testing.T) {
	tests := []struct {
		table   string
		columns []string
		want    string
	}{
		{table: "posts", columns: []string{"author_id"}, want: "ix_posts_author_id"},
		{table: "posts", columns: []string{"author_id", "created_at"}, want: "ix_posts_author_id_created_at"},
	}
	for _, tt := range tests {
		if got := schema.IndexName(tt.table, tt.columns); got != tt.want {
			t.Errorf("IndexName(%q, %v) = %q, want %q", tt.table, tt.columns, got, tt.want)
		}
	}
}
