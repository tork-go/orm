package migrate

import (
	"context"
	"fmt"
	"time"

	"github.com/tork-go/orm/driver"
)

// Up applies pending migrations, in chain order, up to target (HeadTarget,
// StepsTarget, or RevisionTarget; BaseTarget is rejected). Each migration
// runs in its own transaction: its SQL, then its history row, then
// commit. If a migration fails, every migration before it stays
// committed, and the error names the first one that failed.
func Up(ctx context.Context, dialect driver.Dialect, conn driver.Conn, migrations []Migration, target Target) error {
	if target.kind == targetBase {
		return fmt.Errorf("migrate: Up does not accept a base target")
	}

	ordered, err := chainOrder(migrations)
	if err != nil {
		return err
	}
	applied, err := appliedSet(ctx, dialect, conn)
	if err != nil {
		return err
	}
	splitAt, err := splitApplied(ordered, applied)
	if err != nil {
		return err
	}
	pending := ordered[splitAt:]

	toApply, err := selectUpTargets(pending, target)
	if err != nil {
		return err
	}

	for _, m := range toApply {
		if err := applyOne(ctx, dialect, conn, m); err != nil {
			return fmt.Errorf("migrate: applying revision %s: %w", m.Revision, err)
		}
	}
	return nil
}

// Down rolls back applied migrations, most recent first, down to target
// (BaseTarget, StepsTarget, or RevisionTarget; HeadTarget is rejected).
// Each rollback runs in its own transaction: DownSQL, then removing its
// history row, then commit.
func Down(ctx context.Context, dialect driver.Dialect, conn driver.Conn, migrations []Migration, target Target) error {
	if target.kind == targetHead {
		return fmt.Errorf("migrate: Down does not accept a head target")
	}

	ordered, err := chainOrder(migrations)
	if err != nil {
		return err
	}
	applied, err := appliedSet(ctx, dialect, conn)
	if err != nil {
		return err
	}
	splitAt, err := splitApplied(ordered, applied)
	if err != nil {
		return err
	}
	appliedInOrder := ordered[:splitAt]

	toRevert, err := selectDownTargets(appliedInOrder, target)
	if err != nil {
		return err
	}

	for _, m := range toRevert {
		if err := revertOne(ctx, dialect, conn, m); err != nil {
			return fmt.Errorf("migrate: rolling back revision %s: %w", m.Revision, err)
		}
	}
	return nil
}

// Entry is one migration's status in History's output.
type Entry struct {
	Migration Migration
	Applied   bool
	AppliedAt time.Time
}

// History returns every migration in chain order, annotated with whether
// it's applied and, if so, when.
func History(ctx context.Context, dialect driver.Dialect, conn driver.Conn, migrations []Migration) ([]Entry, error) {
	ordered, err := chainOrder(migrations)
	if err != nil {
		return nil, err
	}
	revs, err := dialect.AppliedRevisions(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("migrate: reading applied revisions: %w", err)
	}

	appliedAt := make(map[string]time.Time, len(revs))
	for _, r := range revs {
		appliedAt[r.Revision] = r.AppliedAt
	}

	entries := make([]Entry, len(ordered))
	for i, m := range ordered {
		at, ok := appliedAt[m.Revision]
		entries[i] = Entry{Migration: m, Applied: ok, AppliedAt: at}
	}
	return entries, nil
}

func applyOne(ctx context.Context, dialect driver.Dialect, conn driver.Conn, m Migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, m.UpSQL); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := dialect.InsertHistoryRow(ctx, tx, m.Revision, m.DownRevision); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func revertOne(ctx context.Context, dialect driver.Dialect, conn driver.Conn, m Migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, m.DownSQL); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := dialect.DeleteHistoryRow(ctx, tx, m.Revision); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func appliedSet(ctx context.Context, dialect driver.Dialect, conn driver.Conn) (map[string]bool, error) {
	revs, err := dialect.AppliedRevisions(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("migrate: reading applied revisions: %w", err)
	}
	set := make(map[string]bool, len(revs))
	for _, r := range revs {
		set[r.Revision] = true
	}
	return set, nil
}

// splitApplied validates that applied migrations form a prefix of ordered
// (the chain-ordered migrations) and returns the index where that prefix
// ends: ordered[:i] are applied, ordered[i:] are pending. A revision
// applied out of chain order means the history table and migration files
// have drifted apart, which is an error, not something to guess past.
func splitApplied(ordered []Migration, applied map[string]bool) (int, error) {
	i := 0
	for i < len(ordered) && applied[ordered[i].Revision] {
		i++
	}
	for j := i; j < len(ordered); j++ {
		if applied[ordered[j].Revision] {
			return 0, fmt.Errorf("migrate: revision %s is applied out of chain order, history table is inconsistent with migration files", ordered[j].Revision)
		}
	}
	return i, nil
}

func selectUpTargets(pending []Migration, target Target) ([]Migration, error) {
	switch target.kind {
	case targetHead:
		return pending, nil
	case targetSteps:
		n := target.steps
		if n > len(pending) {
			n = len(pending)
		}
		return pending[:n], nil
	case targetRevision:
		for i, m := range pending {
			if m.Revision == target.revision {
				return pending[:i+1], nil
			}
		}
		return nil, fmt.Errorf("migrate: revision %s not found among pending migrations", target.revision)
	default:
		return nil, fmt.Errorf("migrate: unsupported target for Up")
	}
}

func selectDownTargets(appliedInOrder []Migration, target Target) ([]Migration, error) {
	switch target.kind {
	case targetBase:
		return reversed(appliedInOrder), nil
	case targetSteps:
		n := len(appliedInOrder)
		k := target.steps
		if k > n {
			k = n
		}
		return reversed(appliedInOrder[n-k:]), nil
	case targetRevision:
		for i, m := range appliedInOrder {
			if m.Revision == target.revision {
				return reversed(appliedInOrder[i+1:]), nil
			}
		}
		return nil, fmt.Errorf("migrate: revision %s not found among applied migrations", target.revision)
	default:
		return nil, fmt.Errorf("migrate: unsupported target for Down")
	}
}

func reversed(ms []Migration) []Migration {
	out := make([]Migration, len(ms))
	for i, m := range ms {
		out[len(ms)-1-i] = m
	}
	return out
}
