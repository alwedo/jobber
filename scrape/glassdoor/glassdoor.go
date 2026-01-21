package glassdoor

import (
	"context"
	"log/slog"

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
	paramTerm                     = "term"
)

// When querying the location on the searchEndpoint, glassdoor respond with a
// single letter for locationType but calling searchEndpoint requires a full string.
var locationMap = map[string]string{
	"C": "CITY",
	"S": "STATE",
	"N": "COUNTRY",
}

type locationResponse struct {
	data []struct {
		LocationID   int    `json:"locationId"`
		LocationType string `json:"locationType"`
	}
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
}

func New(l *slog.Logger) *glassdoor { //nolint: revive
	return &glassdoor{client: retryhttp.New(), logger: l}
}

func (g *glassdoor) Scrape(ctx context.Context, query *db.GetQueryScraperRow) ([]db.CreateOfferParams, error) {
	return []db.CreateOfferParams{}, nil
}
