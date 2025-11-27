package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/Alvaroalonsobabbel/jobber/db"
	"github.com/Alvaroalonsobabbel/jobber/jobber"
	"github.com/Alvaroalonsobabbel/jobber/scrape"
	approvals "github.com/approvals/go-approval-tests"
)

func TestServer(t *testing.T) {
	l := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	d, dbCloser := db.NewTestDB(t)
	defer dbCloser()
	j, jCloser := jobber.NewConfigurableJobber(l, d, scrape.MockScraper)
	defer jCloser()
	svr, err := New(l, j)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		path        string
		method      string
		params      map[string]string
		wantStatus  int
		wantHeaders map[string]string
		wantBody    bool
	}{
		{
			name:   "with correct values",
			path:   "/feeds",
			method: http.MethodPost,
			params: map[string]string{
				queryParamKeywords: "golang",
				queryParamLocation: "berlin",
			},
			wantStatus: http.StatusOK,
		},
		{
			name:   "with missing param keywords",
			path:   "/feeds",
			method: http.MethodPost,
			params: map[string]string{
				queryParamLocation: "berlin",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "valid feed",
			path:   "/feeds",
			method: http.MethodGet,
			params: map[string]string{
				queryParamKeywords: "golang",
				queryParamLocation: "berlin",
			},
			wantStatus:  http.StatusOK,
			wantHeaders: map[string]string{"Content-Type": "application/rss+xml"},
			wantBody:    true,
		},
		{
			name:   "invalid feed", // Returns a valid xml with a single post with instructions.
			path:   "/feeds",
			method: http.MethodGet,
			params: map[string]string{
				queryParamKeywords: "fluffy dogs",
				queryParamLocation: "the moon",
			},
			wantStatus:  http.StatusOK,
			wantHeaders: map[string]string{"Content-Type": "application/rss+xml"},
			wantBody:    true,
		},
	}

	client := http.DefaultClient
	server := httptest.NewServer(svr.Handler)
	defer server.Close()

	for _, tt := range tests {
		t.Run(tt.method+tt.path+" "+tt.name, func(t *testing.T) {
			qp := url.Values{}
			for k, v := range tt.params {
				qp.Add(k, v)
			}
			url, err := url.Parse(server.URL + tt.path)
			if err != nil {
				t.Errorf("unable to parse server URL: %v", err)
			}
			url.RawQuery = qp.Encode()
			req, err := http.NewRequest(tt.method, url.String(), nil)
			if err != nil {
				t.Errorf("unable to create http request: %v", err)
			}
			r, err := client.Do(req)
			if err != nil {
				t.Errorf("unable to perform httop request, %v", err)
			}
			defer r.Body.Close()
			if r.StatusCode != tt.wantStatus {
				t.Errorf("wanted status code %d, got %d", tt.wantStatus, r.StatusCode)
			}
			if tt.wantHeaders != nil {
				for k, wantHeader := range tt.wantHeaders {
					gotHeader := r.Header.Get(k)
					if wantHeader != gotHeader {
						t.Errorf("wanted header %s to be %s, got %s", k, wantHeader, gotHeader)
					}
				}
			}
			if tt.wantBody {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("unable to read response body: %v", err)
				}
				scrubber := func(s string) string {
					re := regexp.MustCompile(`<pubDate>[^<]+</pubDate>`)
					return re.ReplaceAllString(s, "<pubDate>DATETIME_SCRUBBED</pubDate>")
				}
				approvals.UseFolder("approvals")
				approvals.VerifyString(t, string(body),
					approvals.Options().ForFile().WithExtension("xml").WithScrubber(scrubber),
				)
			}
		})
	}
}
