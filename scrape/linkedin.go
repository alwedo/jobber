package scrape

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Alvaroalonsobabbel/jobber/db"
	"github.com/PuerkitoBio/goquery"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	linkedInURL      = "https://www.linkedin.com/jobs-guest/jobs/api/seeMoreJobPostings/search"
	paramKeywords    = "keywords" // Search keywords, ie. "golang"
	paramLocation    = "location" // Location of the search, ie. "Berlin"
	paramStart       = "start"    // Start of the pagination, in intervals of 10s, ie. "10"
	paramFTPR        = "f_TPR"    // Time Posted Range. Values are in seconds, starting with 'r', ie. r86400 = Past 24 hours
	searchInterval   = 10         // LinkedIn pagination interval
	oneWeekInSeconds = 604800
)

type linkedIn struct {
	client *http.Client
	logger *slog.Logger
}

func LinkedIn(logger *slog.Logger) *linkedIn { //nolint: revive
	return &linkedIn{
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

// search runs a linkedin search based on a query.
// It will paginate over the search results until it doesn't find any more offers,
// Scrape the data and return a slice of offers ready to be added to the DB.
func (l *linkedIn) Scrape(query *db.Query) ([]db.CreateOfferParams, error) {
	var totalOffers []db.CreateOfferParams
	var offers []db.CreateOfferParams

	for i := 0; i == 0 || len(offers) == searchInterval; i += searchInterval {
		resp, err := l.fetchOffersPage(query, i)
		if err != nil {
			return nil, fmt.Errorf("failed to fetchOffersPage in linkedIn.search: %v", err)
		}
		offers, err = l.parseLinkedInBody(resp)
		if err != nil {
			return nil, fmt.Errorf("failed to parseLinkedInBody body linkedIn.search: %v", err)
		}
		totalOffers = append(totalOffers, offers...)
	}

	return totalOffers, nil
}

// fetchOffersPage gets job offers from LinkedIn based on the passed query params.
// This returns a list of max 10 elements. We move the start by increments of 10.
func (l *linkedIn) fetchOffersPage(query *db.Query, start int) (io.ReadCloser, error) {
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

	// TODO: implement retry mechanism.
	resp, err := l.client.Get(url.String())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received status code: %d", resp.StatusCode)
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
				l.logger.Error("unable to parse datetime for job ID ", job.ID, slog.String("error", err.Error()))
			}
			job.PostedAt = pgtype.Timestamptz{Time: t, Valid: true}

			// Only add if we have essential data
			if job.ID != "" && job.Title != "" {
				jobs = append(jobs, job)
			} else {
				l.logger.Error("Missing essential data for job ID", slog.String("jobID", job.ID))
			}
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
