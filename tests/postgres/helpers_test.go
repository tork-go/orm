//go:build integration

package postgres_test

import "os"

const defaultDSN = "postgres://tork:tork@localhost:5432/tork_orm_dev?sslmode=disable"

// dsn returns the Postgres connection string used by every integration
// test in this package: TORK_ORM_POSTGRES_DSN if set, otherwise the
// credentials in docker-compose.yml.
func dsn() string {
	if v := os.Getenv("TORK_ORM_POSTGRES_DSN"); v != "" {
		return v
	}
	return defaultDSN
}
