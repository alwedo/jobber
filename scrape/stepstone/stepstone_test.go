package stepstone

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/scrape/retryhttp"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestScrape(t *testing.T) {
	mockResp := newStepstoneMockResp()
	s := &stepstone{client: retryhttp.NewWithTransport(mockResp)}

	t.Run("http request is correctly formed", func(t *testing.T) {
		query := &db.Query{Keywords: "golang", Location: "the moon"}
		_, err := s.Scrape(context.Background(), query)
		if err != nil {
			t.Fatalf("expected error not to be nil, got %v", err)
		}
		if mockResp.req.Method != http.MethodPost {
			t.Errorf("expected method to be POST, got %s", mockResp.req.Method)
		}
		gotURL := mockResp.req.URL.String()
		if gotURL != stepstoneBaseURL+stepstonePublicAPIEndpoint {
			t.Errorf("expected URL to be %s, got %s", stepstoneBaseURL+stepstonePublicAPIEndpoint, gotURL)
		}
		appJSON := "application/json"
		gotContentType := mockResp.req.Header.Get("Content-Type")
		if gotContentType != appJSON {
			t.Errorf("expected Content-Type to be %s, got %s", appJSON, gotContentType)
		}
		gotAccept := mockResp.req.Header.Get("Accept")
		if gotAccept != appJSON {
			t.Errorf("expected Accept to be %s, got %s", appJSON, gotAccept)
		}
		gotUserAgent := mockResp.req.Header.Get("User-Agent")
		wantUserAgent := "CustomUserAgent/1.0"
		if gotUserAgent != wantUserAgent {
			t.Errorf("expected User-Agent to be %s, got %s", wantUserAgent, gotUserAgent)
		}
	})

	t.Run("first time query returns a week of offers", func(t *testing.T) {
		query := &db.Query{Keywords: "golang", Location: "the moon"}
		offers, err := s.Scrape(context.Background(), query)
		if err != nil {
			t.Fatalf("expected error not to be nil, got %v", err)
		}
		if len(offers) != 70 {
			t.Errorf("expected 70 offers, got %d", len(offers))
		}
		if offers[0].ID != "13112743" {
			t.Errorf("expected first offer ID to be '13112743', got %s", offers[0].ID)
		}
		if offers[len(offers)-1].ID != "12453702" {
			t.Errorf("expected last offer ID to be '12453702', got %s", offers[len(offers)-1].ID)
		}
		gotParamAge := mockResp.searchURL.Query().Get(paramAge)
		if gotParamAge != paramAgeValueAge7 {
			t.Errorf("expected age param to be %s, got %s", paramAgeValueAge7, gotParamAge)
		}
	})

	t.Run("subsequent query returns a day of offers", func(t *testing.T) {
		query := &db.Query{
			Keywords:  "golang",
			Location:  "the moon",
			UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}
		offers, err := s.Scrape(context.Background(), query)
		if err != nil {
			t.Fatalf("expected error not to be nil, got %v", err)
		}
		if len(offers) != 22 {
			t.Errorf("expected 22 offers, got %d", len(offers))
		}
		if offers[0].ID != "13304740" {
			t.Errorf("expected first offer ID to be '13304740', got %s", offers[0].ID)
		}
		if offers[len(offers)-1].ID != "13435478" {
			t.Errorf("expected last offer ID to be '13435478', got %s", offers[len(offers)-1].ID)
		}
		gotParamAge := mockResp.searchURL.Query().Get(paramAge)
		if gotParamAge != paramAgeValueAge1 {
			t.Errorf("expected age param to be %s, got %s", paramAgeValueAge1, gotParamAge)
		}
	})
}

type stepstoneMockResp struct {
	req       *http.Request
	searchURL *url.URL
}

func newStepstoneMockResp() *stepstoneMockResp {
	return &stepstoneMockResp{}
}

func (s *stepstoneMockResp) RoundTrip(req *http.Request) (*http.Response, error) {
	s.req = req

	reqBody := struct {
		URL string `json:"url"`
	}{}
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		return nil, fmt.Errorf("failed to decode request body in stepstoneMockResp: %w", err)
	}
	parsedURL, err := url.Parse(reqBody.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse request body URL in stepstoneMockResp: %w", err)
	}
	s.searchURL = parsedURL

	// Mock stepstone pagination strategy
	fn := fmt.Sprintf(
		"test_data/stepstone_%s_page%s.json",
		parsedURL.Query().Get(paramAge),
		parsedURL.Query().Get(paramPage),
	)
	body, err := os.Open(fn)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s in stepstoneMockResp.RoundTrip: %w", fn, err)
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       body,
	}, nil
}
