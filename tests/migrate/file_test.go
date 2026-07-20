package migrate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tork-go/orm/migrate"
)

func TestParse_RoundTrip(t *testing.T) {
	m := migrate.Migration{
		Revision:     "1975ea83b712",
		DownRevision: "a3f9c1d4e8b2",
		UpSQL:        `CREATE TABLE "users" ("id" INTEGER)`,
		DownSQL:      `DROP TABLE "users"`,
	}
	dir := t.TempDir()

	path, err := migrate.Write(dir, m, "add_users")
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written file failed: %v", err)
	}
	got, err := migrate.Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if got != m {
		t.Errorf("round-tripped migration = %+v, want %+v", got, m)
	}
}

func TestParse_DownRevisionNone(t *testing.T) {
	data := []byte("-- revision: abc123\n-- down_revision: none\n-- migrate:up\nCREATE TABLE t (a INTEGER)\n\n-- migrate:down\nDROP TABLE t\n")
	got, err := migrate.Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if got.DownRevision != "none" {
		t.Errorf("DownRevision = %q, want %q", got.DownRevision, "none")
	}
}

func TestParse_MalformedFiles(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "empty file", data: ""},
		{name: "missing revision header", data: "-- down_revision: none\n-- migrate:up\nX\n\n-- migrate:down\nY\n"},
		{name: "missing down_revision header", data: "-- revision: abc\n-- migrate:up\nX\n\n-- migrate:down\nY\n"},
		{name: "missing up marker", data: "-- revision: abc\n-- down_revision: none\n-- migrate:down\nY\n"},
		{name: "missing down marker", data: "-- revision: abc\n-- down_revision: none\n-- migrate:up\nX\n"},
		{name: "down before up", data: "-- revision: abc\n-- down_revision: none\n-- migrate:down\nY\n\n-- migrate:up\nX\n"},
		{name: "duplicate up marker", data: "-- revision: abc\n-- down_revision: none\n-- migrate:up\nX\n-- migrate:up\nZ\n\n-- migrate:down\nY\n"},
		{name: "empty up section", data: "-- revision: abc\n-- down_revision: none\n-- migrate:up\n\n-- migrate:down\nY\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := migrate.Parse([]byte(tt.data)); err == nil {
				t.Fatalf("Parse(%q) succeeded, want an error", tt.data)
			}
		})
	}
}

func TestWrite_FilenameIncludesSlugifiedMessage(t *testing.T) {
	dir := t.TempDir()
	m := migrate.Migration{Revision: "abc123", DownRevision: "none", UpSQL: "X", DownSQL: "Y"}

	path, err := migrate.Write(dir, m, "Add Users Table!!")
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	base := filepath.Base(path)
	if base != "abc123_add_users_table.sql" {
		t.Errorf("filename = %q, want %q", base, "abc123_add_users_table.sql")
	}
}

func TestWrite_EmptyMessageDefaultsToAuto(t *testing.T) {
	dir := t.TempDir()
	m := migrate.Migration{Revision: "abc123", DownRevision: "none", UpSQL: "X", DownSQL: "Y"}

	path, err := migrate.Write(dir, m, "")
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if base := filepath.Base(path); base != "abc123_auto.sql" {
		t.Errorf("filename = %q, want %q", base, "abc123_auto.sql")
	}
}

func TestLoadAll_MissingDirectory(t *testing.T) {
	migrations, err := migrate.LoadAll(filepath.Join(t.TempDir(), "does_not_exist"))
	if err != nil {
		t.Fatalf("LoadAll on a missing directory failed: %v", err)
	}
	if len(migrations) != 0 {
		t.Errorf("LoadAll on a missing directory = %v, want none", migrations)
	}
}

func TestLoadAll_SortedByFilename(t *testing.T) {
	dir := t.TempDir()
	b := migrate.Migration{Revision: "b", DownRevision: "a", UpSQL: "X", DownSQL: "Y"}
	a := migrate.Migration{Revision: "a", DownRevision: "none", UpSQL: "X", DownSQL: "Y"}
	if _, err := migrate.Write(dir, b, "second"); err != nil {
		t.Fatalf("Write(b) failed: %v", err)
	}
	if _, err := migrate.Write(dir, a, "first"); err != nil {
		t.Fatalf("Write(a) failed: %v", err)
	}

	migrations, err := migrate.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("got %d migrations, want 2", len(migrations))
	}
	if migrations[0].Revision != "a" || migrations[1].Revision != "b" {
		t.Errorf("LoadAll order = [%s, %s], want [a, b] (sorted by filename)", migrations[0].Revision, migrations[1].Revision)
	}
}

func TestLoadAll_IgnoresNonSQLFiles(t *testing.T) {
	dir := t.TempDir()
	m := migrate.Migration{Revision: "a", DownRevision: "none", UpSQL: "X", DownSQL: "Y"}
	if _, err := migrate.Write(dir, m, "first"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("not sql"), 0o644); err != nil {
		t.Fatalf("writing README.md failed: %v", err)
	}

	migrations, err := migrate.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("got %d migrations, want 1 (non-.sql file must be ignored)", len(migrations))
	}
}

func TestLoadAll_PropagatesParseErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "z_broken.sql"), []byte("not a valid migration file"), 0o644); err != nil {
		t.Fatalf("writing broken file failed: %v", err)
	}
	if _, err := migrate.LoadAll(dir); err == nil {
		t.Fatal("expected LoadAll to fail on a malformed migration file, got nil")
	} else if !strings.Contains(err.Error(), "z_broken.sql") {
		t.Errorf("error %q does not name the offending file", err)
	}
}
