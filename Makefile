.PHONY: test test-cover bench integration test-cover-integration

# The packages actually shipped as part of Tork ORM. Coverage is scoped
# to these, excluding tests/fakedriver and tests/fixtures (test support
# code, not product code).
PRODUCT_PKGS := github.com/tork-go/orm,github.com/tork-go/orm/driver,github.com/tork-go/orm/driver/postgres,github.com/tork-go/orm/migrate,github.com/tork-go/orm/migrate/cli,github.com/tork-go/orm/schema

# Unit tests only (no live database required).
test:
	go test ./...

# Unit test coverage for the shipped packages. Tests live under tests/,
# outside the package directories they exercise, so -coverpkg is
# required. Without it, go test has no local package to instrument and
# reports "no statements" instead of real coverage.
test-cover:
	go test -coverpkg=$(PRODUCT_PKGS) -coverprofile=coverage.out ./tests/...
	go tool cover -func=coverage.out

bench:
	go test -bench=. -benchmem -run=^$$ ./...

# Starts a local Postgres via Docker and runs every integration test
# against it. Requires Docker.
integration:
	docker compose up -d --wait
	go test -tags=integration ./tests/postgres/...
	docker compose down

# Combined unit + integration coverage for the shipped packages. This is
# the only way to reach driver/postgres's introspection and
# migrations-history code, which need a live database. Requires Docker.
test-cover-integration:
	docker compose up -d --wait
	go test -tags=integration -coverpkg=$(PRODUCT_PKGS) -coverprofile=coverage.out ./tests/...
	go tool cover -func=coverage.out
	docker compose down
