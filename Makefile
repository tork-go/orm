.PHONY: test test-cover bench integration

# Unit tests only (no live database required).
test:
	go test ./...

# Unit test coverage for the orm package. Tests live under tests/, outside
# the package directories they exercise, so -coverpkg is required. Without
# it, go test has no local package to instrument and reports "no
# statements" instead of real coverage.
test-cover:
	go test -coverpkg=./... -coverprofile=coverage.out ./tests/...
	go tool cover -func=coverage.out

bench:
	go test -bench=. -benchmem -run=^$$ ./...

# Starts a local Postgres via Docker and runs the pgx connectivity smoke
# test against it. Requires Docker.
integration:
	docker compose up -d --wait
	go test -tags=integration ./tests/postgres/...
	docker compose down
