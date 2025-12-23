package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/jobber"
	"github.com/alwedo/jobber/scrape"
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
		name           string
		path           string
		method         string
		params         map[string]string
		headers        map[string]string
		wantStatus     int
		wantHeaders    map[string]string
		wantBodyAssert string // takes the extension of the file you want to assert, ie. "html" or "xml"
		wantBodyString string
	}{
		{
			name:   "with correct values",
			path:   "/feeds",
			method: http.MethodPost,
			params: map[string]string{
				queryParamKeywords: "golang",
				queryParamLocation: "berlin",
			},
			wantStatus:     http.StatusOK,
			wantBodyAssert: "html",
		},
		{
			name:   "with missing param keywords",
			path:   "/feeds",
			method: http.MethodPost,
			params: map[string]string{
				queryParamLocation: "berlin",
			},
			wantStatus:     http.StatusBadRequest,
			wantBodyString: "missing params: [keywords]",
		},
		{
			name:   "with incorrect param keywords",
			path:   "/feeds",
			method: http.MethodPost,
			params: map[string]string{
				queryParamKeywords: "golang-",
				queryParamLocation: "berlin",
			},
			wantStatus:     http.StatusBadRequest,
			wantBodyString: "invalid params: [keywords], only [A-Za-z0-9] allowed",
		},
		{
			name:   "with incorrect param location",
			path:   "/feeds",
			method: http.MethodPost,
			params: map[string]string{
				queryParamKeywords: "golang",
				queryParamLocation: "berlin&",
			},
			wantStatus:     http.StatusBadRequest,
			wantBodyString: "invalid params: [location], only [A-Za-z0-9] allowed",
		},
		{
			name:   "with missing param location",
			path:   "/feeds",
			method: http.MethodPost,
			params: map[string]string{
				queryParamKeywords: "golang",
			},
			wantStatus:     http.StatusBadRequest,
			wantBodyString: "missing params: [location]",
		},
		{
			name:   "with missing param keywords and incorrect param location",
			path:   "/feeds",
			method: http.MethodPost,
			params: map[string]string{
				queryParamLocation: "the-moon",
			},
			wantStatus:     http.StatusBadRequest,
			wantBodyString: "missing params: [keywords], invalid params: [location], only [A-Za-z0-9] allowed",
		},
		{
			name:   "valid XML feed",
			path:   "/feeds",
			method: http.MethodGet,
			params: map[string]string{
				queryParamKeywords: "golang",
				queryParamLocation: "berlin",
			},
			wantStatus:     http.StatusOK,
			wantHeaders:    map[string]string{"Content-Type": "application/rss+xml"},
			wantBodyAssert: "xml",
		},
		{
			name:   "invalid XML feed", // Returns a valid xml with a single post with instructions.
			path:   "/feeds",
			method: http.MethodGet,
			params: map[string]string{
				queryParamKeywords: "fluffy dogs",
				queryParamLocation: "the moon",
			},
			wantStatus:     http.StatusOK,
			wantHeaders:    map[string]string{"Content-Type": "application/rss+xml"},
			wantBodyAssert: "xml",
		},
		{
			name:   "valid HTML feed",
			path:   "/feeds",
			method: http.MethodGet,
			params: map[string]string{
				queryParamKeywords: "golang",
				queryParamLocation: "berlin",
			},
			headers:        map[string]string{"Accept": "text/html"},
			wantStatus:     http.StatusOK,
			wantHeaders:    map[string]string{"Content-Type": "text/html"},
			wantBodyAssert: "html",
		},
		{
			name:   "invalid HTML feed", // Returns a valid html with instructions.
			path:   "/feeds",
			method: http.MethodGet,
			params: map[string]string{
				queryParamKeywords: "fluffy dogs",
				queryParamLocation: "the moon",
			},
			headers:        map[string]string{"Accept": "text/html"},
			wantStatus:     http.StatusOK,
			wantHeaders:    map[string]string{"Content-Type": "text/html"},
			wantBodyAssert: "html",
		},
		{
			name:   "with missing param keywords",
			path:   "/feeds",
			method: http.MethodGet,
			params: map[string]string{
				queryParamLocation: "berlin",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:           "help page",
			path:           "/help",
			method:         http.MethodGet,
			wantStatus:     http.StatusOK,
			wantBodyAssert: "html",
		},
		{
			name:           "index page",
			path:           "/",
			method:         http.MethodGet,
			wantStatus:     http.StatusOK,
			wantBodyAssert: "html",
		},
		{
			name:           "rando page",
			path:           "/123",
			method:         http.MethodGet,
			wantStatus:     http.StatusNotFound,
			wantBodyString: "404 page not found\n",
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
			if tt.headers != nil {
				for k, v := range tt.headers {
					req.Header.Add(k, v)
				}
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
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("unable to read response body: %v", err)
			}
			if tt.wantBodyAssert != "" {
				approvals.UseFolder("approvals")
				approvals.VerifyString(t, string(body),
					approvals.Options().ForFile().WithExtension(tt.wantBodyAssert).WithScrubber(scroobbyDoobyDoo),
				)
			}
			if tt.wantBodyString != "" && tt.wantBodyString != string(body) {
				t.Errorf("wanted body string '%s', got '%s'", tt.wantBodyString, string(body))
			}
		})
	}
}

// Scrubs dates, times and server ports
func scroobbyDoobyDoo(s string) string {
	s = regexp.MustCompile(`<pubDate>[^<]*</pubDate>`).ReplaceAllString(s, `<pubDate>DATETIME_SCRUBBED</pubDate>`)
	s = regexp.MustCompile(`\(posted [^)]*\)`).ReplaceAllString(s, `(posted POSTED_AT_SCRUBBED)`)
	s = regexp.MustCompile(`<td>\s*[A-Za-z]{3}\s+\d{1,2}\s*</td>`).ReplaceAllString(s, `<td>DATE_SCRUBBED</td>`)
	s = regexp.MustCompile(`127\.0\.0\.1:\d+`).ReplaceAllString(s, `127.0.0.1:PORT_SCRUBBED`)
	return s
}
