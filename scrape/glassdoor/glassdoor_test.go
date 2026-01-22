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
	resp, err := g.fetchLocation(context.Background(), "berlin")
	if err != nil {
		t.Fatalf("failed in fetchLocationId: %v", err)
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
