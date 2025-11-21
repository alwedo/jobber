package jobber

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Alvaroalonsobabbel/jobber/db"
	"github.com/docker/go-connections/nat"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

type mockScraper struct{}

func (m *mockScraper) scrape(*db.Query) ([]db.CreateOfferParams, error) {
	return []db.CreateOfferParams{}, nil
}

func TestCreateQuery(t *testing.T) {
	l := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	d, dbCloser := testDB(t)
	defer dbCloser()
	j, jCloser := newConfigurableJobber(l, d, &mockScraper{})
	defer jCloser()

	t.Run("creates a query", func(t *testing.T) {
		q, err := j.CreateQuery("cuak", "squeek")
		if err != nil {
			t.Fatalf("failed to create query: %s", err)
		}
		if q.Keywords != "cuak" {
			t.Errorf("expected keywords to be 'cuak', got %s", q.Keywords)
		}
		if q.Location != "squeek" {
			t.Errorf("expected location to be 'squeek', got %s", q.Location)
		}
		if len(j.sched.Jobs()) != 4 { // 3 from the seed + the recently created.
			t.Errorf("expected number of jobs to be 4, got %d", len(j.sched.Jobs()))
		}
		time.Sleep(50 * time.Millisecond)
		for _, jb := range j.sched.Jobs() {
			if slices.Contains(jb.Tags(), q.Keywords+q.Location) {
				lr, _ := jb.LastRun()
				if lr.Before(time.Now().Add(-time.Second)) {
					t.Errorf("expected created query to have been performed immediately, got %v", lr)
				}
			}
		}
	})

	t.Run("on existing query it returns the existing one", func(t *testing.T) {
		q, err := j.CreateQuery("golang", "Berlin")
		if err != nil {
			t.Fatalf("failed to create existing query: %s", err)
		}
		if q.ID != 5 {
			t.Errorf("expected query ID to be 5, got %d", q.ID)
		}
		if q.Keywords != "golang" {
			t.Errorf("expected keywords to be 'golang', got %s", q.Keywords)
		}
		if q.Location != "Berlin" {
			t.Errorf("expected location to be 'Berlin', got %s", q.Location)
		}
	})
}

func TestListOffers(t *testing.T) {
	l := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	d, dbCloser := testDB(t)
	defer dbCloser()
	j, jCloser := newConfigurableJobber(l, d, &mockScraper{})
	defer jCloser()

	tests := []struct {
		name       string
		keywords   string
		location   string
		wantOffers int
		wantErr    error
	}{
		{
			name:       "valid query with offers",
			keywords:   "golang",
			location:   "berlin",
			wantOffers: 1,
			wantErr:    nil,
		},
		{
			name:       "valid query with older than 7 days offers",
			keywords:   "python",
			location:   "san francisco",
			wantOffers: 1, // query has two offers, one is older than 7 days.
		},
		{
			name:     "invalid query with no offers",
			keywords: "cuak",
			location: "squeek",
			wantErr:  sql.ErrNoRows,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := j.ListOffers(tt.keywords, tt.location)
			switch {
			case err == nil:
				if len(o) != tt.wantOffers {
					t.Errorf("expected %d offers, got %d", tt.wantOffers, len(o))
				}
			case errors.Is(err, tt.wantErr):
				// expected error
			default:
				t.Errorf("unexpected error: %s", err)
			}
		})
	}
}

var seed = `
INSERT INTO queries (keywords, location) VALUES
('python', 'san francisco'),
('data scientist', 'new york'),
('golang', 'berlin');
INSERT INTO offers (id, title, company, location, posted_at) VALUES
('offer_001', 'Senior Python Developer', 'TechCorp Inc', 'San Francisco, CA', CURRENT_TIMESTAMP - INTERVAL '8 days'),
('existing_offer', 'Junior Golang Dweeb', 'Späti GmbH', 'Berlin', CURRENT_TIMESTAMP);
INSERT INTO query_offers (query_id, offer_id) VALUES
(1, 'offer_001'),
(3, 'existing_offer'),
(1, 'existing_offer');
`

func testDB(t testing.TB) (*db.Queries, func()) {
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

	if err := conn.Ping(ctx); err != nil {
		t.Fatalf("unable to ping the DB: %v", err)
	}

	_, err = conn.Exec(ctx, seed)
	if err != nil {
		t.Fatalf("unable to seed DB: %v", err)
	}

	return db.New(conn), func() {
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
