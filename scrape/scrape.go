// Package scrape defines the Scraper interface for extracting job offer data from external sources.
// Implementations accept a query and return structured offer parameters for database insertion.
// Includes a mock implementation for testing.
package scrape

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/scrape/linkedin"
	"github.com/alwedo/jobber/scrape/stepstone"
)

type Scraper interface {
	Scrape(context.Context, *db.Query) ([]db.CreateOfferParams, error)
}

type scraper struct {
	sources []Scraper
}

func New(log *slog.Logger) *scraper { //nolint: revive
	return &scraper{
		sources: []Scraper{
			linkedin.New(log),
			stepstone.New(log),
		},
	}
}

func (s *scraper) Scrape(ctx context.Context, query *db.Query) ([]db.CreateOfferParams, error) {
	var (
		offersCh    = make(chan []db.CreateOfferParams)
		errorsCh    = make(chan error)
		totalOffers []db.CreateOfferParams
		errs        []error
		wg          sync.WaitGroup
	)

	for _, source := range s.sources {
		wg.Go(func() {
			offers, err := source.Scrape(ctx, query)
			if err != nil {
				errorsCh <- err
			}
			offersCh <- offers
		})
	}

	go func() {
		wg.Wait()
		close(offersCh)
		close(errorsCh)
	}()

	for o := range offersCh {
		totalOffers = append(totalOffers, o...)
	}

	for e := range errorsCh {
		errs = append(errs, e)
	}

	return totalOffers, combineErrors(errs)
}

type mockScraper struct {
	LastQuery *db.Query
}

func (m *mockScraper) Scrape(_ context.Context, q *db.Query) ([]db.CreateOfferParams, error) {
	m.LastQuery = q
	return []db.CreateOfferParams{}, nil
}

var MockScraper = &mockScraper{}

func combineErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}

	combinedErr := errs[0]
	for _, err := range errs[1:] {
		combinedErr = fmt.Errorf("%w; %w", combinedErr, err)
	}

	return combinedErr
}
