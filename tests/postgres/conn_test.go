//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/tork-go/orm/driver/postgres"
)

func TestDialect_Name(t *testing.T) {
	if got, want := (postgres.Dialect{}).Name(), "postgres"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestConn_QueryRow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := (postgres.Dialect{}).Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	var got int
	if err := conn.QueryRow(ctx, "SELECT 1").Scan(&got); err != nil {
		t.Fatalf("QueryRow(SELECT 1).Scan failed: %v", err)
	}
	if got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

func TestTx_QueryAndQueryRow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := (postgres.Dialect{}).Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var viaRow int
	if err := tx.QueryRow(ctx, "SELECT 1").Scan(&viaRow); err != nil {
		t.Fatalf("tx.QueryRow(SELECT 1).Scan failed: %v", err)
	}
	if viaRow != 1 {
		t.Errorf("tx.QueryRow: got %d, want 1", viaRow)
	}

	rows, err := tx.Query(ctx, "SELECT 1 UNION ALL SELECT 2 ORDER BY 1")
	if err != nil {
		t.Fatalf("tx.Query failed: %v", err)
	}
	defer rows.Close()

	var got []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("rows.Scan failed: %v", err)
		}
		got = append(got, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() = %v", err)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("tx.Query results = %v, want [1 2]", got)
	}
}

func TestTx_Rollback_DiscardsChanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	t.Cleanup(func() { _ = conn.Exec(context.Background(), `DROP TABLE IF EXISTS test_tx_rollback`) })

	if err := conn.Exec(ctx, `DROP TABLE IF EXISTS test_tx_rollback`); err != nil {
		t.Fatalf("pre-test cleanup failed: %v", err)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}
	if err := tx.Exec(ctx, `CREATE TABLE test_tx_rollback (id INTEGER)`); err != nil {
		t.Fatalf("tx.Exec failed: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	got, err := dialect.Introspect(ctx, conn, []string{"test_tx_rollback"})
	if err != nil {
		t.Fatalf("Introspect failed: %v", err)
	}
	if len(got.Tables) != 0 {
		t.Errorf("table exists after rollback: %+v", got.Tables)
	}
}

// TestHistoryMethods_OnClosedConnection_Errors exercises the error-return
// paths of EnsureHistoryTable, InsertHistoryRow, DeleteHistoryRow, and
// AppliedRevisions: closing the connection first is a simple, realistic
// way to make the underlying Exec/Query calls fail without needing a
// broken database to test against.
func TestHistoryMethods_OnClosedConnection_Errors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if err := dialect.EnsureHistoryTable(ctx, conn); err == nil {
		t.Error("EnsureHistoryTable on a closed connection succeeded, want an error")
	}
	if err := dialect.InsertHistoryRow(ctx, conn, "x", "none"); err == nil {
		t.Error("InsertHistoryRow on a closed connection succeeded, want an error")
	}
	if err := dialect.DeleteHistoryRow(ctx, conn, "x"); err == nil {
		t.Error("DeleteHistoryRow on a closed connection succeeded, want an error")
	}
	if _, err := dialect.AppliedRevisions(ctx, conn); err == nil {
		t.Error("AppliedRevisions on a closed connection succeeded, want an error")
	}
	if _, err := dialect.Introspect(ctx, conn, []string{"users"}); err == nil {
		t.Error("Introspect on a closed connection succeeded, want an error")
	}
}
