.PHONY: check test lint migrate-up migrate-down

# https://github.com/golang-migrate/migrate/blob/master/database/postgres/TUTORIAL.md
migrate-up:
	migrate -database postgres://jobber:$(POSTGRES_PASSWORD)@localhost:5432/jobber?sslmode=disable -path db/migrations up

migrate-down:
	migrate -database postgres://jobber:$(POSTGRES_PASSWORD)@localhost:5432/jobber?sslmode=disable -path db/migrations down 1

check: lint test

test:
	@go test ./...

lint:
	@golangci-lint run
