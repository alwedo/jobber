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
		mu          sync.Mutex
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
		for o := range offersCh {
			mu.Lock()
			totalOffers = append(totalOffers, o...)
			mu.Unlock()
		}
	}()

	go func() {
		for e := range errorsCh {
			mu.Lock()
			errs = append(errs, e)
			mu.Unlock()
		}
	}()

	wg.Wait()
	close(offersCh)
	close(errorsCh)

	return totalOffers, combineErrors(errs)
}

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

type mockScraper struct {
	LastQuery *db.Query
	mockErr   error
}

func newMockScraper(opts ...mockScraperOpts) *mockScraper {
	scr := &mockScraper{}
	for _, o := range opts {
		o(scr)
	}
	return scr
}

func (m *mockScraper) Scrape(_ context.Context, q *db.Query) ([]db.CreateOfferParams, error) {
	m.LastQuery = q
	if m.mockErr != nil {
		return nil, m.mockErr
	}
	return []db.CreateOfferParams{
		{Title: q.Keywords + " jobs in " + q.Location},
	}, nil
}

type mockScraperOpts func(*mockScraper)

func mockScraperWithError(err error) mockScraperOpts {
	return func(m *mockScraper) {
		m.mockErr = err
	}
}

var MockScraper = &mockScraper{}
