package glassdoor

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"testing"

	"github.com/alwedo/jobber/scrape/retryhttp"
)

func TestFetchLocation(t *testing.T) {
	tests := []struct {
		name         string
		gd           func(*glassdoor)
		wantHTTPCall bool
	}{
		{
			name:         "it calls glassdoor with correct params, returns and caches location type and id",
			wantHTTPCall: true,
		},
		{
			name: "it doesn't call glassdoor if location is cached",
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
			mock := &glassdoorMock{t: t}
			g := &glassdoor{
				client: retryhttp.NewWithTransport(mock),
				logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
				lCache: sync.Map{},
			}
			if tt.gd != nil {
				tt.gd(g)
			}

			loc := "berlin"
			resp, err := g.fetchLocation(context.Background(), loc)
			if err != nil {
				t.Fatalf("failed in fetchLocationId: %v", err)
			}

			if tt.wantHTTPCall {
				gotURL := mock.req.URL.Scheme + "://" + mock.req.URL.Host
				if gotURL != baseURL {
					t.Errorf("wanted url %s, got %s", baseURL, gotURL)
				}

				gotTerm := mock.req.URL.Query().Get(paramTerm)
				if loc != gotTerm {
					t.Errorf("wanted param Term to eq %s, got %s", loc, gotTerm)
				}

				gotLocTypeFilters := mock.req.URL.Query().Get(paramLocationTypeFilters)
				if paramLocationTypeFiltersValue != gotLocTypeFilters {
					t.Errorf("wanted param locationTypeFilters to eq %s, got %s", paramLocationTypeFiltersValue, gotLocTypeFilters)
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
			v, _ := g.lCache.Load(loc)
			cLoc := v.(*location)
			if wantLocID != cLoc.LocationID {
				t.Errorf("wanted cached locationId to be %d, got %d", wantLocID, cLoc.LocationID)
			}
			if wantLocType != cLoc.LocationType {
				t.Errorf("wanted cached locationType to be %s, got %s", wantLocType, cLoc.LocationType)
			}
		})
	}
}

type glassdoorMock struct {
	t   testing.TB
	req *http.Request
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
		fn = "test_data/location.json"
	case searchEndpoint:
		// fn =
	}

	body, err := os.Open(fn)
	if err != nil {
		g.t.Fatalf("failed to open %s in mockResp.RoundTrip: %s", fn, err)
		return resp, err
	}
	resp.Body = body

	return resp, nil
}
