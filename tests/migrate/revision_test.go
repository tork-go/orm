package migrate_test

import (
	"regexp"
	"testing"

	"github.com/tork-go/orm/migrate"
)

var hexRevision = regexp.MustCompile(`^[0-9a-f]{12}$`)

func TestNewRevisionID_Format(t *testing.T) {
	id, err := migrate.NewRevisionID()
	if err != nil {
		t.Fatalf("NewRevisionID failed: %v", err)
	}
	if !hexRevision.MatchString(id) {
		t.Errorf("NewRevisionID() = %q, want 12 lowercase hex characters", id)
	}
}

func TestNewRevisionID_Unique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := migrate.NewRevisionID()
		if err != nil {
			t.Fatalf("NewRevisionID failed: %v", err)
		}
		if seen[id] {
			t.Fatalf("NewRevisionID produced a duplicate: %q", id)
		}
		seen[id] = true
	}
}

func TestHead_NoMigrations(t *testing.T) {
	got, err := migrate.Head(nil)
	if err != nil {
		t.Fatalf("Head(nil) failed: %v", err)
	}
	if got != "none" {
		t.Errorf("Head(nil) = %q, want %q", got, "none")
	}
}

func TestHead_LinearChain(t *testing.T) {
	migrations := []migrate.Migration{
		{Revision: "c", DownRevision: "b"},
		{Revision: "a", DownRevision: "none"},
		{Revision: "b", DownRevision: "a"},
	}
	got, err := migrate.Head(migrations)
	if err != nil {
		t.Fatalf("Head failed: %v", err)
	}
	if got != "c" {
		t.Errorf("Head() = %q, want %q", got, "c")
	}
}

func TestHead_Branch_Error(t *testing.T) {
	migrations := []migrate.Migration{
		{Revision: "a", DownRevision: "none"},
		{Revision: "b1", DownRevision: "a"},
		{Revision: "b2", DownRevision: "a"},
	}
	if _, err := migrate.Head(migrations); err == nil {
		t.Fatal("expected an error for a branched revision chain, got nil")
	}
}

func TestHead_Cycle_Error(t *testing.T) {
	migrations := []migrate.Migration{
		{Revision: "a", DownRevision: "b"},
		{Revision: "b", DownRevision: "a"},
	}
	if _, err := migrate.Head(migrations); err == nil {
		t.Fatal("expected an error for a cyclic revision chain, got nil")
	}
}
