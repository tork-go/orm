package postgres

import (
	"context"
	"fmt"

	"github.com/tork-go/orm/driver"
)

// historyTable is the name of the migrations history table this dialect
// creates and maintains. It is fixed, hand-written DDL, not routed
// through the general Render* machinery: it is a single, permanent
// bootstrap table, not something a diff is ever run against.
const historyTable = "tork_migrations"

const ensureHistoryTableSQL = `
CREATE TABLE IF NOT EXISTS ` + historyTable + ` (
    revision      TEXT PRIMARY KEY,
    down_revision TEXT,
    applied_at    TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// EnsureHistoryTable creates the migrations history table if it doesn't
// already exist. It is safe to call more than once.
func (Dialect) EnsureHistoryTable(ctx context.Context, conn driver.Conn) error {
	if _, err := conn.Exec(ctx, ensureHistoryTableSQL); err != nil {
		return fmt.Errorf("postgres: ensuring history table: %w", err)
	}
	return nil
}

// InsertHistoryRow records that revision has been applied.
func (Dialect) InsertHistoryRow(ctx context.Context, exec driver.Execer, revision, downRevision string) error {
	sql := `INSERT INTO ` + historyTable + ` (revision, down_revision) VALUES ($1, $2)`
	if _, err := exec.Exec(ctx, sql, revision, downRevision); err != nil {
		return fmt.Errorf("postgres: recording revision %s as applied: %w", revision, err)
	}
	return nil
}

// DeleteHistoryRow removes revision's history record.
func (Dialect) DeleteHistoryRow(ctx context.Context, exec driver.Execer, revision string) error {
	sql := `DELETE FROM ` + historyTable + ` WHERE revision = $1`
	if _, err := exec.Exec(ctx, sql, revision); err != nil {
		return fmt.Errorf("postgres: removing revision %s from history: %w", revision, err)
	}
	return nil
}

// AppliedRevisions returns every revision recorded as applied.
func (Dialect) AppliedRevisions(ctx context.Context, exec driver.Execer) ([]driver.AppliedRevision, error) {
	rows, err := exec.Query(ctx, `SELECT revision, applied_at FROM `+historyTable)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading applied revisions: %w", err)
	}
	defer rows.Close()

	var revisions []driver.AppliedRevision
	for rows.Next() {
		var r driver.AppliedRevision
		if err := rows.Scan(&r.Revision, &r.AppliedAt); err != nil {
			return nil, fmt.Errorf("postgres: scanning applied revision: %w", err)
		}
		revisions = append(revisions, r)
	}
	return revisions, rows.Err()
}
