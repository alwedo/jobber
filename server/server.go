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
)

const (
	queryParamKeywords = "keywords"
	queryParamLocation = "location"

	createResponse = `<p>RSS Feed Created Successfully!</p><p><button class="copy-button" onclick="copyToClipboard('%s')">Copy Feed URL</button></p>`

	// Assets
	assetsGlob = "assets/*"
	assetIndex = "index.html"
	assetRSS   = "rss.goxml"
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
	mux.HandleFunc("/", s.index())

	return &http.Server{
		Addr:              ":80",
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}, nil
}

func (s *server) index() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := s.templates.ExecuteTemplate(w, assetIndex, nil); err != nil {
			s.logger.Error("failed to execute template in server.index", slog.String("error", err.Error()))
			http.Error(w, "it's not you it's me", http.StatusInternalServerError)
			return
		}
	}
}

func (s *server) create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params, err := validateParams([]string{queryParamKeywords, queryParamLocation}, r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			s.logger.Info("missing params in server.create", slog.String("error", err.Error()))
			_, err := w.Write([]byte(err.Error()))
			if err != nil {
				s.logger.Error("unable to write response in server.create, validateParams", slog.String("error", err.Error()))
			}
			return
		}
		if err := s.jobber.CreateQuery(params.Get(queryParamKeywords), params.Get(queryParamLocation)); err != nil {
			s.logger.Error("failed to create query", slog.Any("params", params), slog.String("error", err.Error()))
			http.Error(w, "it's not you it's me", http.StatusInternalServerError)
			return
		}

		u, err := url.Parse("https://" + r.Host + "/feeds")
		if err != nil {
			s.logger.Error("failed to parse url in server.create", slog.String("error", err.Error()))
			http.Error(w, "it's not you it's me", http.StatusInternalServerError)
			return
		}
		u.RawQuery = params.Encode()

		response := fmt.Sprintf(createResponse, u.String())
		_, err = w.Write([]byte(response))
		if err != nil {
			s.logger.Error("failed to write response", slog.String("url", u.String()), slog.String("error", err.Error()))
		}
	}
}

type data struct {
	Keywords, Location string
	Offers             []*db.Offer
}

func (s *server) feed() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		k := r.FormValue(queryParamKeywords)
		l := r.FormValue(queryParamLocation)

		offers, err := s.jobber.ListOffers(k, l)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// TODO: return xml with invalid query?
				s.logger.Info("no query found", "keywords", k, "location", l)
				http.Error(w, "no query found", http.StatusNotFound)
				return
			}
			s.logger.Error("failed to get query: " + err.Error())
			http.Error(w, "failed to get query", http.StatusInternalServerError)
			return
		}

		w.Header().Add("Content-Type", "application/rss+xml")
		d := &data{
			Keywords: k,
			Location: l,
			Offers:   offers,
		}
		if err := s.templates.ExecuteTemplate(w, assetRSS, d); err != nil {
			s.logger.Error("failed to execute template in server.feed: " + err.Error())
			http.Error(w, "it's not you it's me", http.StatusInternalServerError)
			return
		}
	}
}

// validateParams receives a list of params, validate they've
// been supplied in the request and normalizes them.
func validateParams(params []string, r *http.Request) (url.Values, error) {
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
}
