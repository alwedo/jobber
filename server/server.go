package server

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/jobber"
	"github.com/alwedo/jobber/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// Path Params.
	pathParamStatic = "static"

	// Query Params.
	queryParamKeywords = "keywords"
	queryParamLocation = "location"

	// Static assets.
	assetStyle  = "assets/css/style.css"
	assetScript = "assets/js/script.js"

	// Templates.
	tmplIndex          = "index.gohtml"
	tmplHelp           = "help.gohtml"
	tmplFeedRSS        = "feed_rss.goxml"
	tmplFeedHTML       = "feed_html.gohtml"
	tmplCreateResponse = "create_response.gohtml"
)

//go:embed assets/*
var assets embed.FS
var isMainStyle = regexp.MustCompile(`^style\.v[\d.]+\.css$`)
var isMainScript = regexp.MustCompile(`^script\.v[\d.]+\.js$`)

type server struct {
	logger    *slog.Logger
	jobber    *jobber.Jobber
	templates *template.Template
}

func New(l *slog.Logger, j *jobber.Jobber) (*http.Server, error) {
	t, err := template.New("").Funcs(funcMap).ParseFS(assets, "assets/templates/*")
	if err != nil {
		return nil, fmt.Errorf("unable to parse templates: %v", err)
	}
	s := &server{logger: l, jobber: j, templates: t}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /feeds", s.feed())
	mux.HandleFunc("POST /feeds", s.create())
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /help", s.help())
	mux.HandleFunc("GET /", s.index())
	mux.HandleFunc("GET /static/{static}", s.static())

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
		if err := s.templates.ExecuteTemplate(w, tmplIndex, nil); err != nil {
			s.internalError(w, "failed to execute template in server.index", err)
			return
		}
	}
}

func (s *server) help() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := s.templates.ExecuteTemplate(w, tmplHelp, nil); err != nil {
			s.internalError(w, "failed to execute template in server.help", err)
			return
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
		if err := s.jobber.CreateQuery(params.Get(queryParamKeywords), params.Get(queryParamLocation)); err != nil {
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

		if err := s.templates.ExecuteTemplate(w, tmplCreateResponse, data); err != nil {
			s.internalError(w, "failed to execute template in server.create", err)
			return
		}
	}
}

type feedData struct {
	Keywords string
	Location string
	Host     string
	Offers   []*db.Offer
	NotFound bool
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
			notFound bool
		)

		offers, updatedAt, err := s.jobber.ListOffers(r.Context(), &db.GetQueryParams{
			Keywords: keywords,
			Location: location,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				notFound = true
				s.logger.Info("no query found in server.feed", slog.Any("params", params), slog.String("error", err.Error()))
			} else {
				s.internalError(w, "failed to get query in server.feed", err)
				return
			}
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

		var tmpl string
		// Set template and Content-Type header based on Accept header.
		// If Accept header is 'text/html' we assue the request is coming
		// from a browser, otherwise it's an RSS reader.
		switch strings.Contains(r.Header.Get("Accept"), "text/html") {
		case true:
			tmpl = tmplFeedHTML
			w.Header().Add("Content-Type", "text/html")
		default:
			tmpl = tmplFeedRSS
			w.Header().Add("Content-Type", "application/rss+xml")
		}

		if err := s.templates.ExecuteTemplate(w, tmpl, &feedData{
			Keywords: keywords,
			Location: location,
			Host:     r.Host,
			NotFound: notFound,
			Offers:   offers,
		}); err != nil {
			s.internalError(w, "failed to execute template in server.feed", err)
			return
		}
	}
}

func (s *server) static() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var a string
		var ct string

		path := r.PathValue(pathParamStatic)

		switch {
		case isMainStyle.MatchString(path):
			a = assetStyle
			ct = "text/css"
		case isMainScript.MatchString(path):
			a = assetScript
			ct = "application/javascript"
		default:
			http.NotFound(w, r)
			return
		}

		f, err := assets.Open(a)
		if err != nil {
			s.internalError(w, "failed to open asset file "+a, err)
			return
		}

		w.Header().Add("Content-Type", ct)
		w.Header().Add("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Add("X-Content-Type-Options", "nosniff")

		_, err = io.Copy(w, f)
		if err != nil {
			s.internalError(w, "failed to serve "+a, err)
			return
		}
	}
}

func (s *server) internalError(w http.ResponseWriter, msg string, err error) {
	s.logger.Error(msg, slog.String("error", err.Error()))
	http.Error(w, "it's not you it's me", http.StatusInternalServerError)
}

// Input validation regex.
var re = regexp.MustCompile(`^[A-Za-z0-9 ]+$`)

// validateParams receives a list of params, validate they've been supplied in the request and normalizes them.
// If a param is missing or contains invalid characters, it will respond with 400.
func validateParams(params []string, w http.ResponseWriter, r *http.Request) (url.Values, error) {
	missing := []string{}
	invalid := []string{}
	valid := url.Values{}
	for _, p := range params {
		v := r.FormValue(p)
		switch {
		case v == "":
			missing = append(missing, p)
		case !re.MatchString(v):
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

var funcMap = template.FuncMap{
	"pubDate": func(o *db.Offer) string {
		return o.PostedAt.Time.Format(time.RFC1123Z)
	},
	"postedAt": func(o *db.Offer) string {
		return o.PostedAt.Time.Format("Jan 2")
	},
}
