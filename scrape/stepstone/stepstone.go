package stepstone

import (
	"context"
	"log/slog"
	"time"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/scrape/retryhttp"
)

const (
	// stepStoneAPIURL request is done with POST and the requestBody below.
	stepStoneAPIURL = "https://www.stepstone.de/public-api/resultlist/unifiedResultlist"

	// requestBody takes a url value that contains the request parameters.
	// The rest of the hardcoded fields are the minimum required. We pass a random uuid as userHashId.
	requestBody = `{"url": "%s","lang": "en","siteId": 250,"userData": {"userHashId": "%s"},"fields": ["items","pagination"]}`

	// Stepstone takes the keywords and the location as path paramters.
	// ie. "https://www.stepstone.de/work/{keywords}/in-{location}"
	// These params have to be URL encoded.
	stepStoneURL = "https://www.stepstone.de/work/%s/in-%s"
	paramPage    = "page"
	paramSort    = "sort" // sort=2 is by age
	paramAge     = "ag"   // ag=age_1 is one day ago, ag=age_7 is one week ago
)

type responseBody struct {
	Items      []Item `json:"items"`
	Pagination struct {
		Page      int `json:"page"`
		PerPage   int `json:"perPage"`   // perPage is always 25.
		PageCount int `json:"pageCount"` // Total amount of pages for the search.

		// TotalCount is the actual number of relevant offers.
		// It might be that it returns only one page with 25 elements but
		// totalCount is 10. That means the first 10 are relevant offers
		// and the rest are non relevant offers to keep you doomscrolling.
		TotalCount int `json:"totalCount"`
	} `json:"pagination"`
}

type Item struct {
	ID           int       `json:"id"`
	Title        string    `json:"title"`
	URL          string    `json:"url"`
	CompanyName  string    `json:"companyName"`
	DatePosted   time.Time `json:"datePosted"`
	Location     string    `json:"location"`
	WorkFromHome string    `json:"workFromHome"`
	TextSnippet  string    `json:"textSnippet"`
}

type Stepstone struct {
	client *retryhttp.Client
	logger *slog.Logger
}

func New(log *slog.Logger) *Stepstone {
	return &Stepstone{client: retryhttp.New(), logger: log}
}

func (s *Stepstone) Scrape(ctx context.Context, query *db.Query) ([]db.CreateOfferParams, error) {

	return nil, nil
}
