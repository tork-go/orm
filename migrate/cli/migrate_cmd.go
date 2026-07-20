package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/tork-go/orm/driver"
	"github.com/tork-go/orm/migrate"
)

func runMigrateUp(ctx context.Context, args []string, out, errOut io.Writer, dialect driver.Dialect, dsn, dir string) int {
	target, err := parseTargetArg(args, "migrate up head")
	if err != nil {
		fmt.Fprintln(errOut, "migrate up:", err)
		return 2
	}

	conn, migrations, err := connectAndLoad(ctx, dialect, dsn, dir)
	if err != nil {
		fmt.Fprintln(errOut, "migrate up:", err)
		return 1
	}
	defer conn.Close(ctx)

	if err := migrate.Up(ctx, dialect, conn, migrations, target); err != nil {
		fmt.Fprintln(errOut, "migrate up:", err)
		return 1
	}
	fmt.Fprintln(out, "OK")
	return 0
}

func runMigrateDown(ctx context.Context, args []string, out, errOut io.Writer, dialect driver.Dialect, dsn, dir string) int {
	target, err := parseTargetArg(args, "migrate down base")
	if err != nil {
		fmt.Fprintln(errOut, "migrate down:", err)
		return 2
	}

	conn, migrations, err := connectAndLoad(ctx, dialect, dsn, dir)
	if err != nil {
		fmt.Fprintln(errOut, "migrate down:", err)
		return 1
	}
	defer conn.Close(ctx)

	if err := migrate.Down(ctx, dialect, conn, migrations, target); err != nil {
		fmt.Fprintln(errOut, "migrate down:", err)
		return 1
	}
	fmt.Fprintln(out, "OK")
	return 0
}

// parseTargetArg parses args[0] as a migrate.Target. example is shown in
// the error message when args is empty, e.g. "migrate up head".
func parseTargetArg(args []string, example string) (migrate.Target, error) {
	if len(args) == 0 {
		return migrate.Target{}, fmt.Errorf("missing target, e.g. %q", example)
	}
	return migrate.ParseTarget(args[0])
}

// connectAndLoad connects, ensures the history table exists, and loads
// every migration file from dir. On error it closes any connection it
// opened.
func connectAndLoad(ctx context.Context, dialect driver.Dialect, dsn, dir string) (driver.Conn, []migrate.Migration, error) {
	conn, err := dialect.Connect(ctx, dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("connecting: %w", err)
	}
	if err := dialect.EnsureHistoryTable(ctx, conn); err != nil {
		_ = conn.Close(ctx)
		return nil, nil, err
	}
	migrations, err := migrate.LoadAll(dir)
	if err != nil {
		_ = conn.Close(ctx)
		return nil, nil, err
	}
	return conn, migrations, nil
}
