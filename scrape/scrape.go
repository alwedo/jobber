// Package scrape defines the Scraper interface for extracting job offer data from external sources.
// Implementations accept a query and return structured offer parameters for database insertion.
// Includes a mock implementation for testing.
package scrape

import (
	"context"
	"fmt"
	"time"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/scrape/glassdoor"
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
func New() List {
	return List{
		stepstone.Name: stepstone.New(),
		linkedin.Name:  linkedin.New(),
		glassdoor.Name: glassdoor.New(),
	}
}

var (
	Mock          = &mock{}
	MockWithErr   = &mock{mockErr: fmt.Errorf("error")}
	MockWithDelay = &mock{delay: 150 * time.Millisecond}
	MockList      = List{"Mock": Mock}
)

type mock struct {
	LastQuery *db.GetQueryScraperRow
	mockErr   error
	delay     time.Duration
}

func (m *mock) Scrape(_ context.Context, q *db.GetQueryScraperRow) ([]db.CreateOfferParams, error) {
	m.LastQuery = q
	time.Sleep(m.delay)
	if m.mockErr != nil {
		return nil, m.mockErr
	}
	return []db.CreateOfferParams{
		{Title: q.Keywords + " jobs in " + q.Location},
	}, nil
}
