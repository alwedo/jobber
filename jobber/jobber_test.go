package jobber

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/Alvaroalonsobabbel/jobber/db"
	"github.com/Alvaroalonsobabbel/jobber/scrape"
)

func TestConstructor(t *testing.T) {
	l := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	d, dbCloser := db.NewTestDB(t)
	defer dbCloser()
	j, jCloser := NewConfigurableJobber(l, d, scrape.MockScraper)
	defer jCloser()

	t.Run("constructor schedules existing queries", func(t *testing.T) {
		wantJobs := 3 // Three queries from DB seed.
		gotJobs := len(j.sched.Jobs())

		if wantJobs != gotJobs {
			t.Errorf("wanted %d initially scheduled jobs, got %d", wantJobs, gotJobs)
		}
	})
}

func TestCreateQuery(t *testing.T) {
	l := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	d, dbCloser := db.NewTestDB(t)
	defer dbCloser()
	j, jCloser := NewConfigurableJobber(l, d, scrape.MockScraper)
	defer jCloser()

	t.Run("creates a query", func(t *testing.T) {
		k := "cuak"
		l := "squeek"
		if err := j.CreateQuery(k, l); err != nil {
			t.Fatalf("failed to create query: %s", err)
		}
		q, err := d.GetQuery(context.Background(), &db.GetQueryParams{Keywords: k, Location: l})
		if err != nil {
			t.Errorf("failed to get query: %s", err)
		}
		if q.Keywords != k {
			t.Errorf("expected keywords to be '%s', got %s", k, q.Keywords)
		}
		if q.Location != l {
			t.Errorf("expected location to be '%s', got %s", l, q.Location)
		}
		if len(j.sched.Jobs()) != 4 { // 3 from the seed + the recently created.
			t.Errorf("expected number of jobs to be 4, got %d", len(j.sched.Jobs()))
		}
		time.Sleep(50 * time.Millisecond)
		for _, jb := range j.sched.Jobs() {
			if slices.Contains(jb.Tags(), k+l) {
				lr, _ := jb.LastRun() //nolint: errcheck
				if lr.Before(time.Now().Add(-time.Second)) {
					t.Errorf("expected created query to have been performed immediately, got %v", lr)
				}
			}
		}
	})

	t.Run("on existing query it returns the existing one", func(t *testing.T) {
		if err := j.CreateQuery("golang", "berlin"); err != nil {
			t.Fatalf("failed to create existing query: %s", err)
		}
		q, err := d.ListQueries(context.Background())
		if err != nil {
			t.Fatalf("failed to list queries: %s", err)
		}
		if len(q) != 4 { // 3 from the seed + last test.
			t.Errorf("expected number of queries to be 4, got %d", len(q))
		}
		if len(j.sched.Jobs()) != 4 { // 3 from the seed + last test.
			t.Errorf("expected number of jobs to be 4, got %d", len(j.sched.Jobs()))
		}
	})
}

func TestListOffers(t *testing.T) {
	l := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	d, dbCloser := db.NewTestDB(t)
	defer dbCloser()
	j, jCloser := NewConfigurableJobber(l, d, scrape.MockScraper)
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

func TestRunQuery(t *testing.T) {
	l := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	d, dbCloser := db.NewTestDB(t)
	defer dbCloser()
	mockScraper := scrape.MockScraper
	j, jCloser := NewConfigurableJobber(l, d, mockScraper)
	defer jCloser()

	t.Run("with valid query", func(t *testing.T) {
		q, err := d.GetQuery(context.Background(), &db.GetQueryParams{Keywords: "golang", Location: "berlin"})
		if err != nil {
			t.Errorf("unable to retrieve seed query: %v", err)
		}
		j.runQuery(q.ID)

		t.Run("it calls the scraper", func(t *testing.T) {
			if *mockScraper.LastQuery != *q {
				t.Errorf("wanted ran query to be %v, got %v", q, mockScraper.LastQuery)
			}
		})
		t.Run("it updates the UpdatedAt field used for removing old queries", func(t *testing.T) {
			qq, err := d.GetQuery(context.Background(), &db.GetQueryParams{Keywords: "golang", Location: "berlin"})
			if err != nil {
				t.Errorf("unable to retrieve seed query: %v", err)
			}
			if q.UpdatedAt.Time.After(qq.UpdatedAt.Time) {
				t.Errorf("wanted the query initial UpdatedAt value to be before the new value")
			}
		})
		// TODO: test adding offer and ignoring existing offer
	})

	t.Run("with older than 7 days query deletes the query", func(t *testing.T) {
		q, err := d.GetQuery(context.Background(), &db.GetQueryParams{Keywords: "python", Location: "san francisco"})
		if err != nil {
			t.Errorf("unable to retrieve seed query: %v", err)
		}
		j.runQuery(q.ID)
		_, err = d.GetQuery(context.Background(), &db.GetQueryParams{Keywords: "python", Location: "san francisco"})
		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("query should have been deleted but got: %v", err)
		}
	})
}
