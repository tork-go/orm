package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

// MakeMigrations diffs the live schema for models' tables against models
// themselves, and if there are any differences, writes a new migration
// file under dir and returns it. It returns (nil, nil) if there is
// nothing to do, matching Alembic's own behavior of never writing an
// empty migration.
//
// Which database this is comes from dsn's scheme; see driver.For.
func MakeMigrations(ctx context.Context, dsn, dir, message string, models ...orm.Model) (*migrate.Migration, error) {
	dialect, err := driver.For(dsn)
	if err != nil {
		return nil, err
	}
	conn, err := dialect.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("cli: connecting: %w", err)
	}
	defer conn.Close(ctx)

	tables := make([]string, len(models))
	for i, m := range models {
		tables[i] = m.TableName()
	}

	current, err := dialect.Introspect(ctx, conn, tables)
	if err != nil {
		return nil, fmt.Errorf("cli: introspecting current schema: %w", err)
	}
	desired, err := schema.ExtractSchema(models...)
	if err != nil {
		return nil, fmt.Errorf("cli: extracting desired schema: %w", err)
	}

	upOps, err := migrate.Diff(current, desired)
	if err != nil {
		return nil, fmt.Errorf("cli: diffing up: %w", err)
	}
	if len(upOps) == 0 {
		return nil, nil
	}
	downOps, err := migrate.Diff(desired, current)
	if err != nil {
		return nil, fmt.Errorf("cli: diffing down: %w", err)
	}

	upSQL, err := migrate.Generate(dialect, upOps)
	if err != nil {
		return nil, fmt.Errorf("cli: rendering up SQL: %w", err)
	}
	downSQL, err := migrate.Generate(dialect, downOps)
	if err != nil {
		return nil, fmt.Errorf("cli: rendering down SQL: %w", err)
	}

	existing, err := migrate.LoadAll(dir)
	if err != nil {
		return nil, err
	}
	head, err := migrate.Head(existing)
	if err != nil {
		return nil, err
	}
	revision, err := migrate.NewRevisionID()
	if err != nil {
		return nil, err
	}

	m := migrate.Migration{Revision: revision, DownRevision: head, UpSQL: upSQL, DownSQL: downSQL}
	if _, err := migrate.Write(dir, m, message); err != nil {
		return nil, err
	}
	return &m, nil
}

func runMakeMigrations(ctx context.Context, args []string, out, errOut io.Writer, dsn, dir string, models []orm.Model) int {
	fs := flag.NewFlagSet("makemigrations", flag.ContinueOnError)
	fs.SetOutput(errOut)
	message := fs.String("m", "", "short message describing this migration")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	m, err := MakeMigrations(ctx, dsn, dir, *message, models...)
	if err != nil {
		fmt.Fprintln(errOut, "makemigrations:", err)
		return 1
	}
	if m == nil {
		fmt.Fprintln(out, "No changes detected")
		return 0
	}
	fmt.Fprintf(out, "Wrote revision %s\n", m.Revision)
	return 0
}
