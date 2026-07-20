package migrate_test

import (
	"context"
	"testing"
	"time"

	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/tests/fakedriver"
)

// chain3 returns a linear three-migration chain none -> a -> b -> c.
func chain3() []migrate.Migration {
	return []migrate.Migration{
		{Revision: "a", DownRevision: "none", UpSQL: "UP A", DownSQL: "DOWN A"},
		{Revision: "b", DownRevision: "a", UpSQL: "UP B", DownSQL: "DOWN B"},
		{Revision: "c", DownRevision: "b", UpSQL: "UP C", DownSQL: "DOWN C"},
	}
}

func TestUp_HeadTarget_AppliesAllPendingInOrder(t *testing.T) {
	ctx := context.Background()
	conn := fakedriver.NewConn()
	dialect := fakedriver.NewDialect()

	if err := migrate.Up(ctx, dialect, conn, chain3(), migrate.HeadTarget()); err != nil {
		t.Fatalf("Up failed: %v", err)
	}

	wantExecs := []string{"UP A", "UP B", "UP C"}
	if got := conn.ExecCalls(); !equalStrings(got, wantExecs) {
		t.Errorf("ExecCalls() = %v, want %v", got, wantExecs)
	}
	assertApplied(t, ctx, dialect, "a", "b", "c")
}

func TestUp_StepsTarget_AppliesOnlyThatMany(t *testing.T) {
	ctx := context.Background()
	conn := fakedriver.NewConn()
	dialect := fakedriver.NewDialect()

	target, err := migrate.ParseTarget("+2")
	if err != nil {
		t.Fatalf("ParseTarget failed: %v", err)
	}
	if err := migrate.Up(ctx, dialect, conn, chain3(), target); err != nil {
		t.Fatalf("Up failed: %v", err)
	}

	assertApplied(t, ctx, dialect, "a", "b")
	assertNotApplied(t, ctx, dialect, "c")
}

func TestUp_RevisionTarget_AppliesUpToAndIncluding(t *testing.T) {
	ctx := context.Background()
	conn := fakedriver.NewConn()
	dialect := fakedriver.NewDialect()

	if err := migrate.Up(ctx, dialect, conn, chain3(), migrate.RevisionTarget("b")); err != nil {
		t.Fatalf("Up failed: %v", err)
	}

	assertApplied(t, ctx, dialect, "a", "b")
	assertNotApplied(t, ctx, dialect, "c")
}

func TestUp_UnknownRevisionTarget_Error(t *testing.T) {
	ctx := context.Background()
	err := migrate.Up(ctx, fakedriver.NewDialect(), fakedriver.NewConn(), chain3(), migrate.RevisionTarget("does-not-exist"))
	if err == nil {
		t.Fatal("expected an error for an unknown revision target, got nil")
	}
}

func TestUp_RejectsBaseTarget(t *testing.T) {
	ctx := context.Background()
	err := migrate.Up(ctx, fakedriver.NewDialect(), fakedriver.NewConn(), chain3(), migrate.BaseTarget())
	if err == nil {
		t.Fatal("expected Up to reject a base target, got nil")
	}
}

func TestUp_StopsOnFirstFailure(t *testing.T) {
	ctx := context.Background()
	conn := fakedriver.NewConn()
	conn.FailOn("UP B")
	dialect := fakedriver.NewDialect()

	err := migrate.Up(ctx, dialect, conn, chain3(), migrate.HeadTarget())
	if err == nil {
		t.Fatal("expected Up to fail when a migration's Exec fails")
	}

	assertApplied(t, ctx, dialect, "a")
	assertNotApplied(t, ctx, dialect, "b", "c")
	if got := conn.ExecCalls(); !equalStrings(got, []string{"UP A", "UP B"}) {
		t.Errorf("ExecCalls() = %v, want [UP A, UP B] (C must never be attempted)", got)
	}
}

func TestDown_BaseTarget_RollsBackAllInReverseOrder(t *testing.T) {
	ctx := context.Background()
	conn := fakedriver.NewConn()
	dialect := fakedriver.NewDialect()
	if err := migrate.Up(ctx, dialect, conn, chain3(), migrate.HeadTarget()); err != nil {
		t.Fatalf("Up (setup) failed: %v", err)
	}

	if err := migrate.Down(ctx, dialect, conn, chain3(), migrate.BaseTarget()); err != nil {
		t.Fatalf("Down failed: %v", err)
	}

	assertNotApplied(t, ctx, dialect, "a", "b", "c")
	wantExecs := []string{"UP A", "UP B", "UP C", "DOWN C", "DOWN B", "DOWN A"}
	if got := conn.ExecCalls(); !equalStrings(got, wantExecs) {
		t.Errorf("ExecCalls() = %v, want %v", got, wantExecs)
	}
}

func TestDown_StepsTarget_RollsBackOnlyThatMany(t *testing.T) {
	ctx := context.Background()
	conn := fakedriver.NewConn()
	dialect := fakedriver.NewDialect()
	if err := migrate.Up(ctx, dialect, conn, chain3(), migrate.HeadTarget()); err != nil {
		t.Fatalf("Up (setup) failed: %v", err)
	}

	target, err := migrate.ParseTarget("-1")
	if err != nil {
		t.Fatalf("ParseTarget failed: %v", err)
	}
	if err := migrate.Down(ctx, dialect, conn, chain3(), target); err != nil {
		t.Fatalf("Down failed: %v", err)
	}

	assertApplied(t, ctx, dialect, "a", "b")
	assertNotApplied(t, ctx, dialect, "c")
}

func TestDown_RevisionTarget_RollsBackAfterThatRevision(t *testing.T) {
	ctx := context.Background()
	conn := fakedriver.NewConn()
	dialect := fakedriver.NewDialect()
	if err := migrate.Up(ctx, dialect, conn, chain3(), migrate.HeadTarget()); err != nil {
		t.Fatalf("Up (setup) failed: %v", err)
	}

	if err := migrate.Down(ctx, dialect, conn, chain3(), migrate.RevisionTarget("a")); err != nil {
		t.Fatalf("Down failed: %v", err)
	}

	assertApplied(t, ctx, dialect, "a")
	assertNotApplied(t, ctx, dialect, "b", "c")
}

func TestDown_RejectsHeadTarget(t *testing.T) {
	ctx := context.Background()
	err := migrate.Down(ctx, fakedriver.NewDialect(), fakedriver.NewConn(), chain3(), migrate.HeadTarget())
	if err == nil {
		t.Fatal("expected Down to reject a head target, got nil")
	}
}

func TestUpDown_OutOfOrderHistory_Error(t *testing.T) {
	ctx := context.Background()
	conn := fakedriver.NewConn()
	dialect := fakedriver.NewDialect()
	// Seed "b" as applied without "a": inconsistent with chain order.
	dialect.SeedApplied("b", time.Now())

	if err := migrate.Up(ctx, dialect, conn, chain3(), migrate.HeadTarget()); err == nil {
		t.Fatal("expected Up to reject an out-of-chain-order applied set, got nil")
	}
	if err := migrate.Down(ctx, dialect, conn, chain3(), migrate.BaseTarget()); err == nil {
		t.Fatal("expected Down to reject an out-of-chain-order applied set, got nil")
	}
}

func TestUp_BeginFailure_Error(t *testing.T) {
	ctx := context.Background()
	conn := fakedriver.NewConn()
	conn.FailBegin = true

	if err := migrate.Up(ctx, fakedriver.NewDialect(), conn, chain3(), migrate.HeadTarget()); err == nil {
		t.Fatal("expected Up to fail when Begin fails, got nil")
	}
}

func TestDown_BeginFailure_Error(t *testing.T) {
	ctx := context.Background()
	conn := fakedriver.NewConn()
	dialect := fakedriver.NewDialect()
	if err := migrate.Up(ctx, dialect, conn, chain3(), migrate.HeadTarget()); err != nil {
		t.Fatalf("Up (setup) failed: %v", err)
	}

	conn.FailBegin = true
	if err := migrate.Down(ctx, dialect, conn, chain3(), migrate.BaseTarget()); err == nil {
		t.Fatal("expected Down to fail when Begin fails, got nil")
	}
}

func TestUp_InsertHistoryRowFailure_RollsBackTheMigration(t *testing.T) {
	ctx := context.Background()
	conn := fakedriver.NewConn()
	dialect := fakedriver.NewDialect()
	dialect.FailHistory = true

	if err := migrate.Up(ctx, dialect, conn, chain3(), migrate.HeadTarget()); err == nil {
		t.Fatal("expected Up to fail when InsertHistoryRow fails, got nil")
	}
	assertNotApplied(t, ctx, dialect, "a", "b", "c")
}

func TestDown_DeleteHistoryRowFailure_Error(t *testing.T) {
	ctx := context.Background()
	conn := fakedriver.NewConn()
	dialect := fakedriver.NewDialect()
	if err := migrate.Up(ctx, dialect, conn, chain3(), migrate.HeadTarget()); err != nil {
		t.Fatalf("Up (setup) failed: %v", err)
	}

	dialect.FailHistory = true
	if err := migrate.Down(ctx, dialect, conn, chain3(), migrate.BaseTarget()); err == nil {
		t.Fatal("expected Down to fail when DeleteHistoryRow fails, got nil")
	}
}

func TestHistory_ChainOrderedWithStatus(t *testing.T) {
	ctx := context.Background()
	conn := fakedriver.NewConn()
	dialect := fakedriver.NewDialect()
	if err := migrate.Up(ctx, dialect, conn, chain3(), migrate.RevisionTarget("b")); err != nil {
		t.Fatalf("Up (setup) failed: %v", err)
	}

	entries, err := migrate.History(ctx, dialect, conn, chain3())
	if err != nil {
		t.Fatalf("History failed: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].Migration.Revision != "a" || entries[1].Migration.Revision != "b" || entries[2].Migration.Revision != "c" {
		t.Fatalf("entries out of chain order: %+v", entries)
	}
	if !entries[0].Applied || !entries[1].Applied {
		t.Errorf("expected a and b to be applied: %+v", entries[:2])
	}
	if entries[2].Applied {
		t.Errorf("expected c to be pending: %+v", entries[2])
	}
	if entries[0].AppliedAt.IsZero() {
		t.Error("expected entries[0].AppliedAt to be set")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func assertApplied(t *testing.T, ctx context.Context, dialect *fakedriver.Dialect, revisions ...string) {
	t.Helper()
	applied, err := dialect.AppliedRevisions(ctx, nil)
	if err != nil {
		t.Fatalf("AppliedRevisions failed: %v", err)
	}
	set := map[string]bool{}
	for _, r := range applied {
		set[r.Revision] = true
	}
	for _, rev := range revisions {
		if !set[rev] {
			t.Errorf("expected revision %s to be applied, applied = %v", rev, applied)
		}
	}
}

func assertNotApplied(t *testing.T, ctx context.Context, dialect *fakedriver.Dialect, revisions ...string) {
	t.Helper()
	applied, err := dialect.AppliedRevisions(ctx, nil)
	if err != nil {
		t.Fatalf("AppliedRevisions failed: %v", err)
	}
	set := map[string]bool{}
	for _, r := range applied {
		set[r.Revision] = true
	}
	for _, rev := range revisions {
		if set[rev] {
			t.Errorf("expected revision %s to not be applied, applied = %v", rev, applied)
		}
	}
}
