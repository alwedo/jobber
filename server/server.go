package server

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/jobber"
	"github.com/alwedo/jobber/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// Params.
	queryParamKeywords = "keywords"
	queryParamLocation = "location"

	// Assets.
	assetsGlob          = "assets/*"
	assetIndex          = "index.gohtml"
	assetHelp           = "help.gohtml"
	assetRSS            = "rss.goxml"
	assetCreateResponse = "create_response.gohtml"
)

//go:embed assets/*
var assets embed.FS

type server struct {
	logger    *slog.Logger
	jobber    *jobber.Jobber
	templates *template.Template
}

func New(l *slog.Logger, j *jobber.Jobber) (*http.Server, error) {
	t, err := template.New("").Funcs(funcMap).ParseFS(assets, assetsGlob)
	if err != nil {
		return nil, err
	}
	s := &server{logger: l, jobber: j, templates: t}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /feeds", s.feed())
	mux.HandleFunc("POST /feeds", s.create())
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /help", s.help())
	mux.HandleFunc("/", s.index())

	return &http.Server{
		Addr:              ":80",
		Handler:           metrics.HTTPMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}, nil
}

func (s *server) index() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		if err := s.templates.ExecuteTemplate(w, assetIndex, nil); err != nil {
			s.internalError(w, "failed to execute template in server.index", err)
			return
		}
	}
}

func (s *server) help() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := s.templates.ExecuteTemplate(w, assetHelp, nil); err != nil {
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
		if err := s.jobber.CreateQuery(params.Get(queryParamKeywords), params.Get(queryParamLocation)); err != nil {
			s.internalError(w, "failed to create query", err)
			return
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

		if err := s.templates.ExecuteTemplate(w, assetCreateResponse, u.String()); err != nil {
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
	Browser  bool
}

func (s *server) feed() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params, err := validateParams([]string{queryParamKeywords, queryParamLocation}, w, r)
		if err != nil {
			s.logger.Info("missing params in server.feed", slog.String("error", err.Error()))
			return
		}
		d := &feedData{
			Keywords: params.Get(queryParamKeywords),
			Location: params.Get(queryParamLocation),
			Host:     r.Host,
		}
		// If the header has Accept="text/html" it means it's coming from a Browser.
		// We set Browser to true in in request data and render html instead of RSS XML.
		switch strings.Contains(r.Header.Get("Accept"), "text/html") {
		case true:
			d.Browser = true
			w.Header().Add("Content-Type", "text/html")
		default:
			w.Header().Add("Content-Type", "application/rss+xml")
		}

		offers, err := s.jobber.ListOffers(params.Get(queryParamKeywords), params.Get(queryParamLocation))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				d.NotFound = true
				s.logger.Info("no query found in server.feed", slog.Any("params", params), slog.String("error", err.Error()))
			} else {
				s.internalError(w, "failed to get query in server.feed", err)
				return
			}
		}
		d.Offers = offers
		if err := s.templates.ExecuteTemplate(w, assetRSS, d); err != nil {
			s.internalError(w, "failed to execute template in server.feed", err)
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
			errStr = append(errStr, fmt.Sprintf("invalid params: %v, only [A-Za-z0-9] allowed", invalid))
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
	"createdAt": func(o *db.Offer) string {
		return o.CreatedAt.Time.Format(time.RFC1123Z)
	},
	"title": func(o *db.Offer) string {
		t := fmt.Sprintf("%s at %s (posted %s)", o.Title, o.Company, o.PostedAt.Time.Format("Jan 2"))
		return html.EscapeString(t)
	},
	"now": func() string {
		return time.Now().Format(time.RFC1123Z)
	},
}
