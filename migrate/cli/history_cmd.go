package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/tork-go/orm/driver"
	"github.com/tork-go/orm/migrate"
)

func runHistory(ctx context.Context, _ []string, out, errOut io.Writer, dialect driver.Dialect, dsn, dir string) int {
	conn, migrations, err := connectAndLoad(ctx, dialect, dsn, dir)
	if err != nil {
		fmt.Fprintln(errOut, "history:", err)
		return 1
	}
	defer conn.Close(ctx)

	entries, err := migrate.History(ctx, dialect, conn, migrations)
	if err != nil {
		fmt.Fprintln(errOut, "history:", err)
		return 1
	}

	for _, e := range entries {
		status := "pending"
		if e.Applied {
			status = "applied " + e.AppliedAt.Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(out, "%s  %s\n", e.Migration.Revision, status)
	}
	return 0
}
