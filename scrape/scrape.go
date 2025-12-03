// Package scrape defines the Scraper interface for extracting job offer data from external sources.
// Implementations accept a query and return structured offer parameters for database insertion.
// Includes a mock implementation for testing.
package scrape

import "github.com/Alvaroalonsobabbel/jobber/db"

type Scraper interface {
	Scrape(*db.Query) ([]db.CreateOfferParams, error)
}

type mockScraper struct {
	LastQuery *db.Query
}

func (m *mockScraper) Scrape(q *db.Query) ([]db.CreateOfferParams, error) {
	m.LastQuery = q
	return []db.CreateOfferParams{}, nil
}

var MockScraper = &mockScraper{}
