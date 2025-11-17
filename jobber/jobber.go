// Package jobber retrieves job offers from linedin based on query
// parameters and store the queries and the job offers on the database.
package jobber

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
)

const (
	paramKeywords = "keywords" // Search keywords, ie. "golang"
	paramLocation = "location" // Location of the search, ie. "Berlin"
	paramStart    = "start"    // Start of the pagination, in intervals of 10s, ie. "10"

	/*	Time Posted Range, ie.
		- r86400` = Past 24 hours
		- `r604800` = Past week
		- `r2592000` = Past month
		- `rALL` = Any time
	*/
	paramFTPR      = "f_TPR"
	lastWeek       = "r604800" // Past week
	searchInterval = 10        // LinkedIn pagination interval
	linkedInURL    = "https://www.linkedin.com/jobs-guest/jobs/api/seeMoreJobPostings/search"
)

type Jobber struct {
	client *http.Client
	logger *slog.Logger
	db     *db.Queries
}

func New(log *slog.Logger, db *db.Queries) *Jobber {
	return &Jobber{
		client: &http.Client{Timeout: 10 * time.Second},
		logger: log,
		db:     db,
	}
}

func (j *Jobber) PerformQuery(query *db.Query) ([]db.CreateOfferParams, error) {
	var totalOffers []db.CreateOfferParams
	var offers []db.CreateOfferParams

	for i := 0; i == 0 || len(offers) == searchInterval; i += searchInterval {
		resp, err := j.fetchOffers(query, i)
		if err != nil {
			return nil, fmt.Errorf("failed to fetchOffers in PerformQuery: %v", err)
		}
		offers, err = j.parseLinkedinBody(resp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse LinkedIn body PerformQuery: %v", err)
		}
		totalOffers = append(totalOffers, offers...)
	}

	return totalOffers, nil
}

// fetchOffers gets job offers from LinkedIn based on the passed query params.
func (j *Jobber) fetchOffers(query *db.Query, start int) (io.ReadCloser, error) {
	qp := url.Values{}
	qp.Add(paramKeywords, query.Keywords)
	qp.Add(paramFTPR, lastWeek)
	if query.Location != "" {
		qp.Add(paramLocation, query.Location)
	}
	if start != 0 {
		qp.Add(paramStart, strconv.Itoa(start))
	}

	url, err := url.Parse(linkedInURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}
	url.RawQuery = qp.Encode()

	resp, err := j.client.Get(url.String())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received status code: %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// Parse parses an HTML document and returns a list of jobs.
// This is specifically tied to LinkedIn job search page.
func (j *Jobber) parseLinkedinBody(body io.ReadCloser) ([]db.CreateOfferParams, error) {
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
				j.logger.Error("unable to parse datetime for job ID ", job.ID, slog.String("error", err.Error()))
			}
			job.PostedAt = t

			// Only add if we have essential data
			if job.ID != "" && job.Title != "" {
				jobs = append(jobs, job)
			} else {
				j.logger.Error("Missing essential data for job ID", slog.String("jobID", job.ID))
			}
		}
	})

	return jobs, nil
}

func normalize(s string) string {
	str := strings.Split(s, "\n")
	for i, v := range str {
		str[i] = strings.TrimSpace(v)
	}
	return strings.TrimSpace(strings.Join(str, " "))
}
