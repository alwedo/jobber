package stepstone

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/scrape/retryhttp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	stepstoneName    = "Stepstone"
	stepstoneBaseURL = "https://www.stepstone.de"
	// stepstonePublicAPIEndpoint accepts POST request with the requestBody below.
	stepstonePublicAPIEndpoint = "/public-api/resultlist/unifiedResultlist"

	// requestBody takes a url value that contains the request parameters.
	// The rest of the hardcoded fields are the minimum required. We pass a random uuid as userHashId.
	requestBody = `{"url": "%s","lang": "en","siteId": 250,"userData": {"userHashId": "%s"},"fields": ["items","pagination"]}`

	// Stepstone takes the keywords and the location as path paramters.
	// ie. "https://www.stepstone.de/work/{keywords}/in-{location}"
	// These params have to be URL encoded.
	stepstoneSearchEndpoint = "/work/%s/in-%s"
	paramPage               = "page"
	paramSort               = "sort"
	paramSortValueByAge     = "2" // sort=2 is by age
	paramAge                = "ag"
	paramAgeValueAge1       = "age_1" // ag=age_1 is one day ago
	paramAgeValueAge7       = "age_7" // ag=age_7 is one week ago
)

type response struct {
	Items      []item `json:"items"`
	Pagination struct {
		Page      int `json:"page"`
		PerPage   int `json:"perPage"`   // perPage is always 25. It might not be needed.
		PageCount int `json:"pageCount"` // Total amount of pages for the search.

		// TotalCount is the actual number of relevant offers.
		// It might be that it returns only one page with 25 elements but
		// totalCount is 10. That means the first 10 are relevant offers
		// and the rest are non relevant offers to keep you doomscrolling.
		// Non relevant offers can be older or for different roles.
		TotalCount int `json:"totalCount"`
	} `json:"pagination"`
}

type item struct {
	ID          int                `json:"id"`
	Title       string             `json:"title"`
	URL         string             `json:"url"`
	CompanyName string             `json:"companyName"`
	Location    string             `json:"location"`
	TextSnippet string             `json:"textSnippet"`
	DatePosted  pgtype.Timestamptz `json:"datePosted"`
}

type stepstone struct {
	client *retryhttp.Client
	logger *slog.Logger
}

func New(log *slog.Logger) *stepstone { //nolint: revive
	return &stepstone{client: retryhttp.New(), logger: log}
}

func (s *stepstone) Scrape(ctx context.Context, query *db.Query) ([]db.CreateOfferParams, error) {
	var totalOffers []db.CreateOfferParams
	var totalCount int
	var resp *response
	var err error

	for i := 1; ; i++ {
		resp, err = s.fetchOffers(ctx, query, i)
		if err != nil {
			// If fetchOffers fails we return the accumulated offers so far and the error.
			err = fmt.Errorf("failed to fetchOffers in stepstone.Scrape: %w", err)
			break
		}
		if totalCount == 0 {
			totalCount = resp.Pagination.TotalCount
		}
		for _, v := range resp.Items {
			totalOffers = append(totalOffers, db.CreateOfferParams{
				ID:          strconv.Itoa(v.ID),
				Title:       v.Title,
				Company:     v.CompanyName,
				Location:    v.Location,
				PostedAt:    v.DatePosted,
				Description: v.TextSnippet,
				Source:      stepstoneName,
				Url:         stepstoneBaseURL + v.URL,
			})
		}
		if resp.Pagination.PageCount == i {
			break
		}
	}

	if totalCount > len(totalOffers) {
		// This will prevent panic in case pagination fails before fetching
		// all the offers or an amount that's bigger than totalCount.
		return totalOffers, err
	}
	// Return only valid offers. See response.Pagination.TotalCount.
	return totalOffers[:totalCount], err
}

func (s *stepstone) fetchOffers(ctx context.Context, query *db.Query, page int) (*response, error) {
	// Stepstone expect the param page to be greather than 0.
	if page < 1 {
		return nil, fmt.Errorf("page must be greater than 0 in stepstone.fetchOffers")
	}

	// We use url.QueryEscape for the path values since we need spaces to be replaced with '+'.
	// Leaving them as is and using url.Parse will replace them with '%20' and stepstone won't get proper results.
	parsedURL, err := url.Parse(fmt.Sprintf(
		stepstoneBaseURL+stepstoneSearchEndpoint,
		url.QueryEscape(query.Keywords),
		url.QueryEscape(query.Location),
	))
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL in stepstone.fetchOffers: %w", err)
	}

	// Add query params.
	qp := &url.Values{}
	qp.Add(paramSort, paramSortValueByAge)
	qp.Add(paramPage, strconv.Itoa(page))
	age := paramAgeValueAge7
	// UpdatedAt is updated every time we run the query against Stepstone.
	// Since Stepstone only accepts either 1 or 7 days in the past, we check
	// if the last query was less than one day ago.
	if query.UpdatedAt.Valid && time.Since(query.UpdatedAt.Time) < 24*time.Hour {
		age = paramAgeValueAge1
	}
	qp.Add(paramAge, age)
	parsedURL.RawQuery = qp.Encode()

	body := strings.NewReader(fmt.Sprintf(requestBody, parsedURL.String(), uuid.New().String()))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, stepstoneBaseURL+stepstonePublicAPIEndpoint, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request in stepstone.fetchOffers: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	// Go default user agent breaks stepstone implementation.
	// We need to pass a custom one.
	req.Header.Set("User-Agent", "CustomUserAgent/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to do http request in stepstone.fetchOffers: %w", err)
	}
	defer resp.Body.Close()

	r := &response{}
	if err := json.NewDecoder(resp.Body).Decode(r); err != nil {
		return nil, fmt.Errorf("failed to decode response body in stepstone.fetchOffers: %w", err)
	}

	return r, nil
}
