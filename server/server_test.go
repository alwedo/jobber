package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/jobber"
	"github.com/alwedo/jobber/scrape"
	approvals "github.com/approvals/go-approval-tests"
)

func TestServer(t *testing.T) {
	l := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	d, dbCloser := db.NewTestDB(t)
	defer dbCloser()

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
		jobberOpts     []jobber.Options
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
			name:   "query creation timeout",
			path:   "/feeds",
			method: http.MethodPost,
			params: map[string]string{
				queryParamKeywords: "fluffy dogs",
				queryParamLocation: "berlin",
			},
			wantStatus:     http.StatusOK,
			jobberOpts:     []jobber.Options{jobber.WithTimeOut(time.Nanosecond)},
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
			wantStatus: http.StatusOK,
			wantHeaders: map[string]string{
				"Content-Type": "application/rss+xml",
				// golang-berlin query in the db seed has been updated 30 min ago.
				"Cache-Control": "max-age=1799",
			},
			wantBodyAssert: "xml",
		},
		{
			name:   "valid XML feed with no cache-control",
			path:   "/feeds",
			method: http.MethodGet,
			params: map[string]string{
				queryParamKeywords: "data scientist",
				queryParamLocation: "new york",
			},
			wantStatus: http.StatusOK,
			wantHeaders: map[string]string{
				"Content-Type": "application/rss+xml",
				// data scientist-new york query updated_at field is null in db seed.
				"Cache-Control": "",
			},
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
		{
			name:       "static/style.css",
			path:       "/static/style.css",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			wantHeaders: map[string]string{
				"Content-Type":           "text/css",
				"Cache-Control":          "public, max-age=31536000, immutable",
				"X-Content-Type-Options": "nosniff",
			},
			wantBodyString: func() string {
				f, _ := assets.ReadFile(assetStyle) //nolint: errcheck
				return string(f)
			}(),
		},
		{
			name:       "static/script.js",
			path:       "/static/script.js",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			wantHeaders: map[string]string{
				"Content-Type":           "application/javascript",
				"Cache-Control":          "public, max-age=31536000, immutable",
				"X-Content-Type-Options": "nosniff",
			},
			wantBodyString: func() string {
				f, _ := assets.ReadFile(assetScript) //nolint: errcheck
				return string(f)
			}(),
		},
		{
			name:           "static/notfound",
			path:           "/static/cuak",
			method:         http.MethodGet,
			wantStatus:     http.StatusNotFound,
			wantBodyString: "404 page not found\n",
		},
		{
			name:       "static wrong method",
			path:       "/static/script.js",
			method:     http.MethodPost,
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.method+tt.path+" "+tt.name, func(t *testing.T) {
			tt.jobberOpts = append(tt.jobberOpts, jobber.WithScrapeList(scrape.MockList))
			j, jCloser := jobber.New(l, d, tt.jobberOpts...)
			defer jCloser()
			svr, err := New(l, j)
			if err != nil {
				t.Fatal(err)
			}
			client := http.DefaultClient
			server := httptest.NewServer(svr.Handler)
			defer server.Close()
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
	s = regexp.MustCompile(`<b>Posted:</b>[^<]*</li>`).ReplaceAllString(s, `<b>Posted:</b>DATETIME_SCRUBBED</li>`)
	s = regexp.MustCompile(`<td>\s*[A-Za-z]{3}\s+\d{1,2}\s*</td>`).ReplaceAllString(s, `<td>DATE_SCRUBBED</td>`)
	s = regexp.MustCompile(`<b>Posted</b>:\s*[A-Za-z]{3}\s+\d{1,2}<br>`).ReplaceAllString(s, `<b>Posted</b>: DATE_SCRUBBED<br>`)
	s = regexp.MustCompile(`127\.0\.0\.1:\d+`).ReplaceAllString(s, `127.0.0.1:PORT_SCRUBBED`)
	return s
}
