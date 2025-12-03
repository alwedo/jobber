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
	"strings"
	"text/template"
	"time"

	"github.com/Alvaroalonsobabbel/jobber/db"
	"github.com/Alvaroalonsobabbel/jobber/jobber"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// Params.
	queryParamKeywords = "keywords"
	queryParamLocation = "location"

	// Create RSS feed response for htmlx.
	createResponse = `<p>done!<br><button class="copy-button" onclick="copyToClipboard('%s')">copy RSS feed</button> <button class="reset-button" onclick="resetForm()">create another RSS feed</button></p>`

	// Assets.
	assetsGlob = "assets/*"
	assetIndex = "index.html"
	assetRSS   = "rss.goxml"
)

//go:embed assets/*
var assets embed.FS

var requestDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{Name: "http_request_duration_seconds"},
	[]string{"code", "method"},
)

type server struct {
	logger    *slog.Logger
	jobber    *jobber.Jobber
	templates *template.Template
}

func New(l *slog.Logger, j *jobber.Jobber) (*http.Server, error) {
	prometheus.MustRegister(requestDuration)

	t, err := template.New("").Funcs(funcMap).ParseFS(assets, assetsGlob)
	if err != nil {
		return nil, err
	}
	s := &server{logger: l, jobber: j, templates: t}
	mux := http.NewServeMux()
	mux.Handle("GET /feeds", s.withMetrics(s.feed()))
	mux.Handle("POST /feeds", s.withMetrics(s.create()))
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("/", s.index())

	return &http.Server{
		Addr:              ":80",
		Handler:           mux,
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

		u, err := url.Parse("https://" + r.Host + "/feeds")
		if err != nil {
			s.internalError(w, "failed to parse url in server.create", err)
			return
		}
		u.RawQuery = params.Encode()

		_, err = fmt.Fprintf(w, createResponse, u.String())
		if err != nil {
			s.logger.Error("failed to write response", slog.String("url", u.String()), slog.String("error", err.Error()))
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
		d := &feedData{
			Keywords: params.Get(queryParamKeywords),
			Location: params.Get(queryParamLocation),
			Host:     r.Host,
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
		w.Header().Add("Content-Type", "application/rss+xml")
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

func (s *server) withMetrics(next http.Handler) http.Handler {
	return promhttp.InstrumentHandlerDuration(requestDuration, next)
}

// validateParams receives a list of params, validate they've
// been supplied in the request and normalizes them.
// If a param is missing, it will respond with 400.
func validateParams(params []string, w http.ResponseWriter, r *http.Request) (url.Values, error) {
	missing := []string{}
	valid := url.Values{}
	for _, p := range params {
		v := r.FormValue(p)
		if v == "" {
			missing = append(missing, p)
			continue
		}
		valid.Add(p, strings.ToLower(strings.TrimSpace(v)))
	}
	if len(missing) != 0 {
		w.WriteHeader(http.StatusBadRequest)
		_, err := fmt.Fprintf(w, "missing params: %v", missing)
		if err != nil {
			return nil, fmt.Errorf("unable to write response in validateParams: %w", err)
		}
		return nil, fmt.Errorf("missing params: %v", missing)
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
