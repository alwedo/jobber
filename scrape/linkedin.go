package scrape

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/metrics"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	linkedInURL      = "https://www.linkedin.com/jobs-guest/jobs/api/seeMoreJobPostings/search"
	linkedInName     = "LinkedIn"
	paramKeywords    = "keywords" // Search keywords, ie. "golang"
	paramLocation    = "location" // Location of the search, ie. "Berlin"
	paramStart       = "start"    // Start of the pagination, in intervals of 10s, ie. "10"
	paramFTPR        = "f_TPR"    // Time Posted Range. Values are in seconds, starting with 'r', ie. r86400 = Past 24 hours
	searchInterval   = 10         // LinkedIn pagination interval
	maxSearchInt     = 1000       // LinkedIn's site returns StatusBadRequest if 'start=1000'
	maxRetries       = 5          // Exponential backoff limit.
	oneWeekInSeconds = 604800
)

type linkedIn struct {
	client *http.Client
	logger *slog.Logger
}

func LinkedIn(l *slog.Logger) *linkedIn { //nolint: revive
	return &linkedIn{client: http.DefaultClient, logger: l}
}

// search runs a linkedin search based on a query.
// It will paginate over the search results until it doesn't find any more offers,
// Scrape the data and return a slice of offers ready to be added to the DB.
func (l *linkedIn) Scrape(ctx context.Context, query *db.Query) ([]db.CreateOfferParams, error) {
	t := time.Now()
	var totalOffers []db.CreateOfferParams
	var offers []db.CreateOfferParams

	for i := 0; i < maxSearchInt; i += searchInterval {
		select {
		case <-ctx.Done():
			return totalOffers, fmt.Errorf("linkedIn.Scrape process was canceled: %w", ctx.Err())
		default:
			resp, err := l.fetchOffersPage(ctx, query, i)
			if err != nil {
				// If fetchOffersPage fails we return the accumulated offers so far.
				return totalOffers, fmt.Errorf("failed to fetchOffersPage in linkedIn.Scrape: %w", err)
			}
			offers, err = l.parseLinkedInBody(resp)
			if err != nil {
				return nil, fmt.Errorf("failed to parseLinkedInBody body linkedIn.Scrape: %v", err)
			}
			totalOffers = append(totalOffers, offers...)
		}
		// LinkedIn returns batches of 10 offers. If a batch has 10
		// offers we assume there is a next page, otherwise we stop.
		if len(offers) != searchInterval {
			break
		}
	}
	metrics.ScraperJob.WithLabelValues(
		linkedInName,
		query.Keywords,
		query.Location,
		strconv.Itoa(len(totalOffers)),
	).Observe(time.Since(t).Seconds())

	return totalOffers, nil
}

// fetchOffersPage gets job offers from LinkedIn based on the passed query params.
// This returns a list of max 10 elements. We move the start by increments of 10.
func (l *linkedIn) fetchOffersPage(ctx context.Context, query *db.Query, start int) (io.ReadCloser, error) {
	qp := url.Values{}
	qp.Add(paramKeywords, query.Keywords)
	qp.Add(paramLocation, query.Location)
	if start != 0 {
		qp.Add(paramStart, strconv.Itoa(start))
	}
	ftpr := oneWeekInSeconds

	// UpdatedAt is updated every time we run the query against LinkedIn.
	// If the query has a valid UpdateAt field we don't use the default f_TPR
	// value (a week) but the time difference between the last query and now.
	if query.UpdatedAt.Valid {
		ftpr = int(time.Since(query.UpdatedAt.Time).Seconds())
	}
	qp.Add(paramFTPR, fmt.Sprintf("r%d", ftpr))

	url, err := url.Parse(linkedInURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	url.RawQuery = qp.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Exponential backoff
	var (
		retry   = true
		retries int
		resp    *http.Response
		cErr    error
	)

	defer func() {
		l.logger.Debug(
			"LinkedIn HTTP call",
			slog.Int64("qID", query.ID),
			slog.String("keywords", query.Keywords),
			slog.String("location", query.Location),
			slog.Group("details",
				slog.String("URL", url.String()),
				slog.Any("response code", resp.StatusCode),
				slog.Int("total retries", retries),
			),
		)
	}()

	for retry {
		resp, cErr = l.client.Do(req)
		if cErr != nil {
			return nil, fmt.Errorf("failed to fetch URL: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			if isRetryable[resp.StatusCode] {
				if retries == maxRetries {
					return nil, fmt.Errorf("%w with %w", ErrRetryable, err)
				}
				time.Sleep(time.Duration(retries * int(time.Second)))
				retries++
				continue
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("unable to read response body: %w", err)
			}
			defer resp.Body.Close()
			return nil, fmt.Errorf("received status code: %d, url: %s, message: %s", resp.StatusCode, url.String(), string(body))
		}
		retry = false
	}
	return resp.Body, nil
}

// Parse parses the LinkedIn HTML response and returns a list of jobs.
func (l *linkedIn) parseLinkedInBody(body io.ReadCloser) ([]db.CreateOfferParams, error) {
	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}
	body.Close()
	var jobs []db.CreateOfferParams

	// Find all job listings
	doc.Find("li").Each(func(_ int, s *goquery.Selection) {
		// Check if this li contains a job card
		if s.Find(".base-search-card").Length() > 0 {
			job := db.CreateOfferParams{}

			// Extract Job ID from data-entity-urn
			if urn, exists := s.Find("[data-entity-urn]").Attr("data-entity-urn"); exists {
				id := strings.Split(urn, ":")
				job.ID = id[len(id)-1]
			}

			// Extract Title
			job.Title = normalize(s.Find(".base-search-card__title").Text())

			// Extract Company
			job.Company = normalize(s.Find(".base-search-card__subtitle a").Text())

			// Extract Location
			job.Location = normalize(s.Find(".job-search-card__location").Text())

			// Extract Posted Date
			postedAt, _ := s.Find("time").Attr("datetime")
			t, err := time.Parse("2006-01-02", postedAt)
			if err != nil {
				l.logger.Error("unable to parse 'postedAt' time in scrape.LinkedIn", slog.Any("error", err))
			}
			job.PostedAt = pgtype.Timestamptz{Time: t, Valid: true}

			jobs = append(jobs, job)
		}
	})

	return jobs, nil
}

// normalize removes newlines and trims whitespace from a string.
func normalize(s string) string {
	str := strings.Split(s, "\n")
	for i, v := range str {
		str[i] = strings.TrimSpace(v)
	}
	return strings.TrimSpace(strings.Join(str, " "))
}
