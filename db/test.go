package db

import (
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func NewTestContainer(t testing.TB) (string, func()) {
	t.Helper()

	postgresContainer, err := postgres.Run(t.Context(),
		"postgres:latest",
		postgres.WithInitScripts(fetchMigrationFiles(t)...),
		testcontainers.WithWaitStrategy(
			wait.ForExposedPort()),
	)
	if err != nil {
		t.Fatalf("failed to start DB container: %s", err)
	}

	connStr, err := postgresContainer.ConnectionString(t.Context(), "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get container host: %s", err)
	}

	return connStr, func() {
		if err := testcontainers.TerminateContainer(postgresContainer); err != nil {
			t.Errorf("failed to terminate container: %s", err)
		}
	}
}

func NewTestDB(t testing.TB) (*pgxpool.Pool, func()) {
	connStr, containerCloser := NewTestContainer(t)
	pool, err := pgxpool.New(t.Context(), connStr)
	if err != nil {
		t.Fatalf("creating db conn: %v", err)
	}

	return pool, func() {
		defer pool.Close()
		defer containerCloser()
	}
}

func fetchMigrationFiles(t testing.TB) []string {
	t.Helper()
	files, err := filepath.Glob("../db/migrations/*.up.sql")
	if err != nil {
		t.Fatalf("unable to read sql files: %v", err)
	}
	files = append(files, "../db/seed.sql")
	return files
}
