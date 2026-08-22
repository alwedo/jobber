package jobber

import (
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/scrape"
)

func TestConstructor(t *testing.T) {
	t.Parallel()
	l := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	pool, closer := db.NewTestDB(t)
	defer closer()
	d := db.New(pool)
	j, jCloser := New(t.Context(), l, d, WithScrapeList(scrape.MockList))
	defer jCloser()

	// Give the scheduler time to process initial jobs.
	time.Sleep(100 * time.Millisecond)

	t.Run("constructor schedules existing queries", func(t *testing.T) {
		wantJobs := 5 // Four queries from DB seed + old offers deletetion.
		gotJobs := len(j.sched.Jobs())

		if wantJobs != gotJobs {
			t.Errorf("wanted %d initially scheduled jobs, got %d", wantJobs, gotJobs)
		}
	})

	t.Run("old offers should've been deleted", func(t *testing.T) {
		offers, err := d.ListOffers(t.Context(), 1)
		if err != nil {
			t.Errorf("wanted no error, got: %v", err)
		}
		if len(offers) != 1 { // query id 1 has 2 jobs in the seed, one is 8 days old.
			t.Errorf("wanted 1, got %d", len(offers))
		}
	})
}

func TestCreateQuery(t *testing.T) {
	t.Parallel()
	l := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	pool, closer := db.NewTestDB(t)
	defer closer()
	d := db.New(pool)
	j, jCloser := New(t.Context(), l, d, WithScrapeList(scrape.List{
		scrape.Mock,
		scrape.Mock2,
	}))
	defer jCloser()

	t.Run("creates a query", func(t *testing.T) {
		k := "cuak"
		l := "squeek"
		if err := j.CreateQuery(t.Context(), k, l); err != nil {
			t.Fatalf("failed to create query: %s", err)
		}
		q, err := d.GetAndUpdateQuery(t.Context(), &db.GetAndUpdateQueryParams{Keywords: k, Location: l})
		if err != nil {
			t.Errorf("failed to get query: %s", err)
		}
		if q.Keywords != k {
			t.Errorf("expected keywords to be '%s', got %s", k, q.Keywords)
		}
		if q.Location != l {
			t.Errorf("expected location to be '%s', got %s", l, q.Location)
		}
		gotJobs := len(j.sched.Jobs())
		wantJobs := 11 // Four queries from DB seed x 2 + recently created x 2 + old offers deletetion.
		if wantJobs != gotJobs {
			t.Errorf("wanted %d jobs, got %d", wantJobs, gotJobs)
		}
		time.Sleep(50 * time.Millisecond)
		for _, jb := range j.sched.Jobs() {
			if slices.Contains(jb.Tags(), k+l) {
				lr, _ := jb.LastRunStartedAt() //nolint: errcheck
				if lr.Before(time.Now().Add(-time.Second)) {
					t.Errorf("expected created query to have been performed immediately, got %v", lr)
				}
			}
		}
	})

	t.Run("on existing query it returns the existing one", func(t *testing.T) {
		if err := j.CreateQuery(t.Context(), "golang", "berlin"); err != nil {
			t.Fatalf("failed to create existing query: %s", err)
		}
		q, err := d.ListQueries(t.Context())
		if err != nil {
			t.Fatalf("failed to list queries: %s", err)
		}
		if len(q) != 5 { // 4 from the seed + last test.
			t.Errorf("expected number of queries to be 5, got %d", len(q))
		}
		wantJobs := 11 // 4 from the seed x2  + last test x2 + old offers deletetion.
		gotJobs := len(j.sched.Jobs())
		if wantJobs != gotJobs {
			t.Errorf("want %d jobs, got %d", wantJobs, gotJobs)
		}
	})
}

func TestCreateWithTimeOut(t *testing.T) {
	t.Parallel()
	l := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	pool, closer := db.NewTestDB(t)
	defer closer()
	d := db.New(pool)
	sl := scrape.List{
		scrape.MockWithDelay,
		scrape.Mock,
		scrape.MockWithErr,
	}
	j, jCloser := New(t.Context(), l, d, WithScrapeList(sl), WithTimeOut(time.Nanosecond))
	defer jCloser()
	cqErr := j.CreateQuery(t.Context(), "cuak", "squeek")
	if !errors.Is(cqErr, ErrTimedOut) {
		t.Errorf("wanted err to be ErrTimedOut, got: %v", cqErr)
	}

	// Ensure new tasks were run immediately by checking if they
	// were performed within the last second.
	time.Sleep(200 * time.Millisecond)
	for _, jb := range j.sched.Jobs() {
		if slices.Contains(jb.Tags(), "cuaksqueek") {
			lr, _ := jb.LastRunStartedAt() //nolint: errcheck
			if lr.Before(time.Now().Add(-time.Second)) {
				t.Errorf("expected created query to have been performed immediately, got %v", lr)
			}
		}
	}
}

func TestListOffers(t *testing.T) {
	t.Parallel()
	l := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	pool, closer := db.NewTestDB(t)
	defer closer()
	d := db.New(pool)
	j, jCloser := New(t.Context(), l, d, WithScrapeList(scrape.MockList))
	defer jCloser()

	// Give the scheduler time to process initial jobs.
	time.Sleep(100 * time.Millisecond)

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
			wantOffers: 2,
			wantErr:    nil,
		},
		{
			name:     "valid query with older than 7 days offers",
			keywords: "python",
			location: "san francisco",
			// Query has two offers in the DB seed. One is older than 7 days and should've be deleted.
			wantOffers: 1,
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
			o, _, err := j.ListOffers(t.Context(), &db.GetAndUpdateQueryParams{
				Keywords: tt.keywords,
				Location: tt.location,
			})
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
	t.Parallel()
	l := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	pool, closer := db.NewTestDB(t)
	defer closer()
	d := db.New(pool)
	s := scrape.Mock
	j, jCloser := New(t.Context(), l, d, WithScrapeList(scrape.List{s}))
	defer jCloser()

	t.Run("with valid query", func(t *testing.T) {
		qID := int64(3) // ID 3 is golang-berlin
		q, err := d.GetQueryScraper(t.Context(), &db.GetQueryScraperParams{ID: qID, ScraperName: s.Name()})
		if err != nil {
			t.Errorf("unable to retrieve seed query: %v", err)
		}
		j.runQuery(t.Context(), q.ID, s)

		t.Run("it calls the scraper", func(t *testing.T) {
			if *s.LastQuery != *q {
				t.Errorf("wanted ran query to be %v, got %v", q, s.LastQuery)
			}
		})
		t.Run("it updates the UpdatedAt field used for removing old queries", func(t *testing.T) {
			qq, err := d.GetAndUpdateQuery(t.Context(), &db.GetAndUpdateQueryParams{Keywords: "golang", Location: "berlin"})
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
		row := pool.QueryRow(t.Context(), `SELECT id FROM queries WHERE keywords='python' AND location='san francisco';`)
		var sq db.Query
		if err := row.Scan(&sq.ID); err != nil {
			t.Errorf("scanning rows %v", err)
		}

		j.runQuery(t.Context(), sq.ID, s)
		q, err := d.GetAndUpdateQuery(t.Context(), &db.GetAndUpdateQueryParams{Keywords: "python", Location: "san francisco"})
		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("expected sql.ErrNoRows after deletion, got: %v (q=%v)", err, q)
		}
	})
}
