package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var seed = `
INSERT INTO queries (keywords, location, queried_at) VALUES
('python', 'san francisco', CURRENT_TIMESTAMP - INTERVAL '8 days'),
('data scientist', 'new york', CURRENT_TIMESTAMP),
('golang', 'berlin', CURRENT_TIMESTAMP),
('retry', 'berlin', CURRENT_TIMESTAMP);
INSERT INTO offers (id, title, company, location, posted_at, description, source, url) VALUES
('offer_001', 'Senior Python Developer', 'TechCorp Inc', 'San Francisco, CA', CURRENT_TIMESTAMP - INTERVAL '8 days', '', 'LinkedIn', ''),
('existing_offer', 'Junior Golang Dweeb', 'Späti GmbH', 'Berlin', CURRENT_TIMESTAMP, '', 'LinkedIn', 'https://www.linkedin.com/jobs/view/existing_offer');
INSERT INTO query_offers (query_id, offer_id) VALUES
(1, 'offer_001'),
(3, 'existing_offer'),
(1, 'existing_offer');
`

func NewTestDB(t testing.TB) (*Queries, func()) {
	t.Helper()
	ctx := context.Background()

	var (
		dbImage          = "postgres:latest"
		dbName           = "jobber"
		dbPort  nat.Port = "5432/tcp"
	)

	postgresContainer, err := postgres.Run(ctx,
		dbImage,
		postgres.WithDatabase(dbName),
		postgres.WithInitScripts(fetchMigrationFiles(t)...),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort(dbPort)),
	)
	if err != nil {
		t.Fatalf("failed to start DB container: %s", err)
	}

	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get container host: %s", err)
	}

	conn, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("unable to initialize db connection: %v", err)
	}

	// Pings the DB with retry mechanism.
	var pingErr error
	for range 5 {
		pingErr = conn.Ping(ctx)
		if pingErr != nil {
			time.Sleep(time.Second)
			continue
		}
	}
	if pingErr != nil {
		t.Fatalf("unable to ping the DB: %v", pingErr)
	}

	_, err = conn.Exec(ctx, seed)
	if err != nil {
		t.Fatalf("unable to seed DB: %v", err)
	}

	return New(conn), func() {
		conn.Close()
		if err := testcontainers.TerminateContainer(postgresContainer); err != nil {
			t.Errorf("failed to terminate container: %s", err)
		}
	}
}

func fetchMigrationFiles(t testing.TB) []string {
	t.Helper()
	files, err := filepath.Glob("../db/migrations/*.up.sql")
	if err != nil {
		t.Fatalf("unable to read sql files: %v", err)
	}
	return files
}
