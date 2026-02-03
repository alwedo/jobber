package glassdoor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/scrape/retryhttp"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	Name = "Glassdoor"

	baseURL                       = "https://www.glassdoor.de"
	locationEndpoint              = "/autocomplete/location"
	searchEndpoint                = "/job-search-next/bff/jobSearchResultsQuery"
	paramLocationTypeFilters      = "locationTypeFilters"
	paramLocationTypeFiltersValue = "CITY,STATE,COUNTRY"
	paramTerm                     = "term" // Term is the location, ie. 'term=berlin'
)

// When querying the location on the searchEndpoint, glassdoor respond with a
// single letter for locationType but calling searchEndpoint requires a full string.
var locationMap = map[string]string{
	"C": "CITY",
	"S": "STATE",
	"N": "COUNTRY",
}

type location struct {
	LocationID   int    `json:"locationId"`
	LocationType string `json:"locationType"`
}

type response struct {
	Data struct {
		JobListings struct {
			JobListings       []offer `json:"jobListings"`
			PaginationCursors []struct {
				Cursor     string `json:"cursor"`
				PageNumber int    `json:"pageNumber"`
			} `json:"paginationCursors"`
		} `json:"jobListings"`
	} `json:"data"`
}

type offer struct {
	JobView struct {
		Header struct {
			AgeInDays              int    `json:"ageInDays"`
			EmployerNameFromSearch string `json:"employerNameFromSearch"`
			LocationName           string `json:"locationName"`
			SEOJobLink             string `json:"seoJobLink"`
		} `json:"header"`
		Job struct {
			DescriptionFragmentsText []string `json:"descriptionFragmentsText"`
			JobTitleText             string   `json:"jobTitleText"`
			ListingID                int      `json:"listingId"`
		} `json:"job"`
	} `json:"jobView"`
}

type requestBody struct {
	FilterParams []struct {
		FilterKey string `json:"filterKey"`
		Values    string `json:"values"`
	} `json:"filterParams"`
	Keyword       string `json:"keyword"`
	LocationID    int    `json:"locationId"`
	LocationType  string `json:"locationType"`
	NumJobsToShow int    `json:"numJobsToShow"`
	PageCursor    string `json:"pageCursor"`
	PageNumber    int    `json:"pageNumber"`
}

type glassdoor struct {
	client *retryhttp.Client
	lCache sync.Map
}

func New() *glassdoor { //nolint: revive
	return &glassdoor{
		client: retryhttp.New(
			retryhttp.WithRandomUserAgent(),

			// Glassdoor cloudflare some times responds
			// with 403 and can work after retrying.
			retryhttp.WithExtraRetryableStatus([]int{
				http.StatusForbidden,
			}),
		),
		lCache: sync.Map{},
	}
}

func (g *glassdoor) Scrape(ctx context.Context, query *db.GetQueryScraperRow) ([]db.CreateOfferParams, error) {
	offers := []db.CreateOfferParams{}

	body, err := g.newRequestBody(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("unable to create newRequestBody in glassdoor.Scrape: %w", err)
	}

scrape:
	for nextPage := 2; ; nextPage++ {
		resp, err := g.fetchOffers(ctx, body)
		if err != nil {
			// If fetchOffers fails we return the accumulated offers so far and the error.
			return offers, fmt.Errorf("failed to fetchOffers in glassdoor.Scrape: %w", err)
		}

		for _, o := range resp.Data.JobListings.JobListings {
			offers = append(offers, db.CreateOfferParams{
				// Glassdoor returns only an ageInDays value for when the offer
				// was published. We use time.Now for our timestamps and substract
				// the amount of days from ageInDays when it's not 0.
				PostedAt: pgtype.Timestamptz{
					Time:  time.Now().AddDate(0, 0, -o.JobView.Header.AgeInDays),
					Valid: true,
				},
				ID:          strconv.Itoa(o.JobView.Job.ListingID),
				Title:       o.JobView.Job.JobTitleText,
				Company:     o.JobView.Header.EmployerNameFromSearch,
				Location:    o.JobView.Header.LocationName,
				Description: strings.Join(o.JobView.Job.DescriptionFragmentsText, " "),
				Source:      Name,
				Url:         o.JobView.Header.SEOJobLink,
			})
		}

		// Check for the next page in paginationCursors
		for _, pagCur := range resp.Data.JobListings.PaginationCursors {
			if pagCur.PageNumber == nextPage {
				body.PageCursor = pagCur.Cursor
				body.PageNumber = pagCur.PageNumber
				continue scrape
			}
		}
		break
	}

	return offers, nil
}

func (g *glassdoor) fetchOffers(ctx context.Context, rb *requestBody) (*response, error) {
	jsonBody, err := json.Marshal(rb)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal body in glassdoor.fetchOffers: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+searchEndpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("unable to create http request in glassdoor.fetchOffers: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to perform http request in glassdor.fetchOffers: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading the response body: %w", err)
		}
		return nil, fmt.Errorf("response code %d, body: %s", resp.StatusCode, string(body))
	}

	var r = &response{}
	if err := json.NewDecoder(resp.Body).Decode(r); err != nil {
		return nil, fmt.Errorf("unable to unmarshal response in glassdoor.fetchOffers: %w", err)
	}

	return r, nil
}

// newRequestBody initializes a request body from a new query.
// - Stores default immutable values (FilterKey, NumJobsToShow, PageNumber)
// - Stores query Keywords
// - Calls for fetchLocation() and resolves the location
// - Calculates the fromAge value filter param
func (g *glassdoor) newRequestBody(ctx context.Context, q *db.GetQueryScraperRow) (*requestBody, error) {
	loc, err := g.fetchLocation(ctx, q.Location)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch location in glassdoor.newRequestBody: %w", err)
	}

	// Glassdoor's fromAge param takes strings for 1, 3 o 7 days.
	// We want 7 unless the scraped time is valid and less than a day old.
	age := "7"
	if q.ScrapedAt.Valid && q.ScrapedAt.Time.After(time.Now().Add(-24*time.Hour)) {
		age = "1"
	}

	return &requestBody{
		FilterParams: []struct {
			FilterKey string `json:"filterKey"`
			Values    string `json:"values"`
		}{
			{FilterKey: "fromAge", Values: age},
		},
		Keyword:       q.Keywords,
		LocationID:    loc.LocationID,
		LocationType:  locationMap[loc.LocationType],
		NumJobsToShow: 30,
		PageNumber:    1,
	}, nil
}

func (g *glassdoor) fetchLocation(ctx context.Context, loc string) (*location, error) {
	// We cache locations to avoid calling glassdoor every time for known ones.
	if v, ok := g.lCache.Load(loc); ok {
		return v.(*location), nil
	}

	params := &url.Values{}
	params.Add(paramLocationTypeFilters, paramLocationTypeFiltersValue)
	params.Add(paramTerm, loc)

	u, err := url.Parse(baseURL + locationEndpoint)
	if err != nil {
		return nil, fmt.Errorf("unable to parse url %s in glassdoor.fetchLocation: %w", baseURL+locationEndpoint, err)
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create http request glassdoor.fetchLocation: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to perform http request glassdoor.fetchLocation: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading the response body: %w", err)
		}
		return nil, fmt.Errorf("response code %d, body: %s", resp.StatusCode, string(body))
	}

	var l = []location{}
	if err := json.NewDecoder(resp.Body).Decode(&l); err != nil {
		return nil, fmt.Errorf("unable to decode http response body in glassdoor.fetchLocation: %w", err)
	}

	// Glassdoor returns a list of location matches for the search term.
	// We pick the first one and store it in the cache.
	result := &l[0]
	actual, loaded := g.lCache.LoadOrStore(loc, result)
	if loaded {
		return actual.(*location), nil
	}

	return result, nil
}
