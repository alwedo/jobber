// Package scrape defines the Scraper interface for extracting job offer data from external sources.
// Implementations accept a query and return structured offer parameters for database insertion.
// Includes a mock implementation for testing.
package scrape

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/scrape/linkedin"
	"github.com/alwedo/jobber/scrape/stepstone"
)

// Scraper defines the interface expected from all the scrapers.
type Scraper interface {
	Scrape(context.Context, *db.GetQueryScraperRow) ([]db.CreateOfferParams, error)
}

// List links the name of the scraper to its implementation.
type List map[string]Scraper

// New returns a list of all available scrapers.
func New(l *slog.Logger) List {
	return List{
		linkedin.Name:  linkedin.New(l),
		stepstone.Name: stepstone.New(l),
	}
}

var (
	MockScraper        = &mockScraper{}
	MockScraperWithErr = &mockScraper{mockErr: fmt.Errorf("error")}
	MockScraperList    = List{"Mock": MockScraper}
)

type mockScraper struct {
	LastQuery *db.GetQueryScraperRow
	mockErr   error
}

func (m *mockScraper) Scrape(_ context.Context, q *db.GetQueryScraperRow) ([]db.CreateOfferParams, error) {
	m.LastQuery = q
	if m.mockErr != nil {
		return nil, m.mockErr
	}
	return []db.CreateOfferParams{
		{Title: q.Keywords + " jobs in " + q.Location},
	}, nil
}
