package linkedin

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
	"github.com/alwedo/jobber/scrape/retryhttp"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	linkedInURL      = "https://www.linkedin.com/jobs-guest/jobs/api/seeMoreJobPostings/search"
	linkedInBaseURL  = "https://www.linkedin.com/jobs/view/" // Direct link to job posting
	linkedInName     = "LinkedIn"
	paramKeywords    = "keywords" // Search keywords, ie. "golang"
	paramLocation    = "location" // Location of the search, ie. "Berlin"
	paramStart       = "start"    // Start of the pagination, in intervals of 10s, ie. "10"
	paramFTPR        = "f_TPR"    // Time Posted Range. Values are in seconds, starting with 'r', ie. r86400 = Past 24 hours
	searchInterval   = 10         // LinkedIn pagination interval
	maxSearchInt     = 1000       // LinkedIn's site returns StatusBadRequest if 'start=1000'
	oneWeekInSeconds = 604800
)

type linkedIn struct {
	client *retryhttp.Client
	logger *slog.Logger
}

func New(l *slog.Logger) *linkedIn { //nolint: revive
	return &linkedIn{client: retryhttp.New(), logger: l}
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
		return nil, fmt.Errorf("failed to parse URL in linkedin.fetchOffersPage: %w", err)
	}
	url.RawQuery = qp.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request in linkedin.fetchOffersPage: %w", err)
	}

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to do http request in linkedin.fetchOffersPage: %w", err)
	}

	return resp.Body, nil
}

// Parse parses the LinkedIn HTML response and returns a list of jobs.
func (l *linkedIn) parseLinkedInBody(body io.ReadCloser) ([]db.CreateOfferParams, error) {
	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML in linkedin.parseLinkedInBody: %w", err)
	}
	body.Close()
	var jobs []db.CreateOfferParams

	// Find all job listings
	doc.Find("li").Each(func(_ int, s *goquery.Selection) {
		// Check if this li contains a job card
		if s.Find(".base-search-card").Length() > 0 {
			job := db.CreateOfferParams{Source: linkedInName}

			// Extract Job ID from data-entity-urn
			if urn, exists := s.Find("[data-entity-urn]").Attr("data-entity-urn"); exists {
				id := strings.Split(urn, ":")
				job.ID = id[len(id)-1]
			}

			// Construct direct link to job posting
			job.Url = linkedInBaseURL + job.ID

			// Extract Title
			job.Title = normalizeText(s.Find(".base-search-card__title").Text())

			// Extract Company
			job.Company = normalizeText(s.Find(".base-search-card__subtitle a").Text())

			// Extract Location
			job.Location = normalizeText(s.Find(".job-search-card__location").Text())

			// Extract Posted Date
			timeSel := s.Find("time")
			postedAt, _ := timeSel.Attr("datetime")
			t, err := normalizeTime(postedAt, normalizeText(timeSel.Text()))
			if err != nil {
				l.logger.Error("unable to normalize time in scrape.LinkedIn", slog.Any("error", err))
			}
			job.PostedAt = pgtype.Timestamptz{Time: t, Valid: true}

			jobs = append(jobs, job)
		}
	})

	return jobs, nil
}

// normalizeText removes newlines and trims whitespaces from a string.
func normalizeText(s string) string {
	str := strings.Split(s, "\n")
	for i, v := range str {
		str[i] = strings.TrimSpace(v)
	}
	return strings.TrimSpace(strings.Join(str, " "))
}

// normalizeTime constructs the most accurate possible time based
// on LinkedIn's obscured machine-readable and human-readable times.
// If the LinkedIn offer was posted hours ago, the time will look like this:
//
//	<time class="job-search-card__listdate" datetime="2025-11-11">
//	2 hours ago
//	</time>
//
// But past a certain point the human readable number will be be shown as:
//
//	2 days ago
//
// If the time is in hours, we'll substract it to the current time.
// Otherwise will add the current hour, min and secs to the parsed time
// to avoid having every old offer look like it was posted at midnight.
//
// Upon errors normalizeTime will return time.Now() and the error.
func normalizeTime(postedAt, rel string) (time.Time, error) {
	var parsedTime time.Time
	var now = time.Now()

	if strings.Contains(rel, "hours") {
		// Split "2 hours ago" to find the number of hours.
		v, err := strconv.Atoi(strings.Split(rel, " ")[0])
		if err != nil {
			return now, fmt.Errorf("unable to parse relative time in normalizeTime: %w", err)
		}
		parsedTime = now.Add(-time.Duration(v) * time.Hour)
	} else {
		t, err := time.ParseInLocation("2006-01-02", postedAt, time.Local)
		if err != nil {
			return now, fmt.Errorf("unable to parse date in normalizeTime: %w", err)
		}
		parsedTime = time.Date(
			t.Year(), t.Month(), t.Day(),
			now.Hour(), now.Minute(), now.Second(), now.Nanosecond(),
			t.Location(),
		)
	}

	return parsedTime.UTC(), nil
}
