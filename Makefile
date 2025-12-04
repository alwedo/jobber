.PHONY: check test lint migrate-up migrate-down init logfile compose-up

POSTGRES_PASSWORD ?= password

# https://github.com/golang-migrate/migrate/blob/master/database/postgres/TUTORIAL.md
migrate-up:
	@migrate -database postgres://jobber:$(POSTGRES_PASSWORD)@localhost:5432/jobber?sslmode=disable -path db/migrations up

migrate-down:
	@migrate -database postgres://jobber:$(POSTGRES_PASSWORD)@localhost:5432/jobber?sslmode=disable -path db/migrations down 1

check: lint test

test:
	@go test ./...

lint:
	@golangci-lint run

init: logfile compose-up migrate-up

logfile:
	@touch jobber.log

compose-up:
	POSTGRES_PASSWORD=$(POSTGRES_PASSWORD) docker compose up -d
