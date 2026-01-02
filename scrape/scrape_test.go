package scrape

import (
	"context"
	"errors"
	"testing"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/scrape/retryhttp"
)

func TestScrape(t *testing.T) {
	query := &db.Query{Keywords: "golang", Location: "the moon"}

	t.Run("collect offers from multiple scrapers", func(t *testing.T) {
		t.Parallel()
		scrapers := []*mockScraper{newMockScraper(), newMockScraper(), newMockScraper(), newMockScraper()}
		s := scraper{}
		for _, v := range scrapers {
			s.sources = append(s.sources, v)
		}

		offers, err := s.Scrape(context.Background(), query)
		if err != nil {
			t.Fatalf("Scrape returned an error: %v", err)
		}
		if len(offers) != len(scrapers) {
			// Mock implementation returns one offer per mock.
			t.Errorf("Scrape returned wrong number of offers: got %v, want %v", len(offers), len(scrapers))
		}
		for _, m := range scrapers {
			if m.LastQuery.Keywords != "golang" {
				t.Errorf("MockScraper received wrong Keywords: got %v, want %v", m.LastQuery.Keywords, "golang")
			}
			if m.LastQuery.Location != "the moon" {
				t.Errorf("MockScraper received wrong Location: got %v, want %v", m.LastQuery.Location, "the moon")
			}
		}
	})

	t.Run("collect errors from multiple scrapers", func(t *testing.T) {
		t.Parallel()
		scrapers := []*mockScraper{newMockScraper(), newMockScraper(), newMockScraper(), newMockScraper(mockScraperWithError(retryhttp.ErrRetryable))}
		s := scraper{}
		for _, v := range scrapers {
			s.sources = append(s.sources, v)
		}
		offers, err := s.Scrape(context.Background(), query)
		if err != nil && !errors.Is(err, retryhttp.ErrRetryable) {
			t.Fatalf("Scrape returned an error: %v", err)
		}
		if len(offers) != len(scrapers)-1 {
			// Mock implementation returns one offer per mock unless it the mock has an error.
			t.Errorf("Scrape returned wrong number of offers: got %v, want %v", len(offers), len(scrapers)-1)
		}
	})
}
