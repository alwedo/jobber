package server

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alwedo/jobber/assets"
	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/jobber"
	"github.com/alwedo/jobber/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// Query Params.
	queryParamKeywords = "keywords"
	queryParamLocation = "location"
)

// Input validation regex.
var isValidKeywords = regexp.MustCompile(`^[A-Za-z0-9 ]+$`)
var isValidLocation = regexp.MustCompile(`^[A-Za-z ]+$`)

type server struct {
	logger *slog.Logger
	jobber *jobber.Jobber
	html   *htmlRenderer
}

func New(l *slog.Logger, j *jobber.Jobber) (*http.Server, error) {
	r, err := newHTMLRenderer(assets.HTMLFiles, "base.tmpl", "xmlfeed.tmpl", "partials/*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("unable to parse templates: %w", err)
	}

	s := &server{logger: l, jobber: j, html: r}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /feeds", s.feed())
	mux.HandleFunc("POST /feeds", s.create())
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /help", s.help())
	mux.HandleFunc("GET /", s.index())
	mux.Handle("GET /static/", staticHeadersMiddleware(http.StripPrefix("/static", http.FileServerFS(assets.StaticFiles))))

	return &http.Server{
		Addr:              ":80",
		Handler:           metrics.HTTPMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}, nil
}

func (s *server) index() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if err := s.html.render(w, nil, "base", "pages/home.tmpl"); err != nil {
			s.internalError(w, "failed to execute template in server.index", err)
		}
	}
}

func (s *server) help() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := s.html.render(w, nil, "base", "pages/help.tmpl"); err != nil {
			s.internalError(w, "failed to execute template in server.help", err)
		}
	}
}

func (s *server) create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params, err := validateParams([]string{queryParamKeywords, queryParamLocation}, w, r)
		if err != nil {
			s.logger.Info("missing params in server.create", slog.String("error", err.Error()))
			return
		}

		var timedOut bool
		if err := s.jobber.CreateQuery(r.Context(), params.Get(queryParamKeywords), params.Get(queryParamLocation)); err != nil {
			if errors.Is(err, jobber.ErrTimedOut) {
				timedOut = true
			} else {
				s.internalError(w, "failed to create query", err)
				return
			}
		}

		scheme := "https://"
		if r.Host == "localhost" {
			scheme = "http://"
		}
		u, err := url.Parse(scheme + r.Host + "/feeds")
		if err != nil {
			s.internalError(w, "failed to parse url in server.create", err)
			return
		}
		u.RawQuery = params.Encode()

		data := struct {
			URL      string
			TimedOut bool
		}{u.String(), timedOut}

		if err := s.html.render(w, data, "partial:response:create"); err != nil {
			s.internalError(w, "failed to execute template in server.create", err)
		}
	}
}

type feedData struct {
	Keywords string
	Location string
	Host     string
	Offers   []*db.Offer
}

func (s *server) feed() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params, err := validateParams([]string{queryParamKeywords, queryParamLocation}, w, r)
		if err != nil {
			s.logger.Info("missing params in server.feed", slog.String("error", err.Error()))
			return
		}
		var (
			keywords = params.Get(queryParamKeywords)
			location = params.Get(queryParamLocation)
		)

		offers, updatedAt, err := s.jobber.ListOffers(r.Context(), &db.GetQueryParams{
			Keywords: keywords,
			Location: location,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.NotFound(w, r)
			} else {
				s.internalError(w, "failed to get query in server.feed", err)
			}
			return
		}
		if updatedAt != nil && updatedAt.Valid {
			// We set a Cache-Control header with max-age so clients don't
			// waste time re-fetching information that hasn't been updated.
			// Since the queries get updated hourly, we want the max-age value
			// to be time in seconds until the next update.
			// If the calculated value is more than one hour we don't retun the
			// header since we can't guarantee when the next update will be.
			lastUpdate := time.Since(updatedAt.Time)
			if lastUpdate < time.Hour {
				t := time.Hour - lastUpdate
				w.Header().Add("Cache-Control", "max-age="+strconv.Itoa(int(t.Seconds())))
			}
		}

		// Set template and Content-Type header based on Accept header.
		// If Accept header is 'text/html' we assue the request is coming
		// from a browser, otherwise it's an RSS reader.
		ct := "application/rss+xml"
		tmpl := "xmlfeed"
		var addtmpl []string
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			ct = "text/html"
			tmpl = "base"
			addtmpl = []string{"pages/feed.tmpl"}
		}
		w.Header().Add("Content-Type", ct)

		data := &feedData{
			Keywords: keywords,
			Location: location,
			Host:     r.Host,
			Offers:   offers,
		}

		if err := s.html.render(w, data, tmpl, addtmpl...); err != nil {
			s.internalError(w, "failed to execute template in server.feed", err)
		}
	}
}

func staticHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/static/")

		var ct string
		switch {
		case strings.HasPrefix(path, "css/"):
			ct = "text/css"
		case strings.HasPrefix(path, "js/"):
			ct = "application/javascript"
		}

		if ct != "" {
			w.Header().Add("Content-Type", ct)
			w.Header().Add("Cache-Control", "public, max-age=31536000, immutable")
			w.Header().Add("X-Content-Type-Options", "nosniff")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) internalError(w http.ResponseWriter, msg string, err error) {
	// If the client disconnected (broken pipe / connection reset) avoid
	// attempting further writes which will only produce noise (and the
	// superfluous WriteHeader log). Log at info and return.
	var errStr string
	if err != nil {
		errStr = err.Error()
	}
	if strings.Contains(errStr, "broken pipe") || strings.Contains(errStr, "connection reset by peer") {
		s.logger.Info(msg, slog.String("error", errStr))
		return
	}

	s.logger.Error(msg, slog.String("error", errStr))
	http.Error(w, "it's not you it's me", http.StatusInternalServerError)
}

// validateParams receives a list of params, validate they've been supplied in the request and normalizes them.
// If a param is missing or contains invalid characters, it will respond with 400.
func validateParams(params []string, w http.ResponseWriter, r *http.Request) (url.Values, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return nil, fmt.Errorf("request body too large: %w", err)
	}

	missing := []string{}
	invalid := []string{}
	valid := url.Values{}
	for _, p := range params {
		v := r.FormValue(p)
		switch {
		case v == "":
			missing = append(missing, p)
		case p == queryParamKeywords && !isValidKeywords.MatchString(v) ||
			p == queryParamLocation && !isValidLocation.MatchString(v):
			invalid = append(invalid, p)
		default:
			valid.Add(p, strings.ToLower(strings.TrimSpace(v)))
		}
	}
	if len(missing) != 0 || len(invalid) != 0 {
		w.WriteHeader(http.StatusBadRequest)
		var errStr []string
		if len(missing) != 0 {
			errStr = append(errStr, fmt.Sprintf("missing params: %v", missing))
		}
		if len(invalid) != 0 {
			errStr = append(errStr, fmt.Sprintf("invalid params: %v, only [A-Za-z0-9] allowed for keywords and [A-Za-z] for location", invalid))
		}
		_, err := fmt.Fprint(w, strings.Join(errStr, ", "))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return nil, fmt.Errorf("unable to write response in validateParams: %w", err)
		}
		return nil, fmt.Errorf("missing params in validateParams: %v", missing)
	}
	return valid, nil
}
