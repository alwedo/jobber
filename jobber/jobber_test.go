package jobber

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/Alvaroalonsobabbel/jobber/db"
	_ "modernc.org/sqlite"
)

func TestCreateQuery(t *testing.T) {
	d, closer := testDB(t)
	defer closer()
	j := &Jobber{
		db:     d,
		logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
	}

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
	})

	t.Run("on existing query it returns the existing one", func(t *testing.T) {
		q, err := j.CreateQuery("golang", "Berlin")
		if err != nil {
			t.Fatalf("failed to create existing query: %s", err)
		}
		if q.ID != 3 {
			t.Errorf("expected query ID to be 3, got %d", q.ID)
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
	d, closer := testDB(t)
	defer closer()
	j := &Jobber{
		db:     d,
		logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
	}

	t.Run("returns a list of offers", func(t *testing.T) {
		o, err := j.ListOffers("golang", "Berlin")
		if err != nil {
			t.Fatalf("failed to list offers: %s", err)
		}
		if len(o) != 1 {
			t.Fatalf("expected 1 offer, got %d", len(o))
		}
	})

	t.Run("returns sql.ErrNoRows for invalid query", func(t *testing.T) {
		o, err := j.ListOffers("cuak", "squeek")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("expected error to be sql.ErrNoRows, got: %s", err)
		}
		if len(o) != 0 {
			t.Fatalf("expected 0 offers, got %d", len(o))
		}
	})

}

func testDB(t testing.TB) (*db.Queries, func() error) {
	schema, err := os.Open("../schema.sql")
	if err != nil {
		t.Fatalf("unable to open DB schema: %s", err)
	}
	defer schema.Close()
	ddl, err := io.ReadAll(schema)
	if err != nil {
		t.Fatalf("unable to read DB schema: %s", err)
	}
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %s", err)
	}
	if _, err := d.ExecContext(context.Background(), string(ddl)); err != nil {
		t.Fatalf("failed to execute DB schema: %s", err)
	}
	seed := `
INSERT INTO queries (keywords, location) VALUES
('software engineer python', 'San Francisco'),
('data scientist remote', 'New York'),
('golang', 'Berlin');
INSERT INTO offers (id, title, company, location, posted_at) VALUES
('offer_001', 'Senior Python Developer', 'TechCorp Inc', 'San Francisco, CA', '2024-01-15 10:30:00'),
('existing_offer', 'Junior Golang Dweeb', 'Späti GmbH', 'Berlin', '2024-01-15 10:30:00');
INSERT INTO query_offers (query_id, offer_id) VALUES
(1, 'offer_001'),
(3, 'existing_offer'),
(1, 'existing_offer');
`
	if _, err := d.ExecContext(context.Background(), seed); err != nil {
		t.Fatalf("failed to seed database: %s", err)
	}
	return db.New(d), d.Close
}
