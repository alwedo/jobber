package glassdoor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/scrape/retryhttp"
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
			Offers []offer `json:"jobListings"`
		} `json:"jobListings"`
		PaginationCursors []struct {
			Cursor     string `json:"cursor"`
			PageNumber int    `json:"pageNumber"`
		} `json:"paginationCursors"`
		TotalJobCount int `json:"totalJobCount"`
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
			DescriptionFragmentText []string `json:"descriptionFragmentText"`
			JobTitleText            string   `json:"jobTitleText"`
			ListingID               int      `json:"listingId"`
		} `json:"job"`
	} `json:"jobView"`
}

type body struct {
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
	logger *slog.Logger
	lCache sync.Map
}

func New(l *slog.Logger) *glassdoor { //nolint: revive
	return &glassdoor{
		client: retryhttp.New(),
		logger: l,
		lCache: sync.Map{},
	}
}

func (g *glassdoor) Scrape(ctx context.Context, query *db.GetQueryScraperRow) ([]db.CreateOfferParams, error) {
	return []db.CreateOfferParams{}, nil
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
		return nil, fmt.Errorf("unable to parse url %s in glassdoor.fetchLocation: %v", baseURL+locationEndpoint, err)
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create http request glassdoor.fetchLocation: %v", err)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to perform http request glassdoor.fetchLocation: %v", err)
	}
	defer resp.Body.Close()

	var l = []location{}

	if err := json.NewDecoder(resp.Body).Decode(&l); err != nil {
		return nil, fmt.Errorf("unable to decode http response body in glassdoor.fetchLocation: %v", err)
	}

	result := &l[0]

	actual, loaded := g.lCache.LoadOrStore(loc, result)
	if loaded {
		return actual.(*location), nil
	}

	return result, nil
}
