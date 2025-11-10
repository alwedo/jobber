// Package jobber retrieves job offers from linedin based on query
// parameters and store the queries and the job offers on the database.
package jobber

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

	"github.com/Alvaroalonsobabbel/jobber/db"
	"github.com/PuerkitoBio/goquery"
)

const (
	// Time Posted Range (f_TPR) values
	OneDayAgo    = "r86400"
	ThreeDaysAgo = "r259200"
	FiveDaysAgo  = "r432000"

	ParamKeywords = "keywords" // Search keywords, ie. "golang"
	ParamLocation = "location" // Location of the search, ie. "Berlin"

	/*	Time Posted Range, ie.
		- r86400` = Past 24 hours
		- `r604800` = Past week
		- `r2592000` = Past month
		- `rALL` = Any time
	*/
	ParamFTPR = "f_TPR"

	/*Job Type, ie.
	- F` = Full-time
	- `P` = Part-time
	- `C` = Contract
	- `T` = Temporary
	- `I` = Internship
	- `V` = Volunteer
	- `O` = Other

	Currently only the option for P is implemented.
	By default F will be used.
	*/
	ParamFJT = "f_JT"
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

func (j *Jobber) ListQueries() []*db.Query {
	qs, err := j.db.ListQueries(context.Background())
	if err != nil {
		j.logger.Error("ListQueries in jobber.ListQueries", slog.String("error", err.Error()))
	}
	return qs
}

func (j *Jobber) NewQuery(q *db.CreateQueryParams) *db.Query {
	query, err := j.db.CreateQuery(context.Background(), q)
	if err != nil {
		j.logger.Error("CreateQuery in jobber.NewQuery", slog.String("error", err.Error()))
	}
	return query
}

// RunQuery runs the provided query against linkedin, parses the response,
// adds the resultant offers to the db and returns the updated list of offers.
func (j *Jobber) RunQuery(query *db.Query) []*db.Offer {
	ctx := context.Background()

	// Fetch offers
	resp, err := j.fetchOffers(query)
	if err != nil {
		j.logger.Error("fetchOffers in jobber.RunQuery", slog.String("error", err.Error()))
		return []*db.Offer{}
	}
	defer resp.Close()

	// Parse results
	newOffers, err := j.parseLinkedinBody(resp, query.ID)
	if err != nil {
		j.logger.Error("parse in RunQuery", slog.String("error", err.Error()))
		return []*db.Offer{}
	}

	// Add offers to DB.
	// Existing offers in the DB should not be added due to the UNIQUE ID constrain.
	for _, o := range newOffers {
		if err := j.db.CreateOffer(ctx, &o); err != nil {
			// We log an error creating an offer in DB as warning since
			// we do want exiting offers in the DB not to be created again.
			// TODO: disassociate this error and log a warning, but an error for the rest.w
			j.logger.Warn("CreateOffer in jobber.RunQuery", slog.String("error", err.Error()))
		}
	}

	// List all offers to return.
	offers, err := j.db.ListOffers(ctx, query.ID)
	if err != nil {
		j.logger.Error("ListOffers in jobber.RunQuery", slog.String("error", err.Error()))
		return []*db.Offer{}
	}
	return offers
}

// fetchOffers gets job offers from LinkedIn based on the passed query params.
func (j *Jobber) fetchOffers(query *db.Query) (io.ReadCloser, error) {
	queryParams := url.Values{}
	queryParams.Add(ParamKeywords, query.Keywords)
	if query.Location != "" {
		queryParams.Add(ParamLocation, query.Location)
	}
	if query.FTpr != "" {
		queryParams.Add(ParamFTPR, query.FTpr)
	}
	if query.FJt != "" {
		queryParams.Add(ParamFJT, query.FJt)
	}

	url, err := url.Parse("https://www.linkedin.com/jobs/search")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}
	url.RawQuery = queryParams.Encode()

	resp, err := j.client.Get(url.String())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("received status code: %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// Parse parses an HTML document and returns a list of jobs.
// This is specifically tied to LinkedIn job search page.
func (j *Jobber) parseLinkedinBody(body io.ReadCloser, queryID int64) ([]db.CreateOfferParams, error) {
	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}
	var jobs []db.CreateOfferParams

	// Counts total jobs listed in the title.
	totalJobs, err := strconv.Atoi(strings.Split(doc.Find("title").Text(), " ")[0])
	if err != nil {
		j.logger.Error("strconv.Atoi in parse", slog.String("error", err.Error()))
	}
	if totalJobs > 60 {
		// Currently I don't know how to paginate on linkedin and if
		// there are mote than 60 jobs we log a warning.
		// TODO: investigate how to paginate multiple jobs.
		j.logger.Error("More than 60 jobs found for this search", slog.Int("total jobs found", totalJobs))
	}

	// Find all job listings
	doc.Find("li").Each(func(_ int, s *goquery.Selection) {
		// Check if this li contains a job card
		if s.Find(".base-search-card").Length() > 0 {
			job := db.CreateOfferParams{QueryID: queryID}

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
