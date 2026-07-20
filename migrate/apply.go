package migrate

import (
	"context"
	"fmt"

	"github.com/tork-go/orm/driver"
)

// Apply connects using dialect, loads migrations from dir, and applies
// every pending one up to head. Call it once at your application's
// startup to bring the database schema up to date, the same role as
// SQLModel.metadata.create_all(engine) in SQLAlchemy or Drizzle's
// migrate(db, {...}):
//
//	err := migrate.Apply(ctx, postgres.Dialect{}, dsn, "migrations")
func Apply(ctx context.Context, dialect driver.Dialect, dsn, dir string) error {
	conn, err := dialect.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("migrate: connecting: %w", err)
	}
	defer conn.Close(ctx)

	if err := dialect.EnsureHistoryTable(ctx, conn); err != nil {
		return err
	}

	migrations, err := LoadAll(dir)
	if err != nil {
		return err
	}

	return Up(ctx, dialect, conn, migrations, HeadTarget())
}
