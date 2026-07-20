//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/tork-go/orm/driver"
	"github.com/tork-go/orm/driver/postgres"
)

func TestEnsureHistoryTable_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer conn.Close(ctx)

	if err := dialect.EnsureHistoryTable(ctx, conn); err != nil {
		t.Fatalf("EnsureHistoryTable (1st call) failed: %v", err)
	}
	if err := dialect.EnsureHistoryTable(ctx, conn); err != nil {
		t.Fatalf("EnsureHistoryTable (2nd call) failed: %v", err)
	}
}

func TestHistoryRows_InsertDeleteAndList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	// t.Cleanup, not defer: t.Cleanup callbacks run after the test
	// function returns, which is after any plain defer in it has already
	// fired. Closing conn must be registered first (via t.Cleanup, not
	// defer) so it still runs last, after the row cleanup below that
	// needs conn open.
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	if err := dialect.EnsureHistoryTable(ctx, conn); err != nil {
		t.Fatalf("EnsureHistoryTable failed: %v", err)
	}
	t.Cleanup(func() {
		_ = dialect.DeleteHistoryRow(context.Background(), conn, "test_history_rev_a")
		_ = dialect.DeleteHistoryRow(context.Background(), conn, "test_history_rev_b")
	})

	if err := dialect.InsertHistoryRow(ctx, conn, "test_history_rev_a", "none"); err != nil {
		t.Fatalf("InsertHistoryRow(a) failed: %v", err)
	}
	if err := dialect.InsertHistoryRow(ctx, conn, "test_history_rev_b", "test_history_rev_a"); err != nil {
		t.Fatalf("InsertHistoryRow(b) failed: %v", err)
	}

	revisions, err := dialect.AppliedRevisions(ctx, conn)
	if err != nil {
		t.Fatalf("AppliedRevisions failed: %v", err)
	}
	if !containsRevision(revisions, "test_history_rev_a") || !containsRevision(revisions, "test_history_rev_b") {
		t.Fatalf("AppliedRevisions() = %+v, want it to contain both test rows", revisions)
	}
	for _, r := range revisions {
		if r.AppliedAt.IsZero() {
			t.Errorf("revision %s has a zero AppliedAt", r.Revision)
		}
	}

	if err := dialect.DeleteHistoryRow(ctx, conn, "test_history_rev_b"); err != nil {
		t.Fatalf("DeleteHistoryRow(b) failed: %v", err)
	}

	revisions, err = dialect.AppliedRevisions(ctx, conn)
	if err != nil {
		t.Fatalf("AppliedRevisions failed: %v", err)
	}
	if containsRevision(revisions, "test_history_rev_b") {
		t.Fatalf("AppliedRevisions() = %+v, want test_history_rev_b removed", revisions)
	}
	if !containsRevision(revisions, "test_history_rev_a") {
		t.Fatalf("AppliedRevisions() = %+v, want test_history_rev_a still present", revisions)
	}
}

func containsRevision(revisions []driver.AppliedRevision, revision string) bool {
	for _, r := range revisions {
		if r.Revision == revision {
			return true
		}
	}
	return false
}
