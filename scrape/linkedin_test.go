package scrape

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alwedo/jobber/db"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestFetchOffersPage(t *testing.T) {
	mockResp := newLinkedInMockResp(t)
	l := &linkedIn{&http.Client{Transport: mockResp}}
	ctx := context.Background()

	t.Run("first time query", func(t *testing.T) {
		query := &db.Query{
			Keywords: "golang",
			Location: "the moon",
		}
		resp, err := l.fetchOffersPage(ctx, query, 0)
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
		if values.Get(paramFTPR) != fmt.Sprintf("r%d", oneWeekInSeconds) {
			t.Errorf("expected 'f_TPR' in query params to be lastlastWeek, got %s", values.Get(paramFTPR))
		}
		if mockResp.req.URL.Host != "www.linkedin.com" {
			t.Errorf("expected host to be 'www.linkedin.com', got %s", mockResp.req.URL.Host)
		}
		if mockResp.req.URL.Path != "/jobs-guest/jobs/api/seeMoreJobPostings/search" {
			t.Errorf("expected path to be '/jobs-guest/jobs/api/seeMoreJobPostings/search', got %s", mockResp.req.URL.Path)
		}
		file, err := os.Open("test_data/linkedin1.html")
		if err != nil {
			t.Fatalf("failed to open file test_data/linkedin1.html: %s", err.Error())
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
		resp, err := l.fetchOffersPage(ctx, query, 0)
		if err != nil {
			t.Errorf("error fetching offers: %s", err.Error())
		}
		defer resp.Close()
		gotFTPR := mockResp.req.URL.Query().Get(paramFTPR)
		if gotFTPR != "r3600" {
			t.Errorf("expected FT_PR to be 'r3600', got %s", gotFTPR)
		}
	})

	t.Run("retryable cases", func(t *testing.T) {
		t.Run("working exponential backoff", func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				query := &db.Query{
					Keywords: "retry", // retry keyword makes mock to return 429
					Location: "the moon",
				}
				pages := []int{0, 10, 20}
				for _, p := range pages {
					resp, err := l.fetchOffersPage(ctx, query, p)
					if err != nil {
						t.Errorf("expected no error, got: %v", err)
					}
					if resp == nil {
						t.Errorf("expected response body not to be nil")
					}
				}
				synctest.Wait()
			})
		})
		t.Run("exhausted exponential backoff", func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				query := &db.Query{
					// retry-fail keyword makes mock to return 429 all the time after the first call.
					Keywords: "retry-fail",
					Location: "the moon",
				}
				pages := []int{0, 10, 20}
				for _, p := range pages {
					switch p {
					case 0:
						resp, err := l.fetchOffersPage(ctx, query, p)
						if err != nil {
							t.Errorf("expected no error, got: %v", err)
						}
						if resp == nil {
							t.Errorf("expected response body not to be nil")
						}
					default:
						resp, err := l.fetchOffersPage(ctx, query, p)
						if !errors.Is(err, ErrRetryable) {
							t.Errorf("expected err to be ErrRetryable, got: %v", err)
						}
						if resp != nil {
							t.Errorf("expected response body to be nil, got %v", resp)
						}
					}
				}
				synctest.Wait()
			})
		})
	})
}

func TestParseLinkedInBody(t *testing.T) {
	l := &linkedIn{}

	file, err := os.Open("test_data/linkedin1.html")
	if err != nil {
		log.Fatalf("failed to open file: %s", err.Error())
	}
	defer file.Close()

	jobs, err := l.parseLinkedInBody(file)
	if err != nil {
		t.Fatalf("error parsing test_data/linkedin1.html: %s", err.Error())
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

func TestScrape(t *testing.T) {
	mockResp := newLinkedInMockResp(t)
	l := &linkedIn{&http.Client{Transport: mockResp}}

	t.Run("expected behaviour", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			query := &db.Query{Keywords: "golang", Location: "the moon"}
			offers, err := l.Scrape(context.Background(), query)
			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
			synctest.Wait()
			if len(offers) != 27 {
				t.Errorf("expected 27 offers, got %d", len(offers))
			}
		})
	})
	t.Run("too many retries don't discard data", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			query := &db.Query{Keywords: "retry-fail", Location: "the moon"}
			offers, err := l.Scrape(context.Background(), query)
			if !errors.Is(err, ErrRetryable) {
				t.Errorf("expected ErrRetryable, got: %v", err)
			}
			synctest.Wait()
			if len(offers) != 10 {
				t.Errorf("expected 10 offers from the first page, got %d", len(offers))
			}
		})
	})
}

type linkedInMockResp struct {
	t       testing.TB
	req     *http.Request
	lastReq time.Time
}

func (h *linkedInMockResp) RoundTrip(req *http.Request) (*http.Response, error) {
	// Save the last request for further inspection
	h.req = req

	status := http.StatusOK
	// Mock 429. We currently don't know LinkedIn's 429 strategy.
	// We're starting with 1 second delay and increase if it doesn't work.
	if req.URL.Query().Get(paramKeywords) == "retry" {
		if time.Since(h.lastReq) < time.Second {
			status = http.StatusTooManyRequests
		}
	}

	// We want to make sure the outcome when exhausting all retry posibilities.
	// We also want scrapings with pagination to return patrial results when failing.
	// The keyword 'retry-fail' will always return 429 after the first call.
	if req.URL.Query().Get(paramKeywords) == "retry-fail" && req.URL.Query().Get("start") != "" {
		status = http.StatusTooManyRequests
	}

	// Mock LinkedIn pagination strategy
	fn := "test_data/linkedin1.html"
	switch req.URL.Query().Get("start") {
	case "10":
		fn = "test_data/linkedin2.html"
	case "20":
		fn = "test_data/linkedin3.html"
	}

	// Return the html according to pagination
	body, err := os.Open(fn)
	if err != nil {
		h.t.Fatalf("failed to open %s in mockResp.RoundTrip: %s", fn, err)
	}

	// Save last request time for mocking 429
	h.lastReq = time.Now()

	return &http.Response{
		StatusCode: status,
		Body:       body,
	}, nil
}

func newLinkedInMockResp(t testing.TB) *linkedInMockResp {
	return &linkedInMockResp{t: t}
}
