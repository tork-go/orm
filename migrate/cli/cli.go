package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver"
)

const usage = `usage:
  makemigrations [-m message]
  migrate up   {head|+N|<revision>}
  migrate down {base|-N|<revision>}
  history
`

// Run parses os.Args[1:] and runs the requested subcommand (makemigrations,
// migrate up, migrate down, history) against dialect and dsn, using
// models for makemigrations. It returns a process exit code suitable for
// os.Exit:
//
//	func main() {
//	    os.Exit(cli.Run(postgres.Dialect{}, os.Getenv("DATABASE_URL"), "migrations",
//	        models.User, models.Post))
//	}
func Run(dialect driver.Dialect, dsn, migrationsDir string, models ...orm.Model) int {
	return RunWithArgs(os.Args[1:], os.Stdout, os.Stderr, dialect, dsn, migrationsDir, models...)
}

// RunWithArgs is Run with its arguments and output streams explicit,
// letting callers (including this package's own tests) drive it without
// touching the real process's os.Args or stdio.
func RunWithArgs(args []string, out, errOut io.Writer, dialect driver.Dialect, dsn, migrationsDir string, models ...orm.Model) int {
	if migrationsDir == "" {
		migrationsDir = "migrations"
	}
	if len(args) == 0 {
		fmt.Fprint(errOut, usage)
		return 2
	}

	ctx := context.Background()
	switch args[0] {
	case "makemigrations":
		return runMakeMigrations(ctx, args[1:], out, errOut, dialect, dsn, migrationsDir, models)
	case "migrate":
		if len(args) < 2 {
			fmt.Fprint(errOut, usage)
			return 2
		}
		switch args[1] {
		case "up":
			return runMigrateUp(ctx, args[2:], out, errOut, dialect, dsn, migrationsDir)
		case "down":
			return runMigrateDown(ctx, args[2:], out, errOut, dialect, dsn, migrationsDir)
		default:
			fmt.Fprint(errOut, usage)
			return 2
		}
	case "history":
		return runHistory(ctx, args[1:], out, errOut, dialect, dsn, migrationsDir)
	default:
		fmt.Fprint(errOut, usage)
		return 2
	}
}
