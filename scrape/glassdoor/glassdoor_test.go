package glassdoor

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/alwedo/jobber/scrape/retryhttp"
)

func TestFetchLocation(t *testing.T) {
	mock := &glassdoorMock{t: t}
	g := &glassdoor{retryhttp.NewWithTransport(mock), slog.New(slog.NewJSONHandler(io.Discard, nil))}

	location := "berlin"
	resp, err := g.fetchLocation(context.Background(), location)
	if err != nil {
		t.Fatalf("failed in fetchLocationId: %v", err)
	}

	gotURL := mock.req.URL.Scheme + "://" + mock.req.URL.Host
	if gotURL != baseURL {
		t.Errorf("wanted url %s, got %s", baseURL, gotURL)
	}

	gotTerm := mock.req.URL.Query().Get(paramTerm)
	if location != gotTerm {
		t.Errorf("wanted param Term to eq %s, got %s", location, gotTerm)
	}

	gotLocTypeFilters := mock.req.URL.Query().Get(paramLocationTypeFilters)
	if paramLocationTypeFiltersValue != gotLocTypeFilters {
		t.Errorf("wanted param locationTypeFilters to eq %s, got %s", paramLocationTypeFiltersValue, gotLocTypeFilters)
	}

	wantLocID := 2622109
	wantLocType := "C"
	if wantLocID != resp.LocationID {
		t.Errorf("wanted locationId to be %d, got %d", wantLocID, resp.LocationID)
	}
	if wantLocType != resp.LocationType {
		t.Errorf("wanted locationType to be %s, got %s", wantLocType, resp.LocationType)
	}
}

type glassdoorMock struct {
	t   testing.TB
	req *http.Request
}

func (g *glassdoorMock) RoundTrip(req *http.Request) (*http.Response, error) {
	// Save the last request for further inspection
	g.req = req

	resp := &http.Response{StatusCode: http.StatusOK}

	fn := "test_data/location.json"
	body, err := os.Open(fn)
	if err != nil {
		g.t.Fatalf("failed to open %s in mockResp.RoundTrip: %s", fn, err)
		return resp, err
	}
	resp.Body = body

	return resp, nil
}
