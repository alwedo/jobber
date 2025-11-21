package jobber

import (
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Alvaroalonsobabbel/jobber/db"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestFetchOffersPage(t *testing.T) {
	mockResp := newLinkedInMockResp(t)
	l := &linkedIn{
		client: &http.Client{Transport: mockResp},
		logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
	}

	t.Run("first time query", func(t *testing.T) {
		query := &db.Query{
			Keywords: "golang",
			Location: "the moon",
		}
		resp, err := l.fetchOffersPage(query, 0)
		if err != nil {
			t.Errorf("error fetching offers: %s", err.Error())
		}
		defer resp.Close()
		values := mockResp.req.URL.Query()
		if values.Get(paramKeywords) != "golang" {
			t.Errorf("expected 'keywords' in query params to be 'golang', got %s", values.Get(paramKeywords))
		}
		if values.Get(paramLocation) != "the moon" {
			t.Errorf("expected 'location' in query params to be 'the moon', got %s", values.Get(paramLocation))
		}
		if values.Get(paramFTPR) != lastWeek {
			t.Errorf("expected 'f_TPR' in query params to be lastlastWeek, got %s", values.Get(paramFTPR))
		}
		if mockResp.req.URL.Host != "www.linkedin.com" {
			t.Errorf("expected host to be 'www.linkedin.com', got %s", mockResp.req.URL.Host)
		}
		if mockResp.req.URL.Path != "/jobs-guest/jobs/api/seeMoreJobPostings/search" {
			t.Errorf("expected path to be '/jobs-guest/jobs/api/seeMoreJobPostings/search', got %s", mockResp.req.URL.Path)
		}
		file, err := os.Open("example1.html")
		if err != nil {
			t.Fatalf("failed to open file: %s", err.Error())
		}
		defer file.Close()
		want, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("failed to read example1.html file: %s", err.Error())
		}
		got, err := io.ReadAll(resp)
		if err != nil {
			t.Errorf("unable to read response body: %v", err)
		}
		if len(want) != len(got) {
			t.Errorf("expected response body length to be %d, got %d", len(want), len(got))
		}
	})

	t.Run("queries with UpdatedAt field should have relative FTPR", func(t *testing.T) {
		query := &db.Query{
			Keywords:  "golang",
			Location:  "the moon",
			UpdatedAt: pgtype.Timestamptz{Valid: true, Time: time.Now().Add(-time.Hour)},
		}
		resp, err := l.fetchOffersPage(query, 0)
		if err != nil {
			t.Errorf("error fetching offers: %s", err.Error())
		}
		defer resp.Close()
		gotFTPR := mockResp.req.URL.Query().Get(paramFTPR)
		if gotFTPR != "r3600" {
			t.Errorf("expected FT_PR to be 'r3600', got %s", gotFTPR)
		}
	})

}

func TestParseLinkedInBody(t *testing.T) {
	l := &linkedIn{logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))}

	file, err := os.Open("example1.html")
	if err != nil {
		log.Fatalf("failed to open file: %s", err.Error())
	}
	defer file.Close()

	jobs, err := l.parseLinkedInBody(file)
	if err != nil {
		t.Fatalf("error parsing example.html: %s", err.Error())
	}
	if len(jobs) != 10 {
		t.Errorf("expected 10 jobs, got %d", len(jobs))
	}
	if jobs[0].ID != "4322119156" {
		t.Errorf("expected job ID 4322119156, got %s", jobs[0].ID)
	}
	if jobs[0].Title != "Software Engineer (Golang)" {
		t.Errorf("expected job title 'Software Engineer (Golang)', got '%s'", jobs[0].Title)
	}
	if jobs[0].Location != "Berlin, Berlin, Germany" {
		t.Errorf("expected job location 'Berlin, Berlin, Germany', got '%s'", jobs[0].Location)
	}
	if jobs[0].Company != "Delivery Hero" {
		t.Errorf("expected job company 'Delivery Hero', got '%s'", jobs[0].Company)
	}
	if jobs[0].PostedAt.Time.Format("2006-01-02") != "2025-11-13" {
		t.Errorf("expected job posted at time %v, got %v", "2025-11-13", jobs[0].PostedAt.Time.Format("2006-01-02"))
	}
}

func TestSearch(t *testing.T) {
	mockResp := newLinkedInMockResp(t)
	l := &linkedIn{
		client: &http.Client{Transport: mockResp},
		logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
	}
	query := &db.Query{
		Keywords: "golang",
		Location: "the moon",
	}
	offers, err := l.search(query)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(offers) != 27 {
		t.Errorf("expected 27 offers, got %d", len(offers))
	}
}

type linkedInMockResp struct {
	t   testing.TB
	req *http.Request
}

func (h *linkedInMockResp) RoundTrip(req *http.Request) (*http.Response, error) {
	h.req = req
	fn := "example1.html"
	switch h.req.URL.Query().Get("start") {
	case "10":
		fn = "example2.html"
	case "20":
		fn = "example3.html"
	}

	body, err := os.Open(fn)
	if err != nil {
		h.t.Fatalf("failed to open %s in mockResp.RoundTrip: %s", fn, err)
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       body,
	}, nil
}

func newLinkedInMockResp(t testing.TB) *linkedInMockResp {
	return &linkedInMockResp{t: t}
}
