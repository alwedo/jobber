DB_CONN ?= postgres://jobber:password@localhost:5432/jobber?sslmode=disable

# https://github.com/golang-migrate/migrate/blob/master/database/postgres/TUTORIAL.md
.PHONY: migrate-up
migrate-up:
	@result=$$(migrate -database $(DB_CONN) -path db/migrations up 2>&1); \
	echo "Migrating DB: $$result"

.PHONY: migrate-down
migrate-down:
	@migrate -database $(DB_CONN) -path db/migrations down 1

.PHONY: check
check: lint test

.PHONY: test
test:
	@go test ./...

.PHONY: lint
lint:
	@golangci-lint run

.PHONY: build
build: migrate-up
	DB_CONN=$(DB_CONN) docker compose up -d --build

.PHONY: db-up
db-up:
	@{ \
		if docker ps -a --format '{{.Names}}' | grep -q '^jobber-postgres-dev$$'; then \
		    docker start jobber-postgres-dev; \
		else \
			docker run --name jobber-postgres-dev \
				-e POSTGRES_DB=jobber \
				-e POSTGRES_USER=jobber \
				-e POSTGRES_PASSWORD=password \
				-p 5432:5432 \
				-v $(PWD)/postgres-data:/var/lib/postgresql \
				--health-cmd="pg_isready -U jobber -d jobber" \
				--health-interval=5s \
				--health-timeout=3s \
				--health-retries=10 \
				--restart unless-stopped \
				-d postgres:latest; \
		fi; \
		echo "Waiting for Postgres to become healthy..."; \
		tries=0; \
		while [ "$$(docker inspect -f '{{.State.Health.Status}}' jobber-postgres-dev)" != "healthy" ]; do \
			sleep 1; tries=$$((tries+1)); \
			if [ $$tries -ge 30 ]; then \
				echo "Postgres did not become healthy after 30 seconds."; \
				docker inspect -f '{{json .State.Health}}' jobber-postgres-dev; \
				exit 1; \
			fi; \
		done; \
		echo "Postgres is healthy."; \
	}

.PHONY: run
run: db-up mod migrate-up
	@echo "Starting dev server..."
	@DB_CONN=$(DB_CONN) go run main.go; \
	exit 0

.PHONY: mod
mod:
	@echo "Getting dependencies..."
	@go mod download
