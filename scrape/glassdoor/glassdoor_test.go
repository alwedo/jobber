package glassdoor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/scrape/retryhttp"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestScrape(t *testing.T) {
	synctest.Test(t, func(*testing.T) {
		mock := newGlassdoorMock(t)
		g := &glassdoor{
			client: retryhttp.New(
				retryhttp.WithTransport(mock),
				retryhttp.WithRandomUserAgent(),
			),
			lCache: sync.Map{},
		}
		result, err := g.Scrape(context.Background(), &db.GetQueryScraperRow{
			Keywords: "developer",
			Location: "germany",
		})
		if err != nil {
			t.Errorf("scraper failed: %v", err)
		}

		if len(result) != 83 {
			t.Fatalf("wanted 83 offers, got %d", len(result))
		}

		wantFirstResult := db.CreateOfferParams{
			ID:          "1010007206002",
			Title:       "Lead Backend Engineer | PHP Symfony",
			Company:     "Dyflexis",
			Location:    "Köln",
			PostedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
			Description: "Earn up to €7,000 per month based on experience and work hybrid (2 days per week at our offices in Den Haag or Cologne). 25 vacation days + your birthday off.",
			Source:      "Glassdoor",
			Url:         "https://www.glassdoor.de/job-listing/lead-backend-engineer-php-symfony-dyflexis-JV_IC5023222_KO0,33_KE34,42.htm?jl=1010007206002",
		}
		if !reflect.DeepEqual(wantFirstResult, result[0]) {
			t.Errorf("wanted first jobListing to be:\n%v\ngot:\n%v\n", wantFirstResult, result[0])
		}

		wantLastResult := db.CreateOfferParams{
			ID:       "1010007519935",
			Title:    "Senior Cloud Solution Developer (m/w/d)",
			Company:  "Sopra Steria",
			Location: "Deutschland",
			// The last job offer as an `ageInDays` of 1, so  we expect the PostedAt date to be now - 1 day.
			PostedAt:    pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, -1), Valid: true},
			Description: "Wir sind als eine der führenden europäischen Management- und Technologieberatungen ein echter Tech-Player. Wir sehen uns als Vordenker*innen, handeln und denken…",
			Source:      "Glassdoor",
			Url:         "https://www.glassdoor.de/job-listing/senior-cloud-solution-developer-mwd-sopra-steria-JV_KO0,35_KE36,48.htm?jl=1010007519935",
		}
		if !reflect.DeepEqual(wantLastResult, result[len(result)-1]) {
			t.Errorf("wanted last jobListing to be:\n%v\ngot:\n%v\n", wantLastResult, result[len(result)-1])
		}
	})
}

func TestFetchOffers(t *testing.T) {
	mock := newGlassdoorMock(t)
	g := &glassdoor{
		client: retryhttp.New(
			retryhttp.WithTransport(mock),
			retryhttp.WithRandomUserAgent(),
		),
		lCache: sync.Map{},
	}

	query := &db.GetQueryScraperRow{
		Keywords: "developer",
		Location: "germany",
	}

	req, err := g.newRequestBody(context.Background(), query)
	if err != nil {
		t.Fatalf("failed in newReqBody: %v", err)
	}

	pageCursor := "cuak"
	req.PageCursor = pageCursor

	resp, err := g.fetchOffers(context.Background(), req)
	if err != nil {
		t.Fatalf("want no errors on fetchOffers, got %v", err)
	}

	// Assert http values
	if mock.req.Method != http.MethodPost {
		t.Errorf("wanted fetchOffers http call method to be %s, got %s", http.MethodPost, mock.req.Method)
	}

	gotURL := mock.req.URL.Scheme + "://" + mock.req.URL.Host
	if gotURL != baseURL {
		t.Errorf("wanted url %s, got %s", baseURL, gotURL)
	}

	if mock.req.URL.Path != searchEndpoint {
		t.Errorf("wanted fetchOffers http call path to be %s, got %s", searchEndpoint, mock.req.URL.Path)
	}

	gotAccept := mock.req.Header.Get("Accept")
	if gotAccept != "*/*" {
		t.Errorf("wanted Accept header to be '*.*', got %s", gotAccept)
	}

	gotContentType := mock.req.Header.Get("Content-Type")
	if gotContentType != "application/json" {
		t.Errorf("wanted Content-Type header to be 'application/json', got %s", gotContentType)
	}

	if mock.req.Header.Get("User-Agent") == "" {
		t.Error("wanted User-Agent not to be empty")
	}

	// Assert request body default values
	if mock.reqBody.FilterParams[0].FilterKey != "fromAge" {
		t.Errorf("wanted FilterKey to be fromAge, got %s", mock.reqBody.FilterParams[0].FilterKey)
	}
	if mock.reqBody.FilterParams[0].Values != "7" {
		t.Errorf("wanted FilterKey to be 7, got %s", mock.reqBody.FilterParams[0].Values)
	}
	if mock.reqBody.NumJobsToShow != 30 {
		t.Errorf("wanted NumJobsToShow to be 30, got %d", mock.reqBody.NumJobsToShow)
	}
	// Assert request body passed values
	if mock.reqBody.Keyword != query.Keywords {
		t.Errorf("wanted Keywords to be %s, got %s", query.Keywords, mock.reqBody.Keyword)
	}
	if mock.reqBody.LocationID != 2622109 { // this is the LocationID in the test data
		t.Errorf("wanted LocationID to be %d, got %d", 2622109, mock.reqBody.LocationID)
	}
	if mock.reqBody.LocationType != "CITY" { // see locationMap
		t.Errorf("wanted LocationType to be %s, got %s", "CITY", mock.reqBody.LocationType)
	}
	if mock.reqBody.PageCursor != pageCursor {
		t.Errorf("wanted PageCursor to be %s, got %s", pageCursor, mock.reqBody.PageCursor)
	}
	if mock.reqBody.PageNumber != 1 {
		t.Errorf("wanted PageNumber to be 1, got %d", mock.reqBody.PageNumber)
	}

	// Assert response brings test data values
	if len(resp.Data.JobListings.JobListings) != 30 {
		t.Errorf("wanted 30 job listings, got %d", len(resp.Data.JobListings.JobListings))
	}
}

func TestNewRequestBody(t *testing.T) {
	// We only test the fromAge value generation here.
	// All the other elements are indirectly tested in TestFetchOffers.
	mock := newGlassdoorMock(t)
	g := &glassdoor{
		client: retryhttp.New(
			retryhttp.WithTransport(mock),
			retryhttp.WithRandomUserAgent(),
		),
		lCache: sync.Map{},
	}

	tests := []struct {
		name      string
		qt        time.Duration
		wantValue string
	}{
		{
			name:      "less than a day ago",
			qt:        23 * time.Hour,
			wantValue: "1",
		},
		{
			name:      "more than a day ago",
			qt:        25 * time.Hour,
			wantValue: "7",
		},
		{
			name:      "uninitialized timestamp ",
			wantValue: "7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(*testing.T) {
				query := &db.GetQueryScraperRow{
					Keywords: "cuak",
					Location: "squeek",
				}
				if tt.qt != 0 {
					query.ScrapedAt = pgtype.Timestamptz{
						Time:  time.Now().Add(-tt.qt),
						Valid: true,
					}
				}

				req, err := g.newRequestBody(context.Background(), query)
				if err != nil {
					t.Fatalf("failed to newReqBody: %v", err)
				}

				if req.FilterParams[0].Values != tt.wantValue {
					t.Errorf("wanted %s, got %s", tt.wantValue, req.FilterParams[0].Values)
				}
			})
		})
	}
}

func TestFetchLocation(t *testing.T) {
	tests := []struct {
		name         string
		location     string
		gd           func(*glassdoor)
		wantHTTPCall bool
	}{
		{
			name:         "it calls glassdoor with correct params, returns and caches location type and id",
			location:     "berlin",
			wantHTTPCall: true,
		},
		{
			name:     "it doesn't call glassdoor if location is cached",
			location: "berlin",
			gd: func(g *glassdoor) {
				g.lCache.Store("berlin", &location{
					LocationID:   2622109,
					LocationType: "C",
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newGlassdoorMock(t)
			g := &glassdoor{
				client: retryhttp.New(
					retryhttp.WithTransport(mock),
					retryhttp.WithRandomUserAgent(),
				),
				lCache: sync.Map{},
			}
			if tt.gd != nil {
				tt.gd(g)
			}

			resp, err := g.fetchLocation(context.Background(), tt.location)
			if err != nil {
				t.Fatalf("failed in fetchLocationId: %v", err)
			}

			if tt.wantHTTPCall {
				gotURL := mock.req.URL.Scheme + "://" + mock.req.URL.Host
				if gotURL != baseURL {
					t.Errorf("wanted url %s, got %s", baseURL, gotURL)
				}

				if mock.req.URL.Path != locationEndpoint {
					t.Errorf("wanted path %s, got %s", locationEndpoint, mock.req.URL.Path)
				}

				gotTerm := mock.req.URL.Query().Get(paramTerm)
				if tt.location != gotTerm {
					t.Errorf("wanted param Term to eq %s, got %s", tt.location, gotTerm)
				}

				gotLocTypeFilters := mock.req.URL.Query().Get(paramLocationTypeFilters)
				if paramLocationTypeFiltersValue != gotLocTypeFilters {
					t.Errorf("wanted param locationTypeFilters to eq %s, got %s", paramLocationTypeFiltersValue, gotLocTypeFilters)
				}

				gotAccept := mock.req.Header.Get("Accept")
				if gotAccept != "*/*" {
					t.Errorf("wanted Accept header to be '*.*', got %s", gotAccept)
				}

				gotContentType := mock.req.Header.Get("Content-Type")
				if gotContentType != "application/json" {
					t.Errorf("wanted Content-Type header to be 'application/json', got %s", gotContentType)
				}

				if mock.req.Header.Get("User-Agent") == "" {
					t.Error("wanted User-Agent not to be empty")
				}
			}

			if !tt.wantHTTPCall && mock.req != nil {
				t.Errorf("want http call to be nill, got %v", mock.req)
			}

			wantLocID := 2622109
			wantLocType := "C"
			if wantLocID != resp.LocationID {
				t.Errorf("wanted locationId to be %d, got %d", wantLocID, resp.LocationID)
			}
			if wantLocType != resp.LocationType {
				t.Errorf("wanted locationType to be %s, got %s", wantLocType, resp.LocationType)
			}

			// Assess the location was cached.
			v, _ := g.lCache.Load(tt.location)
			cLoc := v.(*location)
			if wantLocID != cLoc.LocationID {
				t.Errorf("wanted cached locationId to be %d, got %d", wantLocID, cLoc.LocationID)
			}
			if wantLocType != cLoc.LocationType {
				t.Errorf("wanted cached locationType to be %s, got %s", wantLocType, cLoc.LocationType)
			}
		})
	}

	t.Run("location 200 with empty array", func(t *testing.T) {
		mock := newGlassdoorMock(t)
		g := &glassdoor{
			client: retryhttp.New(
				retryhttp.WithTransport(mock),
				retryhttp.WithRandomUserAgent(),
			),
			lCache: sync.Map{},
		}

		_, err := g.fetchLocation(context.Background(), "")
		if err == nil {
			t.Error("wanted err, got nil")
		}
		if err.Error() != "location not found" {
			t.Errorf("wanted err to be 'location not found', got %s", err.Error())
		}
	})
}

type glassdoorMock struct {
	t       testing.TB
	req     *http.Request
	reqBody *requestBody
}

func newGlassdoorMock(t testing.TB) *glassdoorMock {
	return &glassdoorMock{
		t:       t,
		reqBody: &requestBody{},
	}
}

func (g *glassdoorMock) RoundTrip(req *http.Request) (*http.Response, error) {
	// Save the last request for further inspection
	g.req = req

	resp := &http.Response{
		StatusCode: http.StatusOK,
	}

	var fn string
	switch req.URL.Path {
	case locationEndpoint:
		if req.URL.Query().Get(paramTerm) == "" {
			fmt.Println(req.Form.Get(paramTerm))
			resp.Body = io.NopCloser(strings.NewReader("[]"))
		} else {
			fn = "test_data/location.json"
		}
	case searchEndpoint:
		defer req.Body.Close()

		// Decode reqBody into mock for further inspection
		if err := json.NewDecoder(req.Body).Decode(g.reqBody); err != nil {
			g.t.Fatalf("unable to decode request body in glassdoorMock: %v", err)
		}

		fn = fmt.Sprintf("test_data/glassdoor%d.json", g.reqBody.PageNumber)
	}

	if fn != "" {
		body, err := os.Open(fn)
		if err != nil {
			g.t.Fatalf("failed to open %s in mockResp.RoundTrip: %s", fn, err)
			return resp, err
		}
		resp.Body = body
	}

	return resp, nil
}
